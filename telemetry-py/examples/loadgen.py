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
import threading
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
    # LOADGEN_HTTP=1 exports over OTLP/HTTP (the endpoint is then the HTTP
    # port), as free-threaded Python must: grpcio has no build for it.
    if os.environ.get("LOADGEN_HTTP") == "1":
        b = b.with_otlp_http()
    # LOADGEN_CONSOLE=1 also prints every log line, as a service watched in a
    # terminal would; off by default, to measure the pipeline.
    b = b.with_console(os.environ.get("LOADGEN_CONSOLE") == "1")
    if os.environ.get("LOADGEN_MCAP"):
        b = b.with_mcap(os.environ["LOADGEN_MCAP"])
    # LOADGEN_LOSSLESS=1 waits for room instead of dropping; LOADGEN_EXPORTERS
    # sets the exporters per signal and LOADGEN_WORKERS the threads writing,
    # which run in parallel on free-threaded Python.
    exporters = int(os.environ.get("LOADGEN_EXPORTERS", "1"))
    workers = max(1, int(os.environ.get("LOADGEN_WORKERS", "1")))
    if os.environ.get("LOADGEN_LOSSLESS") == "1":
        b = b.with_export(lossless=True, exporters=exporters)
    elif exporters > 1:
        b = b.with_export(exporters=exporters)
    t = b.build()

    def log(i: int) -> None:
        t.logger.info("load", {"i": i})

    def span(i: int) -> None:
        with t.tracing.start_span("load.op"):
            pass

    start = time.perf_counter()
    if signal == "logs":
        fan_out(n, workers, log)
    elif signal == "spans":
        fan_out(n, workers, span)
    elif signal == "metrics":
        fan_out(n, workers, lambda i: t.metrics.counter("ops", 1.0))
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
                "workers": workers,
                "emit_s": emit,
                "close_s": close,
                "dropped_reported": dropped,
            }
        )
    )


def fan_out(n: int, workers: int, fn) -> None:
    """Calls fn n times across workers threads."""
    per = n // workers
    threads = []
    for w in range(workers):
        count = n - per * (workers - 1) if w == workers - 1 else per
        threads.append(
            threading.Thread(target=lambda c=count: [fn(i) for i in range(c)])
        )
    for th in threads:
        th.start()
    for th in threads:
        th.join()


if __name__ == "__main__":
    main()
