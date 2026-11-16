"""
A hop of the cross-language tracing demo (bench/crosslang-demo.sh): continues,
in Python, the trace an upstream service started, from the W3C headers it
sent, and hands it on.

The headers arrive on stdin as a JSON object; in a real service they are the
incoming HTTP headers, gRPC metadata or message headers. Set
TELEMETRY_DEMO_OTLP=localhost:4317 to export the span to a collector.

Run with:
    echo '{"traceparent": "00-…-…-01"}' | uv run python examples/crosslang.py
"""

import json
import os
import sys

from opentelemetry.trace import SpanKind

from telemetry import Telemetry


def main() -> None:
    headers = json.load(sys.stdin)
    host, _, port = os.environ.get("TELEMETRY_DEMO_OTLP", "127.0.0.1:4317").rpartition(":")

    t = (
        Telemetry.new()
        .with_service("billing-py", "1.0.0")
        .with_otlp(host, int(port))
        .with_tracing()
        .build()
    )

    # The one call that joins the caller's trace.
    parent = t.tracing.extract(headers)
    with t.tracing.start_span("billing.charge", parent=parent, kind=SpanKind.SERVER) as span:
        span.span.set_attribute("order.id", "o-42")
        downstream = t.tracing.inject()  # and the next hop continues it in turn
        print(
            json.dumps(
                {
                    "received_traceparent": headers.get("traceparent"),
                    "trace_id": span.trace_id,
                    "span_id": span.span_id,
                    "downstream_traceparent": downstream.get("traceparent"),
                    "baggage": t.tracing.get_baggage(parent),
                },
                separators=(",", ":"),
            )
        )
    t.close()


if __name__ == "__main__":
    main()
