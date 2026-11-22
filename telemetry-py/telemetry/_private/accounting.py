"""Accounting for every log record and span handed to OTLP export.

The SDK's batch processors drop what does not fit in their queue (2048 by
default) and say so only once, through Python's logging, so a burst of a
million spans could lose half of them unnoticed. These wrappers count what was
emitted, exported and failed per signal; what was emitted and never left the
process by close was dropped, and ``Telemetry.close()`` warns about it. With
``ExportOptions.lossless`` a writer waits while the queue is full instead.

The OpenTelemetry tracer and logger providers are process-wide, so the
counters are too: ``LOGS`` and ``SPANS``.
"""

import itertools
import threading
import time
from collections.abc import Sequence
from dataclasses import dataclass

from opentelemetry.instrumentation.utils import suppress_instrumentation
from opentelemetry.sdk._logs import LogRecordProcessor
from opentelemetry.sdk._logs.export import LogRecordExporter, LogRecordExportResult
from opentelemetry.sdk.trace import SpanProcessor
from opentelemetry.sdk.trace.export import SpanExporter, SpanExportResult


@dataclass(frozen=True)
class SignalStats:
    """What happened to one signal's records."""

    emitted: int = 0  # handed to the pipeline by the program
    exported: int = 0  # accepted by the collector
    failed: int = 0  # in an export request that failed
    pending: int = 0  # not exported yet: queued while running, dropped once closed

    @property
    def dropped(self) -> int:
        """Records that never left the process: meaningful after close."""
        return self.pending


class Counters:
    """Counters for one signal, shared by its processor and exporter.

    With several exporters each has its own (``part()``) and the signal's
    stats add them up, so writer threads on different exporters do not
    contend for one lock: without the GIL they run at once.
    """

    def __init__(self) -> None:
        self._lock = threading.Lock()
        self.emitted = 0
        self.exported = 0
        self.failed = 0
        self._parts: list[Counters] = []

    def part(self) -> "Counters":
        """New counters for one exporter, counted in this signal's stats."""
        c = Counters()
        with self._lock:
            self._parts.append(c)
        return c

    def pending(self) -> int:
        return self.emitted - self.exported - self.failed

    def stats(self) -> SignalStats:
        with self._lock:
            emitted, exported, failed = self.emitted, self.exported, self.failed
            parts = list(self._parts)
        for p in parts:
            s = p.stats()
            emitted, exported, failed = (
                emitted + s.emitted,
                exported + s.exported,
                failed + s.failed,
            )
        return SignalStats(emitted, exported, failed, emitted - exported - failed)

    def add_emitted(self, n: int = 1) -> None:
        with self._lock:
            self.emitted += n

    def add_result(self, n: int, ok: bool) -> None:
        with self._lock:
            if ok:
                self.exported += n
            else:
                self.failed += n


LOGS = Counters()
SPANS = Counters()


