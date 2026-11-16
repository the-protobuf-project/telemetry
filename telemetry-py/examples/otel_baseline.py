"""
Writes the load examples/loadgen.py writes — n log records, spans or counter
increments — through the upstream OpenTelemetry Python SDK alone, with its
batch processors and OTLP exporters, and prints one JSON line with how long it
took. bench/bench_all.py runs it beside loadgen.py to compare this SDK with
plain OpenTelemetry. The batch processors read the standard OTEL_BSP_* and
OTEL_BLRP_* variables, which is how the bench tunes them;
OTEL_BASELINE_WORKERS sets the threads writing and OTEL_BASELINE_HTTP=1
exports over OTLP/HTTP (the endpoint is then the HTTP port).

Run with:
    uv run python examples/otel_baseline.py <signal> <n> <host:port>
"""

import json
import os
import sys
import threading
import time

from opentelemetry._logs import LogRecord, SeverityNumber
from opentelemetry.sdk._logs import LoggerProvider
from opentelemetry.sdk._logs.export import BatchLogRecordProcessor
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.metrics.export import PeriodicExportingMetricReader
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor


def main() -> None:
    signal = sys.argv[1] if len(sys.argv) > 1 else "logs"
    n = int(sys.argv[2]) if len(sys.argv) > 2 else 1_000_000
    endpoint = sys.argv[3] if len(sys.argv) > 3 else "127.0.0.1:4317"
    workers = max(1, int(os.environ.get("OTEL_BASELINE_WORKERS", "1")))
    if os.environ.get("OTEL_BASELINE_HTTP") == "1":
        from opentelemetry.exporter.otlp.proto.http._log_exporter import OTLPLogExporter
        from opentelemetry.exporter.otlp.proto.http.metric_exporter import (
            OTLPMetricExporter,
        )
        from opentelemetry.exporter.otlp.proto.http.trace_exporter import (
            OTLPSpanExporter,
        )

        def exporter(cls, path):
            return cls(endpoint=f"http://{endpoint}{path}")
    else:
        from opentelemetry.exporter.otlp.proto.grpc._log_exporter import OTLPLogExporter
        from opentelemetry.exporter.otlp.proto.grpc.metric_exporter import (
            OTLPMetricExporter,
        )
        from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import (
            OTLPSpanExporter,
        )

        def exporter(cls, path):
            return cls(endpoint=endpoint, insecure=True)

    if signal == "logs":
        provider = LoggerProvider()
        provider.add_log_record_processor(
            BatchLogRecordProcessor(exporter(OTLPLogExporter, "/v1/logs"))
        )
        logger = provider.get_logger("otel-baseline")

        def emit(i: int) -> None:
            now = time.time_ns()
            logger.emit(
                LogRecord(
                    timestamp=now,
                    observed_timestamp=now,
                    severity_number=SeverityNumber.INFO,
                    severity_text="INFO",
                    body="load",
                    attributes={"i": i},
                )
            )

    elif signal == "spans":
        provider = TracerProvider()
        provider.add_span_processor(
            BatchSpanProcessor(exporter(OTLPSpanExporter, "/v1/traces"))
        )
        tracer = provider.get_tracer("otel-baseline")

        def emit(i: int) -> None:
            with tracer.start_as_current_span("load.op"):
                pass

    elif signal == "metrics":
        provider = MeterProvider(
            metric_readers=[
                PeriodicExportingMetricReader(
                    exporter(OTLPMetricExporter, "/v1/metrics")
                )
            ]
        )
        counter = provider.get_meter("otel-baseline").create_counter("ops")

        def emit(i: int) -> None:
            counter.add(1.0)

    else:
        raise SystemExit(f"unknown signal {signal}")

    start = time.perf_counter()
    per = n // workers
    threads = []
    for w in range(workers):
        count = n - per * (workers - 1) if w == workers - 1 else per
        threads.append(
            threading.Thread(target=lambda c=count: [emit(i) for i in range(c)])
        )
    for th in threads:
        th.start()
    for th in threads:
        th.join()
    emitted = time.perf_counter() - start

    start = time.perf_counter()
    provider.shutdown()
    closed = time.perf_counter() - start

    print(
        json.dumps(
            {
                "lang": "python",
                "signal": signal,
                "n": n,
                "workers": workers,
                "emit_s": emitted,
                "close_s": closed,
            }
        )
    )


if __name__ == "__main__":
    main()
