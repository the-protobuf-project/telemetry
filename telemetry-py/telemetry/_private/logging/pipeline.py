"""The log pipeline for free-threaded Python: per-thread buffers to OTLP bytes.

On a free-threaded build every thread writes at once, and any object they all
touch — a shared queue, an enum member, the OpenTelemetry context lookup —
makes them wait on each other: the SDK's record path ran a third as fast on 14
threads as on one. Here each thread appends plain tuples to a buffer of its
own, found through a context variable; a full buffer goes to the exporters as
one batch, so the threads meet once per batch rather than once per record.
The exporters encode the tuples straight to OTLP bytes (otlp_wire) and send
them over HTTP.

Every record is still counted: emitted, exported, failed, dropped; with
``lossless`` a writer waits for room instead of its batch being dropped.
"""

import contextvars
import threading
import time
from collections import deque

from opentelemetry import context as otel_context
from opentelemetry import trace
from opentelemetry.sdk._logs import LogRecordProcessor
from opentelemetry.sdk.util.instrumentation import InstrumentationScope

from ...options import ExportOptions
from .. import otlp_wire
from ..accounting import LOGS, Counters
from ..exporters import fast_log_client


class _Buffer:
    """One thread's records not yet handed to the exporters."""

    __slots__ = ("lock", "items", "ident")

    def __init__(self, ident: int) -> None:
        self.lock = threading.Lock()
        self.items: list = []
        self.ident = ident


def _span_context_reader():
    """A function returning the current span's (trace id, span id, flags), or
    None: through OpenTelemetry's own context variable when it can be found,
    since the context API's lookup is shared by every thread."""
    runtime = getattr(otel_context, "_RUNTIME_CONTEXT", None)
    var = getattr(runtime, "_current_context", None)
    key = getattr(trace, "_SPAN_KEY", None)
    if not isinstance(var, contextvars.ContextVar) or key is None:

        def slow():
            sc = trace.get_current_span().get_span_context()
            return (
                (sc.trace_id, sc.span_id, int(sc.trace_flags)) if sc.is_valid else None
            )

        return slow

    get = var.get

    def fast():
        ctx = get(None)
        span = ctx.get(key) if ctx else None
        if span is None:
            return None
        sc = span.get_span_context()
        return (sc.trace_id, sc.span_id, int(sc.trace_flags)) if sc.is_valid else None

    return fast


class LogPipeline(LogRecordProcessor):
    """Buffers, batches and exports the logger's records; added to the logger
    provider so the provider's flush and shutdown reach it. Records the SDK
    itself emits (the stdlib logging bridge) go through the provider's other
    processors, not here."""

    def __init__(
        self,
        resource,
        scope_name: str,
        endpoint: str,
        headers: dict,
        export: ExportOptions,
    ) -> None:
        self._resource = resource
        self._scope = InstrumentationScope(scope_name)
        self._batch = max(1, min(export.batch_size, export.queue_size))
        self._queue = export.queue_size
        self._lossless = export.lossless
        self._interval = 1.0
        self._current = _span_context_reader()
        self._mine = contextvars.ContextVar(
            f"telemetry-log-buffer-{id(self)}", default=None
        )
        self._buffers: list[_Buffer] = []
        self._buffers_lock = threading.Lock()
        self._room = threading.Condition()
        self._ready: deque[list] = deque()
        self._pending = 0  # records in ready batches and in exports under way
        self._closed = False
        self._stopped = False
        self._counters: list[Counters] = []
        self._threads = []
        for _ in range(max(1, export.exporters)):
            counters = LOGS.part()
            client = fast_log_client(endpoint, headers, export.timeout)
            self._counters.append(counters)
            t = threading.Thread(
                target=self._export_loop, args=(client, counters), daemon=True
            )
            t.start()
            self._threads.append(t)
        self._emitted = self._counters[0]  # every record is counted emitted here
        self._ticker = threading.Thread(target=self._tick, daemon=True)
        self._ticker.start()

    # The writer side.

    def _buffer(self) -> _Buffer:
        """This thread's buffer. Threads may inherit a copy of their creator's
        context, and with it the creator's buffer; the thread id tells."""
        buf = self._mine.get()
        ident = threading.get_ident()
        if buf is None or buf.ident != ident:
            buf = _Buffer(ident)
            with self._buffers_lock:
                self._buffers.append(buf)
            self._mine.set(buf)
        return buf

    def emit(
        self, ts: int, severity: int, severity_text: str, body, attrs: dict | None
    ) -> None:
        span = self._current()
        rec = (ts, severity, severity_text, body, attrs, *(span or (0, 0, 0)))
        buf = self._buffer()
        with buf.lock:
            buf.items.append(rec)
            if len(buf.items) < self._batch:
                return
            batch, buf.items = buf.items, []
        self._hand_off(batch)

    def _hand_off(self, batch: list) -> None:
        """Queues a batch for the exporters: waiting for room when lossless,
        dropping it otherwise."""
        n = len(batch)
        with self._room:
            while (
                self._pending + n > self._queue
                and self._lossless
                and not self._closed
                and self._pending
            ):
                self._room.wait()
            self._emitted.add_emitted(n)
            if self._stopped or (
                self._pending + n > self._queue and not self._lossless and self._pending
            ):
                return  # dropped: emitted and never exported
            self._ready.append(batch)
            self._pending += n
            self._room.notify_all()

    # The exporter side.

    def _export_loop(self, client, counters: Counters) -> None:
        while True:
            with self._room:
                while not self._ready and not self._stopped:
                    self._room.wait()
                if not self._ready:
                    return
                batch = self._ready.popleft()
            ok = False
            try:
                ok = client(
                    otlp_wire.encode_log_tuples(self._resource, self._scope, batch)
                )
            except (
                Exception
            ):  # an export that raised failed; the records are counted so
                ok = False
            counters.add_result(len(batch), ok)
            with self._room:
                self._pending -= len(batch)
                self._room.notify_all()

    def _collect(self) -> None:
        """Hands every thread's partly filled buffer to the exporters."""
        with self._buffers_lock:
            buffers = list(self._buffers)
        for buf in buffers:
            with buf.lock:
                batch, buf.items = buf.items, []
            if batch:
                self._hand_off(batch)

    def _tick(self) -> None:
        while not self._closed:
            time.sleep(self._interval)
            self._collect()

    # The provider side.

    def on_emit(self, log_record) -> None:
        pass

    def force_flush(self, timeout_millis: int = 30000) -> bool:
        self._collect()
        deadline = time.monotonic() + timeout_millis / 1000
        with self._room:
            while self._pending and time.monotonic() < deadline:
                self._room.wait(0.05)
            return not self._pending

    def shutdown(self) -> None:
        if self._closed:
            return
        self.force_flush()
        self._closed = True
        with self._room:
            self._stopped = True
            self._room.notify_all()
        for t in self._threads:
            t.join(timeout=5)