class _Gate:
    """Holds a writer back while the queue behind it is full (lossless)."""

    def __init__(self, lossless: bool, queue_size: int) -> None:
        self.lossless = lossless
        # Headroom below the queue size for writers racing past the check.
        self.capacity = max(1, queue_size - min(queue_size // 4, 1024))
        self.closed = False

    def wait(self, counters: Counters) -> None:
        if not self.lossless:
            return
        backoff = 0.000_001
        while counters.pending() >= self.capacity and not self.closed:
            time.sleep(backoff)
            backoff = min(backoff * 2, 0.001)


class CountingSpanExporter(SpanExporter):
    """Counts what the wrapped exporter exports and what fails."""

    def __init__(self, inner: SpanExporter, counters: Counters = SPANS) -> None:
        self._inner = inner
        self._counters = counters

    def export(self, spans: Sequence) -> SpanExportResult:
        # The export's own HTTP or gRPC request is not traced.
        with suppress_instrumentation():
            result = self._inner.export(spans)
        self._counters.add_result(len(spans), result == SpanExportResult.SUCCESS)
        return result

    def shutdown(self) -> None:
        self._inner.shutdown()

    def force_flush(self, timeout_millis: int = 30000) -> bool:
        return self._inner.force_flush(timeout_millis)


class CountingLogExporter(LogRecordExporter):
    """Counts what the wrapped exporter exports and what fails."""

    def __init__(self, inner: LogRecordExporter, counters: Counters = LOGS) -> None:
        self._inner = inner
        self._counters = counters

    def export(self, batch: Sequence) -> LogRecordExportResult:
        with suppress_instrumentation():
            result = self._inner.export(batch)
        self._counters.add_result(len(batch), result == LogRecordExportResult.SUCCESS)
        return result

    def shutdown(self) -> None:
        self._inner.shutdown()

    def force_flush(self, timeout_millis: int = 30000) -> bool:
        return self._inner.force_flush(timeout_millis)


class AccountedSpanProcessor(SpanProcessor):
    """In front of the SDK's batch processor: counts, and waits when lossless."""

    def __init__(
        self,
        inner: SpanProcessor,
        lossless: bool,
        queue_size: int,
        counters: Counters = SPANS,
    ) -> None:
        self._inner = inner
        self._counters = counters
        self._gate = _Gate(lossless, queue_size)

    def on_start(self, span, parent_context=None) -> None:
        self._inner.on_start(span, parent_context=parent_context)

    def on_end(self, span) -> None:
        self._gate.wait(self._counters)
        self._counters.add_emitted()
        self._inner.on_end(span)

    def shutdown(self) -> None:
        self._gate.closed = True
        self._inner.shutdown()

    def force_flush(self, timeout_millis: int = 30000) -> bool:
        return self._inner.force_flush(timeout_millis)


class AccountedLogProcessor(LogRecordProcessor):
    """In front of the SDK's batch processor: counts, and waits when lossless."""

    def __init__(
        self,
        inner: LogRecordProcessor,
        lossless: bool,
        queue_size: int,
        counters: Counters = LOGS,
    ) -> None:
        self._inner = inner
        self._counters = counters
        self._gate = _Gate(lossless, queue_size)

    def on_emit(self, log_record) -> None:
        self._gate.wait(self._counters)
        self._counters.add_emitted()
        self._inner.on_emit(log_record)

    def shutdown(self) -> None:
        self._gate.closed = True
        self._inner.shutdown()

    def force_flush(self, timeout_millis: int = 30000) -> bool:
        return self._inner.force_flush(timeout_millis)


class ShardedSpanProcessor(SpanProcessor):
    """Spreads spans over several processors, each with its own exporter,
    round robin: one exporter sends one batch at a time, so n of them send n
    batches at once, on separate cores when the GIL is off."""

    def __init__(self, shards: list[SpanProcessor]) -> None:
        self._shards = shards
        self._next = itertools.count()

    def on_start(self, span, parent_context=None) -> None:
        pass

    def on_end(self, span) -> None:
        self._shards[next(self._next) % len(self._shards)].on_end(span)

    def shutdown(self) -> None:
        for p in self._shards:
            p.shutdown()

    def force_flush(self, timeout_millis: int = 30000) -> bool:
        return all([p.force_flush(timeout_millis) for p in self._shards])


class ShardedLogProcessor(LogRecordProcessor):
    """Spreads log records over several processors; see ShardedSpanProcessor."""

    def __init__(self, shards: list[LogRecordProcessor]) -> None:
        self._shards = shards
        self._next = itertools.count()

    def on_emit(self, log_record) -> None:
        self._shards[next(self._next) % len(self._shards)].on_emit(log_record)

    def shutdown(self) -> None:
        for p in self._shards:
            p.shutdown()

    def force_flush(self, timeout_millis: int = 30000) -> bool:
        return all([p.force_flush(timeout_millis) for p in self._shards])


def loss_report(logs: SignalStats, spans: SignalStats) -> str | None:
    """Describe the records that never reached the collector, or None."""
    parts = []
    for name, s in (("log records", logs), ("spans", spans)):
        if s.dropped:
            parts.append(
                f"{s.dropped} of {s.emitted} {name} dropped (export queue full)"
            )
        if s.failed:
            parts.append(f"{s.failed} of {s.emitted} {name} failed to export")
    if not parts:
        return None
    report = "telemetry: " + "; ".join(parts)
    if logs.dropped + spans.dropped:
        report += "; use with_export(lossless=True) or a larger queue_size to keep them"
    return report
