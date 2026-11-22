"""Trace propagation over HTTP and gRPC.

Every request the process makes carries the W3C trace (traceparent,
tracestate, baggage) and every request it answers continues the caller's, so
a trace runs end to end across services and languages.

The clients are patched where every request goes through, so they are traced
however the program imported them: ``requests`` (Session.send), ``httpx``
(its transports), ``urllib`` (OpenerDirector), ``urllib3`` (connection
pools), ``aiohttp`` (ClientSession) and gRPC channels and servers, sync and
asyncio. Each is instrumented only when its library is installed. Web
servers are wrapped once, at the app: ``Telemetry.asgi_middleware`` and
``Telemetry.wsgi_middleware``.

The instrumentors use the global tracer and meter providers, so with nothing
exporting they still pass a caller's trace on: the no-op tracer keeps the
parent's span context.
"""

import importlib
import importlib.util
import logging
import threading
from typing import Any

_log = logging.getLogger(__name__)

# (library it traces, instrumentor module, instrumentor class)
_INSTRUMENTORS = [
    ("requests", "opentelemetry.instrumentation.requests", "RequestsInstrumentor"),
    ("httpx", "opentelemetry.instrumentation.httpx", "HTTPXClientInstrumentor"),
    ("urllib", "opentelemetry.instrumentation.urllib", "URLLibInstrumentor"),
    ("urllib3", "opentelemetry.instrumentation.urllib3", "URLLib3Instrumentor"),
    (
        "aiohttp",
        "opentelemetry.instrumentation.aiohttp_client",
        "AioHttpClientInstrumentor",
    ),
    ("grpc", "opentelemetry.instrumentation.grpc", "GrpcInstrumentorClient"),
    ("grpc", "opentelemetry.instrumentation.grpc", "GrpcInstrumentorServer"),
    ("grpc", "opentelemetry.instrumentation.grpc", "GrpcAioInstrumentorClient"),
    ("grpc", "opentelemetry.instrumentation.grpc", "GrpcAioInstrumentorServer"),
]

# The patches are process-wide, so the first Telemetry to instrument owns
# them and only it takes them off again.
_lock = threading.Lock()
_owner: object | None = None
_active: list[Any] = []


def instrument(owner: object) -> None:
    """Patches every installed HTTP and gRPC library; a no-op when another
    Telemetry already has."""
    global _owner
    with _lock:
        if _owner is not None:
            return
        _owner = owner
        for library, module, name in _INSTRUMENTORS:
            if importlib.util.find_spec(library) is None:
                continue
            try:
                instrumentor = getattr(importlib.import_module(module), name)()
                if instrumentor.is_instrumented_by_opentelemetry:
                    continue  # the program instrumented it itself
                instrumentor.instrument()
            except Exception as error:  # one library failing must not stop the rest
                _log.debug("telemetry: could not instrument %s: %s", library, error)
                continue
            _active.append(instrumentor)


def uninstrument(owner: object) -> None:
    """Takes the patches off, if this Telemetry put them on."""
    global _owner
    with _lock:
        if _owner is not owner:
            return
        for instrumentor in reversed(_active):
            try:
                instrumentor.uninstrument()
            except Exception as error:
                _log.debug(
                    "telemetry: could not uninstrument %s: %s", instrumentor, error
                )
        _active.clear()
        _owner = None
