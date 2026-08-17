import time
from typing import Any

from opentelemetry import metrics
from opentelemetry.exporter.otlp.proto.grpc.metric_exporter import OTLPMetricExporter
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.metrics.export import PeriodicExportingMetricReader
from opentelemetry.sdk.resources import Resource
from pydantic import BaseModel

from ...options import MetricsOptions, OTLPOptions, ServiceOptions
from ..foxglove import UnifiedMcapWriter


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
        self._counters: dict[str, Any] = {}
        self._histograms: dict[str, Any] = {}
        self._gauges: dict[str, Any] = {}  # Stores UpDownCounter instruments
        self._gauge_values: dict[str, float] = {}  # Tracks current gauge values

        if otlp_opts and otlp_opts.enabled:
            self._setup_otel_metrics(service_opts, metrics_opts, otlp_opts)

    def _setup_otel_metrics(
        self,
        service_opts: ServiceOptions,
        metrics_opts: MetricsOptions,
        otlp_opts: OTLPOptions,
    ):
        """Setup OpenTelemetry metrics with OTLP exporter"""
        # If a real MeterProvider is already set, reuse it to avoid override warnings
        current = metrics.get_meter_provider()
        if isinstance(current, MeterProvider):
            self.meter = metrics.get_meter(service_opts.name)
            return

        resource = Resource.create(
            {
                "service.name": service_opts.name,
                "service.version": service_opts.version,
                "service.environment": service_opts.environment.value,
            }
        )

        exporter = OTLPMetricExporter(
            endpoint=f"{otlp_opts.host}:{otlp_opts.port}",
            insecure=True,
        )

        reader = PeriodicExportingMetricReader(
            exporter,
            export_interval_millis=metrics_opts.export_interval_seconds * 1000,
        )

        provider = MeterProvider(resource=resource, metric_readers=[reader])
        metrics.set_meter_provider(provider)

        self.meter = metrics.get_meter(service_opts.name)

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

    def _record_counter(
        self, name: str, value: float, labels: dict[str, Any] | None = None
    ):
        """Record a counter metric"""
        # OTEL counter
        if self.meter:
            if name not in self._counters:
                self._counters[name] = self.meter.create_counter(name)
            self._counters[name].add(value, attributes=labels or {})

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
            if name not in self._histograms:
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

                self._histograms[name] = self.meter.create_histogram(
                    name,
                    description=(field_info.description or "") if field_info else "",
                    **kwargs,
                )
            self._histograms[name].record(value, attributes=labels or {})

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
        """Record a gauge metric using UpDownCounter (matches Go implementation)"""
        # OTEL gauge (using UpDownCounter like Go does)
        if self.meter:
            if name not in self._gauges:
                # Create the UpDownCounter instrument
                self._gauges[name] = self.meter.create_up_down_counter(
                    name, unit="1", description=f"Gauge metric: {name}"
                )

            # Create a unique key for tracking this gauge instance with its labels
            label_key = name
            if labels:
                # Sort labels to ensure consistent key regardless of dictionary order
                label_key = f"{name}:{sorted(labels.items())}"

            if label_key not in self._gauge_values:
                self._gauge_values[label_key] = 0.0

            # Calculate delta to reach the target value
            current_value = self._gauge_values[label_key]
            delta = value - current_value

            # Add the delta to reach the new value
            self._gauges[name].add(delta, attributes=labels or {})

            # Update tracked value
            self._gauge_values[label_key] = value

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

    def close(self):
        """Close metrics system"""
        pass
