"""Trace propagation over HTTP and gRPC, with no inject or extract in the program.

Each scenario runs in a fresh interpreter: OpenTelemetry's tracer provider is
set once per process, and a real service builds Telemetry once at start-up.
A small OTLP/HTTP collector in the scenario records the spans it receives.
"""

import importlib.util
import json
import subprocess
import sys
import textwrap

import pytest

# What another language's SDK sends: a sampled trace this process never saw.
UPSTREAM_TRACE = "0af7651916cd43dd8448eb211c80319c"
UPSTREAM_SPAN = "b7ad6b7169203331"
UPSTREAM = f"00-{UPSTREAM_TRACE}-{UPSTREAM_SPAN}-01"

PRELUDE = '''
import json, threading, urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from opentelemetry.instrumentation.utils import suppress_instrumentation
from opentelemetry.proto.collector.trace.v1.trace_service_pb2 import ExportTraceServiceRequest
from telemetry import Telemetry

spans, seen = [], {}

class Collector(BaseHTTPRequestHandler):
    def do_POST(self):
        body = self.rfile.read(int(self.headers["Content-Length"]))
        if self.path == "/v1/traces":
            for rs in ExportTraceServiceRequest.FromString(body).resource_spans:
                for ss in rs.scope_spans:
                    for s in ss.spans:
                        spans.append({"name": s.name, "kind": s.kind,
                                      "trace": s.trace_id.hex(), "span": s.span_id.hex(),
                                      "parent": s.parent_span_id.hex()})
        self.send_response(200); self.send_header("Content-Length", "0"); self.end_headers()
    def log_message(self, *args): pass

class Recorder(BaseHTTPRequestHandler):
    """A service elsewhere: keeps the traceparent of each request, by path."""
    def do_GET(self):
        seen[self.path] = self.headers.get("traceparent", "")
        self.send_response(200); self.send_header("Content-Length", "2"); self.end_headers()
        self.wfile.write(b"ok")
    def log_message(self, *args): pass

def serve(handler):
    srv = ThreadingHTTPServer(("127.0.0.1", 0), handler)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    return f"127.0.0.1:{srv.server_address[1]}"

collector = serve(Collector)
recorder = serve(Recorder)

def build(**kw):
    b = (Telemetry.new().with_service("py-hop", "1.0.0")
         .with_otlp_endpoint(collector).with_otlp_http().with_console(False))
    if "auto" in kw:
        b = b.with_auto_instrumentation(kw["auto"])
    return b.build()

def get(url, headers=None):
    with urllib.request.urlopen(urllib.request.Request(url, headers=headers or {})) as r:
        r.read()
'''


def run(scenario: str) -> dict:
    """Runs PRELUDE and the scenario in a new interpreter; returns what it prints."""
    code = PRELUDE + textwrap.dedent(scenario) + "\nprint(json.dumps(out))\n"
    done = subprocess.run(
        [sys.executable, "-c", code], capture_output=True, text=True, timeout=60
    )
    assert done.returncode == 0, done.stderr
    return json.loads(done.stdout.strip().splitlines()[-1])


def trace_of(traceparent: str) -> str:
    parts = traceparent.split("-")
    return parts[1] if len(parts) == 4 else ""


SERVER, CLIENT = 2, 3  # OTLP span kinds


def test_every_client_carries_the_active_trace():
    out = run(
        """
        import requests, urllib3
        tel = build()
        with tel.tracing.start_span("checkout") as span:
            get(f"http://{recorder}/urllib")
            requests.get(f"http://{recorder}/requests").raise_for_status()
            urllib3.PoolManager().request("GET", f"http://{recorder}/urllib3")
            trace_id = span.trace_id
        tel.close()
        out = {"trace": trace_id, "seen": seen, "spans": spans}
        """
    )
    for path in ("/urllib", "/requests", "/urllib3"):
        assert trace_of(out["seen"][path]) == out["trace"], path
    clients = [s for s in out["spans"] if s["kind"] == CLIENT]
    # requests sends through urllib3, which does not trace it a second time.
    assert len(clients) == 3, out["spans"]


def test_a_server_continues_another_services_trace_and_hands_it_on():
    out = run(
        f"""
        from wsgiref.simple_server import WSGIRequestHandler, make_server
        tel = build()

        def app(environ, start_response):
            get(f"http://{{recorder}}/next")
            start_response("200 OK", [("Content-Length", "2")])
            return [b"ok"]

        class Quiet(WSGIRequestHandler):
            def log_message(self, *args): pass

        here = make_server("127.0.0.1", 0, tel.wsgi_middleware(app), handler_class=Quiet)
        threading.Thread(target=here.serve_forever, daemon=True).start()
        # As from a service in another language: untraced, its own traceparent.
        with suppress_instrumentation():
            get(f"http://127.0.0.1:{{here.server_address[1]}}/order",
                {{"traceparent": "{UPSTREAM}"}})
        tel.close()
        out = {{"seen": seen, "spans": spans}}
        """
    )
    assert trace_of(out["seen"]["/next"]) == UPSTREAM_TRACE
    server = next(s for s in out["spans"] if s["kind"] == SERVER)
    assert server["trace"] == UPSTREAM_TRACE
    assert server["parent"] == UPSTREAM_SPAN
    client = next(s for s in out["spans"] if s["kind"] == CLIENT)
    assert client["parent"] == server["span"]
    assert out["seen"]["/next"].split("-")[2] == client["span"]


def test_its_own_exports_are_not_traced():
    out = run(
        """
        tel = build()
        tel.logger.info("hello")
        with tel.tracing.start_span("op"):
            pass
        tel.close()
        out = {"names": [s["name"] for s in spans]}
        """
    )
    assert out["names"] == ["op"]


def test_auto_instrumentation_can_be_turned_off():
    out = run(
        """
        tel = build(auto=False)
        with tel.tracing.start_span("checkout"):
            get(f"http://{recorder}/urllib")
        tel.close()
        out = {"seen": seen}
        """
    )
    assert out["seen"]["/urllib"] == ""


@pytest.mark.skipif(
    importlib.util.find_spec("grpc") is None
    or importlib.util.find_spec("grpc_health") is None,
    reason="needs grpcio and grpcio-health-checking",
)
def test_grpc_carries_the_trace():
    out = run(
        """
        from concurrent import futures
        import grpc
        from grpc_health.v1 import health, health_pb2, health_pb2_grpc
        tel = build()
        srv = grpc.server(futures.ThreadPoolExecutor(2))
        health_pb2_grpc.add_HealthServicer_to_server(health.HealthServicer(), srv)
        port = srv.add_insecure_port("127.0.0.1:0")
        srv.start()
        with tel.tracing.start_span("checkout") as span:
            with grpc.insecure_channel(f"127.0.0.1:{port}") as ch:
                health_pb2_grpc.HealthStub(ch).Check(health_pb2.HealthCheckRequest())
            trace_id = span.trace_id
        srv.stop(None)
        tel.close()
        out = {"trace": trace_id, "spans": spans}
        """
    )
    # Python's gRPC instrumentation names the span with the method's leading slash.
    rpc = [s for s in out["spans"] if s["name"].endswith("grpc.health.v1.Health/Check")]
    client = next(s for s in rpc if s["kind"] == CLIENT)
    server = next(s for s in rpc if s["kind"] == SERVER)
    assert client["trace"] == out["trace"]
    assert server["parent"] == client["span"]
