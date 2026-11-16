"""Counters, gauges and histograms over OpenTelemetry, and Pydantic models of them.

On a free-threaded build the SDK's synchronous counter is slow when threads
count at once: every add takes the instrument's locks and touches state all
threads share, so 14 threads counted slower than one. Here a counter or gauge
write touches only the calling thread's own totals, found through a context
variable; the SDK reads every thread's totals through an asynchronous
instrument when it collects, so the threads never meet on the write path.
Histograms keep the SDK's synchronous instrument.
"""

import contextvars
import threading
import time
import weakref
from threading import get_ident
from typing import Any

from opentelemetry import metrics
from opentelemetry.metrics import Observation
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.metrics.export import PeriodicExportingMetricReader
from opentelemetry.sdk.resources import Resource
from pydantic import BaseModel

from ...options import MetricsOptions, OTLPOptions, ServiceOptions
from ..exporters import metric_exporter
from ..foxglove import UnifiedMcapWriter

# Providers made here: their owner shuts them down on close, so a later
# instance makes its own instead of reusing one.
_OWN_PROVIDERS: "weakref.WeakSet[MeterProvider]" = weakref.WeakSet()


def _attr_key(labels: dict[str, Any]) -> tuple:
    """A hashable key for a label set, the same whatever the labels' order."""
    try:
        key = tuple(sorted(labels.items()))
        hash(key)
        return key
    except TypeError:  # a list value: kept as a tuple, which OTLP sends alike
        return tuple(
            sorted(
                (k, tuple(v) if isinstance(v, list) else v) for k, v in labels.items()
            )
        )


class _Totals:
    """One thread's counter sums and last gauge values, by metric name and then
    by label set. Only its thread writes them; the collection reads copies."""

    __slots__ = ("ident", "thread", "sums", "gauges")

    def __init__(self) -> None:
        self.ident = get_ident()
        self.thread = threading.current_thread()
        self.sums: dict[str, dict[tuple, float]] = {}
        # (monotonic ns, value): the latest write across threads wins
        self.gauges: dict[str, dict[tuple, tuple[int, float]]] = {}


