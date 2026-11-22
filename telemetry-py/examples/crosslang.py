"""
The billing service of the cross-language tracing demo (`just crosslang`):
a WSGI app behind `tel.wsgi_middleware`, which continues the trace the caller
sent, calling the next service with plain urllib, which Telemetry has
instrumented to carry it on. Neither side has an inject or extract of its own.

It listens on a free port and prints {"listening": <port>}; GET /hop answers
with this hop's view of the trace, followed by the hops after it
(CROSSLANG_NEXT, a URL, when there is one). Set
TELEMETRY_DEMO_OTLP=localhost:4317 to export the spans to a collector.

Run with:
    CROSSLANG_NEXT=http://127.0.0.1:8081 uv run python examples/crosslang.py
"""

import json
import os
import sys
import urllib.request
from socketserver import ThreadingMixIn
from wsgiref.simple_server import WSGIRequestHandler, WSGIServer, make_server

from opentelemetry import trace

from telemetry import Telemetry


class Server(ThreadingMixIn, WSGIServer):
    daemon_threads = True


class Quiet(WSGIRequestHandler):
    def log_message(self, *args) -> None:
        pass


def main() -> None:
    host, _, port = os.environ.get("TELEMETRY_DEMO_OTLP", "127.0.0.1:4317").rpartition(
        ":"
    )
    tel = (
        Telemetry.new()
        .with_service("billing-py", "1.0.0")
        .with_otlp(host, int(port))
        .with_tracing()
        .with_console(False)
        .build()
    )
    next_url = os.environ.get("CROSSLANG_NEXT")

    def hop(environ, start_response):
        # The middleware's server span, which continues the caller's trace.
        span = trace.get_current_span().get_span_context()
        hops = [
            {
                "service": "billing-py",
                "received_traceparent": environ.get("HTTP_TRACEPARENT"),
                "trace_id": format(span.trace_id, "032x"),
                "span_id": format(span.span_id, "016x"),
            }
        ]
        if next_url:
            # urllib is instrumented: the request carries this server span on.
            with urllib.request.urlopen(f"{next_url}/hop") as res:
                hops += json.load(res)
        body = json.dumps(hops).encode()
        start_response(
            "200 OK",
            [("Content-Type", "application/json"), ("Content-Length", str(len(body)))],
        )
        return [body]

    server = make_server(
        "127.0.0.1", 0, tel.wsgi_middleware(hop), Server, handler_class=Quiet
    )
    print(json.dumps({"listening": server.server_address[1]}), flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        tel.close()
        sys.exit(0)


if __name__ == "__main__":
    main()
