"""End to end: logs and spans through the SDK to a real OTLP/HTTP endpoint.

A small collector runs in the test, decodes every request it receives and
counts the records; the SDK must deliver all of them, lossless, over several
exporters, and its own stats must agree. Runs on every supported Python,
free-threaded builds included.
"""

import gzip
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

from opentelemetry.proto.collector.logs.v1.logs_service_pb2 import (
    ExportLogsServiceRequest,
)
from opentelemetry.proto.collector.trace.v1.trace_service_pb2 import (
    ExportTraceServiceRequest,
)

from telemetry import Telemetry

N = 5000


class Collector(ThreadingHTTPServer):
    daemon_threads = True

    def __init__(self) -> None:
        super().__init__(("127.0.0.1", 0), _Handler)
        self.lock = threading.Lock()
        self.logs = 0
        self.spans = 0
        self.log_bodies: set[str] = set()
        self.log_ids: dict[str, tuple[bytes, bytes]] = {}
        self.span_ids: dict[str, tuple[bytes, bytes]] = {}


class _Handler(BaseHTTPRequestHandler):
    server: Collector

    def do_POST(self) -> None:  # noqa: N802 - the http.server hook
        body = self.rfile.read(int(self.headers["Content-Length"]))
        if self.headers.get("Content-Encoding") == "gzip":
            body = gzip.decompress(body)
        with self.server.lock:
            if self.path == "/v1/logs":
                for rl in ExportLogsServiceRequest.FromString(body).resource_logs:
                    for sl in rl.scope_logs:
                        self.server.logs += len(sl.log_records)
                        for r in sl.log_records:
                            self.server.log_bodies.add(r.body.string_value)
                            self.server.log_ids[r.body.string_value] = (
                                r.trace_id,
                                r.span_id,
                            )
            elif self.path == "/v1/traces":
                for rs in ExportTraceServiceRequest.FromString(body).resource_spans:
                    for ss in rs.scope_spans:
                        self.server.spans += len(ss.spans)
                        for sp in ss.spans:
                            self.server.span_ids[sp.name] = (sp.trace_id, sp.span_id)
        self.send_response(200)
        self.send_header("Content-Type", "application/x-protobuf")
        self.send_header("Content-Length", "0")
        self.end_headers()

    def log_message(self, *args) -> None:
        pass


def test_every_log_and_span_reaches_the_collector():
    collector = Collector()
    threading.Thread(target=collector.serve_forever, daemon=True).start()
    try:
        t = (
            Telemetry.new()
            .with_service("e2e-py", "1.0.0")
            .with_otlp("127.0.0.1", collector.server_address[1])
            .with_otlp_http()
            .with_tracing()
            .with_console(False)
            .with_export(lossless=True, queue_size=512, batch_size=128, exporters=2)
            .build()
        )

        def write(worker: int) -> None:
            for i in range(N // 4):
                t.logger.info(f"w{worker}-{i}", {"i": i})
                with t.tracing.start_span("e2e.op"):
                    pass

        writers = [threading.Thread(target=write, args=(w,)) for w in range(4)]
        for w in writers:
            w.start()
        for w in writers:
            w.join()
        # Distributed logging: a record written inside a span names it.
        with t.tracing.start_span("e2e.correlated"):
            t.logger.info("correlated")
        t.close()
        stats = t.stats()
    finally:
        collector.shutdown()
        collector.server_close()

    # Each record once: none lost, none sent twice.
    assert collector.logs == len(collector.log_bodies) == N + 1
    assert collector.spans == N + 1
    assert stats["logs"].exported == N + 1 and stats["logs"].dropped == 0
    assert stats["spans"].exported == N + 1 and stats["spans"].dropped == 0
    assert collector.log_ids["correlated"] == collector.span_ids["e2e.correlated"]
    assert collector.log_ids["w0-0"] == (b"", b"")  # written outside any span
