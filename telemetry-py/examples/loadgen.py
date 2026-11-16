"""
Writes N log records, spans or counter increments through the Python SDK to
an OTLP collector and prints one JSON line with how long it took. Driven by
bench/bench_all.py, which counts what reached the collector.

Run with:
    uv run python examples/loadgen.py <signal> <n> <host:port>
"""

import json
import os
import sys
import time

from telemetry import Telemetry


def main() -> None:
    signal = sys.argv[1] if len(sys.argv) > 1 else "logs"
    n = int(sys.argv[2]) if len(sys.argv) > 2 else 1_000_000
    host, _, port = (sys.argv[3] if len(sys.argv) > 3 else "127.0.0.1:4317").rpartition(
        ":"
    )

    # LOADGEN_MCAP=path records to MCAP; an endpoint of "none" skips OTLP,
    # to measure MCAP recording alone.
    b = Telemetry.new().with_service("loadgen-py", "1.0.0").with_tracing()
    if host != "" and port != "none":
        b = b.with_otlp(host, int(port))
    if os.environ.get("LOADGEN_MCAP"):
        b = b.with_mcap(os.environ["LOADGEN_MCAP"])
    # LOADGEN_LOSSLESS=1 waits for room instead of dropping.
    if os.environ.get("LOADGEN_LOSSLESS") == "1":
        b = b.with_export(lossless=True)
    t = b.build()

    start = time.perf_counter()
    if signal == "logs":
        for i in range(n):
            t.logger.info("load", {"i": i})
    elif signal == "spans":
        for _ in range(n):
            with t.tracing.start_span("load.op"):
                pass
    elif signal == "metrics":
        for _ in range(n):
            t.metrics.counter("ops", 1.0)
    else:
        raise SystemExit(f"unknown signal {signal}")
    emit = time.perf_counter() - start

    start = time.perf_counter()
    t.close()
    close = time.perf_counter() - start
    stats = t.stats()
    dropped = {"logs": stats["logs"].dropped, "spans": stats["spans"].dropped}.get(
        signal, 0
    )

    print(
        json.dumps(
            {
                "lang": "python",
                "signal": signal,
                "n": n,
                "workers": 1,
                "emit_s": emit,
                "close_s": close,
                "dropped_reported": dropped,
            }
        )
    )


if __name__ == "__main__":
    main()
