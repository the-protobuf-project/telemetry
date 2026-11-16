"""The OTLP exporters for each signal, over gRPC or HTTP.

gRPC needs ``grpcio``, which has no build for free-threaded Python (3.14t),
so it is the ``[grpc]`` extra; OTLP/HTTP needs nothing beyond protobuf and
works on every build. Without the extra, gRPC export falls back to HTTP, on
the standard HTTP port when the gRPC one was given.
"""

import importlib.util
import warnings
from typing import Any

from opentelemetry.exporter.otlp.proto.http._log_exporter import (
    OTLPLogExporter as _HTTPLogExporter,
)
from opentelemetry.exporter.otlp.proto.http.trace_exporter import (
    OTLPSpanExporter as _HTTPSpanExporter,
)
from opentelemetry.sdk._logs.export import LogRecordExportResult
from opentelemetry.sdk.trace.export import SpanExportResult

from . import otlp_wire


class _WireLogExporter(_HTTPLogExporter):
    """The OTLP/HTTP log exporter, encoding with otlp_wire rather than protobuf
    message objects; anything unexpected goes the exporter's own way."""

    def export(self, batch) -> LogRecordExportResult:
        client = getattr(self, "_client", None)
        if client is None or self._shutdown:
            return super().export(batch)
        try:
            data = otlp_wire.encode_logs(batch)
        except Exception:  # an unexpected value: the exporter's encoder decides
            return super().export(batch)
        if client.export(data).success:
            return LogRecordExportResult.SUCCESS
        return LogRecordExportResult.FAILURE


class _WireSpanExporter(_HTTPSpanExporter):
    """The OTLP/HTTP span exporter, encoding with otlp_wire; see _WireLogExporter."""

    def export(self, spans) -> SpanExportResult:
        client = getattr(self, "_client", None)
        if client is None or self._shutdown:
            return super().export(spans)
        try:
            data = otlp_wire.encode_spans(spans)
        except Exception:  # an unexpected value: the exporter's encoder decides
            return super().export(spans)
        if client.export(data).success:
            return SpanExportResult.SUCCESS
        return SpanExportResult.FAILURE


def _url(endpoint: str, secure: bool, path: str) -> str:
    base = (
        endpoint
        if "://" in endpoint
        else f"{'https' if secure else 'http'}://{endpoint}"
    )
    return base.rstrip("/") + path


def _grpc(module: str) -> Any:
    return __import__(
        f"opentelemetry.exporter.otlp.proto.grpc.{module}", fromlist=["_"]
    )


_warned = False


def _protocol(endpoint: str, use_http: bool) -> tuple[str, bool]:
    """The endpoint and protocol to export with: gRPC as asked when it is
    installed, otherwise HTTP, moving the standard gRPC port to HTTP's."""
    global _warned
    if use_http or importlib.util.find_spec("opentelemetry.exporter.otlp.proto.grpc"):
        return endpoint, use_http
    moved = (
        endpoint[: -len(":4317")] + ":4318" if endpoint.endswith(":4317") else endpoint
    )
    if not _warned:
        _warned = True
        warnings.warn(
            f"telemetry: OTLP over gRPC needs the grpc extra (pip install 'telemetry-py[grpc]'), "
            f"which free-threaded Python cannot install; exporting over OTLP/HTTP to {moved} instead",
            stacklevel=4,
        )
    return moved, True


def log_exporter(
    endpoint: str,
    secure: bool,
    use_http: bool,
    headers: dict[str, str],
    timeout: float = 30.0,
):
    endpoint, use_http = _protocol(endpoint, use_http)
    if use_http:
        return _WireLogExporter(
            endpoint=_url(endpoint, secure, "/v1/logs"),
            headers=headers or None,
            timeout=timeout,
        )
    return _grpc("_log_exporter").OTLPLogExporter(
        endpoint=endpoint,
        insecure=not secure,
        headers=tuple(headers.items()) or None,
        timeout=timeout,
    )


def span_exporter(
    endpoint: str,
    secure: bool,
    use_http: bool,
    headers: dict[str, str],
    timeout: float = 30.0,
):
    endpoint, use_http = _protocol(endpoint, use_http)
    if use_http:
        return _WireSpanExporter(
            endpoint=_url(endpoint, secure, "/v1/traces"),
            headers=headers or None,
            timeout=timeout,
        )
    return _grpc("trace_exporter").OTLPSpanExporter(
        endpoint=endpoint,
        insecure=not secure,
        headers=tuple(headers.items()) or None,
        timeout=timeout,
    )


def metric_exporter(
    endpoint: str,
    secure: bool,
    use_http: bool,
    headers: dict[str, str],
    timeout: float = 30.0,
):
    endpoint, use_http = _protocol(endpoint, use_http)
    if use_http:
        from opentelemetry.exporter.otlp.proto.http.metric_exporter import (
            OTLPMetricExporter,
        )

        return OTLPMetricExporter(
            endpoint=_url(endpoint, secure, "/v1/metrics"),
            headers=headers or None,
            timeout=timeout,
        )
    return _grpc("metric_exporter").OTLPMetricExporter(
        endpoint=endpoint,
        insecure=not secure,
        headers=tuple(headers.items()) or None,
        timeout=timeout,
    )


def fast_log_client(endpoint: str, headers: dict[str, str], timeout: float = 30.0):
    """A function sending an encoded ExportLogsServiceRequest over OTLP/HTTP
    and returning whether the collector took it: the HTTP log exporter's own
    client, with its retries and headers. Raises AttributeError when the
    exporter has no such client, so the caller uses the exporter instead."""
    exporter = _HTTPLogExporter(
        endpoint=_url(endpoint, False, "/v1/logs"),
        headers=headers or None,
        timeout=timeout,
    )
    client = exporter._client  # noqa: SLF001 - the exporter's transport, shared rather than copied
    return lambda data: client.export(data).success