class TelemetryMetrics:
    """
    Metrics system that integrates OpenTelemetry metrics with Pydantic models.

    Features:
    - Automatic metric extraction from Pydantic models using field metadata
    - Sends metrics to OTLP collector when enabled
    - Writes metrics to MCAP file when enabled
    """

    def __init__(
        self,
        service_opts: ServiceOptions,
        metrics_opts: MetricsOptions,
        otlp_opts: OTLPOptions | None = None,
        mcap_writer: UnifiedMcapWriter | None = None,
    ):
        self.service_opts = service_opts
        self.metrics_opts = metrics_opts
        self.mcap_writer = mcap_writer

        # Initialize OpenTelemetry metrics if enabled
        self.meter = None
        self._provider: MeterProvider | None = None  # set when this instance owns it
        self._closed = False
        self._histograms: dict[str, Any] = {}
        self._observed: dict[str, Any] = {}  # asynchronous instruments by name
        self._mine = contextvars.ContextVar(
            f"telemetry-metric-totals-{id(self)}", default=None
        )
        self._lock = threading.Lock()
        self._threads: list[_Totals] = []
        # What threads that have exited counted, folded together
        self._retired = _Totals()

        if otlp_opts and otlp_opts.enabled:
            self._setup_otel_metrics(service_opts, metrics_opts, otlp_opts)

    def _setup_otel_metrics(
        self,
        service_opts: ServiceOptions,
        metrics_opts: MetricsOptions,
        otlp_opts: OTLPOptions,
    ):
        """Setup OpenTelemetry metrics with OTLP exporter"""
        # A MeterProvider the application set is reused, to avoid override
        # warnings; it is flushed on close, not shut down.
        current = metrics.get_meter_provider()
        if isinstance(current, MeterProvider) and current not in _OWN_PROVIDERS:
            self.meter = metrics.get_meter(service_opts.name)
            return

        resource = Resource.create(
            {
                "service.name": service_opts.name,
                "service.version": service_opts.version,
                "service.environment": service_opts.environment.value,
            }
        )

        headers = dict(otlp_opts.headers)
        if otlp_opts.auth_token:
            headers["authorization"] = f"Bearer {otlp_opts.auth_token}"
        exporter = metric_exporter(
            otlp_opts.endpoint,
            otlp_opts.secure,
            otlp_opts.use_http,
            headers,
            otlp_opts.export.timeout,
        )

        reader = PeriodicExportingMetricReader(
            exporter,
            export_interval_millis=metrics_opts.export_interval_seconds * 1000,
        )

        provider = MeterProvider(resource=resource, metric_readers=[reader])
        _OWN_PROVIDERS.add(provider)
        self._provider = provider
        # The first one becomes the global provider; a later one, made while
        # ours is global, stays private to this instance.
        if not isinstance(current, MeterProvider):
            metrics.set_meter_provider(provider)
        self.meter = provider.get_meter(service_opts.name)

    def record(self, model: BaseModel, labels: dict[str, Any] | None = None):
        """
        Record metrics from a Pydantic model.

        The model should use field json_schema_extra to specify metric types:

        Example:
            class MyMetrics(BaseModel):
                request_count: int = Field(json_schema_extra={"metric_type": "counter", "metric_name": "requests_total"})
                response_time: float = Field(json_schema_extra={"metric_type": "histogram", "metric_name": "response_time_ms"})
                active_users: int = Field(json_schema_extra={"metric_type": "gauge", "metric_name": "active_users"})
        """
        if not isinstance(model, BaseModel):
            raise ValueError("record() requires a Pydantic BaseModel instance")

        # Auto-inject service labels from service_opts so callers don't have to
        service_labels = getattr(self.service_opts, "labels", None) or {}
        if service_labels:
            merged = dict(service_labels)
            if labels:
                merged.update(labels)
            labels = merged

        # If model is a MetricsBaseModel, resolve metric names with service name prefix
        if hasattr(model, "_resolve_metric_names"):
            model._resolve_metric_names(service_name=self.service_opts.name)

        # Extract metrics from model fields
        for field_name, field_info in model.model_fields.items():
            # Check json_schema_extra for metric metadata
            json_schema_extra = field_info.json_schema_extra
            if not json_schema_extra or not isinstance(json_schema_extra, dict):
                continue

            if "metric_type" not in json_schema_extra:
                continue

            metric_type = json_schema_extra.get("metric_type")
            metric_name = json_schema_extra.get("metric_name", field_name)
            value = getattr(model, field_name)

            # Skip metrics with default value (0) that were not explicitly set.
            # For gauges: avoids resetting unrelated fields in partial updates.
            # For counters/histograms: avoids creating label-less ghost series when
            if value == 0 and field_name not in model.model_fields_set:
                continue

            # Record based on type
            if metric_type == "counter":
                self._record_counter(metric_name, float(value), labels)
            elif metric_type == "histogram":
                self._record_histogram(
                    metric_name, float(value), labels, field_info=field_info
                )
            elif metric_type == "gauge":
                self._record_gauge(metric_name, float(value), labels)

    # The writer side: each thread writes its own totals, without locks.

    def _totals(self) -> _Totals:
        """This thread's totals. Threads may inherit a copy of their creator's
        context, and with it the creator's totals; the thread id tells."""
        mine = self._mine.get()
        if mine is None or mine.ident != get_ident():
            mine = _Totals()
            with self._lock:
                self._threads.append(mine)
            self._mine.set(mine)
        return mine

    def _record_counter(
        self, name: str, value: float, labels: dict[str, Any] | None = None
    ):
        """Record a counter metric"""
        if self.meter is not None:
            mine = self._mine.get()
            if mine is None or mine.ident != get_ident():
                mine = self._totals()
            sums = mine.sums.get(name)
            if sums is None:
                sums = mine.sums[name] = self._observe(name, "counter")
            key = _attr_key(labels) if labels else ()
            sums[key] = sums.get(key, 0.0) + value

        # MCAP
        if self.mcap_writer and not self.mcap_writer.is_closed():
            self.mcap_writer.write_metric(
                name=name,
                value=value,
                metric_type="counter",
                labels=labels,
                timestamp=time.time_ns(),
            )

    def _record_histogram(
        self,
        name: str,
        value: float,
        labels: dict[str, Any] | None = None,
        field_info: Any | None = None,
    ):
        """Record a histogram metric"""
        # OTEL histogram
        if self.meter:
            histogram = self._histograms.get(name)
            if histogram is None:
                # Extract buckets from field_info if available
                buckets = None
                if (
                    field_info
                    and hasattr(field_info, "json_schema_extra")
                    and field_info.json_schema_extra
                ):
                    buckets = field_info.json_schema_extra.get("buckets")

                # Create histogram with advisory buckets if provided
                kwargs = {}
                if buckets:
                    kwargs["explicit_bucket_boundaries_advisory"] = buckets

                with self._lock:
                    histogram = self._histograms.get(name)
                    if histogram is None:
                        histogram = self._histograms[name] = (
                            self.meter.create_histogram(
                                name,
                                description=(field_info.description or "")
                                if field_info
                                else "",
                                **kwargs,
                            )
                        )
            histogram.record(value, attributes=labels or {})

        # MCAP
        if self.mcap_writer and not self.mcap_writer.is_closed():
            self.mcap_writer.write_metric(
                name=name,
                value=value,
                metric_type="histogram",
                labels=labels,
                timestamp=time.time_ns(),
            )

    def _record_gauge(
        self, name: str, value: float, labels: dict[str, Any] | None = None
    ):
        """Record a gauge metric: its last value, exported as an up-down
        counter's sum (matches the Go implementation)"""
        if self.meter is not None:
            mine = self._mine.get()
            if mine is None or mine.ident != get_ident():
                mine = self._totals()
            values = mine.gauges.get(name)
            if values is None:
                values = mine.gauges[name] = self._observe(name, "gauge")
            values[_attr_key(labels) if labels else ()] = (time.monotonic_ns(), value)

        # MCAP
        if self.mcap_writer and not self.mcap_writer.is_closed():
            self.mcap_writer.write_metric(
                name=name,
                value=value,
                metric_type="gauge",
                labels=labels,
                timestamp=time.time_ns(),
            )

    def counter(
        self, name: str, value: float = 1.0, labels: dict[str, Any] | None = None
    ):
        """Manually record a counter metric"""
        self._record_counter(name, value, labels)

    def histogram(self, name: str, value: float, labels: dict[str, Any] | None = None):
        """Manually record a histogram metric"""
        self._record_histogram(name, value, labels)

    def gauge(self, name: str, value: float, labels: dict[str, Any] | None = None):
        """Manually record a gauge metric"""
        self._record_gauge(name, value, labels)

    # The collection side: the SDK calls these when it collects.

    def _observe(self, name: str, kind: str) -> dict:
        """Creates the asynchronous instrument reporting a metric the first time
        any thread writes it; returns a new, empty set of the thread's totals."""
        with self._lock:
            if name not in self._observed:
                if kind == "counter":
                    self._observed[name] = self.meter.create_observable_counter(
                        name, callbacks=[lambda _options: self._sums(name)]
                    )
                else:
                    self._observed[name] = self.meter.create_observable_up_down_counter(
                        name,
                        callbacks=[lambda _options: self._gauges(name)],
                        unit="1",
                        description=f"Gauge metric: {name}",
                    )
        return {}

    def _live(self) -> list[_Totals]:
        """Every thread's totals, the exited threads' folded into one. An exited
        thread's are first marked, so a thread inheriting them (with a reused
        thread id) makes its own, and folded at the next collection; called
        with the lock held."""
        live = []
        for t in self._threads:
            if t.thread.is_alive():
                live.append(t)
            elif t.ident:
                t.ident = 0
                live.append(t)
            else:
                for name, sums in t.sums.items():
                    into = self._retired.sums.setdefault(name, {})
                    for key, v in sums.items():
                        into[key] = into.get(key, 0.0) + v
                for name, values in t.gauges.items():
                    into = self._retired.gauges.setdefault(name, {})
                    for key, tv in values.items():
                        if key not in into or tv[0] > into[key][0]:
                            into[key] = tv
        self._threads = live
        return [self._retired, *live]

    def _sums(self, name: str) -> list[Observation]:
        totals: dict[tuple, float] = {}
        with self._lock:
            for t in self._live():
                sums = t.sums.get(name)
                if sums:
                    for key, v in sums.copy().items():
                        totals[key] = totals.get(key, 0.0) + v
        return [Observation(v, dict(key)) for key, v in totals.items()]

    def _gauges(self, name: str) -> list[Observation]:
        latest: dict[tuple, tuple[int, float]] = {}
        with self._lock:
            for t in self._live():
                values = t.gauges.get(name)
                if values:
                    for key, tv in values.copy().items():
                        if key not in latest or tv[0] > latest[key][0]:
                            latest[key] = tv
        return [Observation(tv[1], dict(key)) for key, tv in latest.items()]

    def close(self):
        """Exports every counter, gauge and histogram as it stands. A provider
        this instance made is shut down; one the application set is flushed."""
        if self._closed or self.meter is None:
            return
        self._closed = True
        if self._provider is not None:
            self._provider.shutdown()
        else:
            flush = getattr(metrics.get_meter_provider(), "force_flush", None)
            if flush:
                flush()
