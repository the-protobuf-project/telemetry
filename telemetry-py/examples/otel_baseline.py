"""
Writes the load examples/loadgen.py writes — n log records, spans or counter
increments — through the upstream OpenTelemetry Python SDK alone, with its
default batch processors and OTLP/gRPC exporters, and prints one JSON line
with how long it took. bench/bench_all.py runs it beside loadgen.py to compare
this SDK with plain OpenTelemetry.

Run with:
    uv run python examples/otel_baseline.py <signal> <n> <host:port>
"""

import json
import sys
import time

from opentelemetry._logs import LogRecord, SeverityNumber
from opentelemetry.exporter.otlp.proto.grpc._log_exporter import OTLPLogExporter
from opentelemetry.exporter.otlp.proto.grpc.metric_exporter import OTLPMetricExporter
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter
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

    if signal == "logs":
        provider = LoggerProvider()
        provider.add_log_record_processor(
            BatchLogRecordProcessor(OTLPLogExporter(endpoint=endpoint, insecure=True))
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
            BatchSpanProcessor(OTLPSpanExporter(endpoint=endpoint, insecure=True))
        )
        tracer = provider.get_tracer("otel-baseline")

        def emit(i: int) -> None:
            with tracer.start_as_current_span("load.op"):
                pass

    elif signal == "metrics":
        provider = MeterProvider(
            metric_readers=[
                PeriodicExportingMetricReader(
                    OTLPMetricExporter(endpoint=endpoint, insecure=True)
                )
            ]
        )
        counter = provider.get_meter("otel-baseline").create_counter("ops")

        def emit(i: int) -> None:
            counter.add(1.0)

    else:
        raise SystemExit(f"unknown signal {signal}")

    start = time.perf_counter()
    for i in range(n):
        emit(i)
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
                "workers": 1,
                "emit_s": emitted,
                "close_s": closed,
            }
        )
    )


if __name__ == "__main__":
    main()
