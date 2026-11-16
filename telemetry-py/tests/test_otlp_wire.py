"""otlp_wire encodes the same requests the OpenTelemetry exporters send."""

from opentelemetry._logs import SeverityNumber
from opentelemetry.exporter.otlp.proto.common._log_encoder import encode_logs
from opentelemetry.exporter.otlp.proto.common.trace_encoder import encode_spans
from opentelemetry.proto.collector.logs.v1.logs_service_pb2 import (
    ExportLogsServiceRequest,
)
from opentelemetry.proto.collector.trace.v1.trace_service_pb2 import (
    ExportTraceServiceRequest,
)
from opentelemetry.sdk._logs import LoggerProvider
from opentelemetry.sdk._logs.export import (
    InMemoryLogRecordExporter,
    SimpleLogRecordProcessor,
)
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter
from opentelemetry.trace import Link, SpanKind, Status, StatusCode

from telemetry._private import otlp_wire

RESOURCE = Resource.create({"service.name": "wire", "service.version": "1.0.0", "n": 3})
VALUES = {
    "s": "text",
    "i": 42,
    "neg": -7,
    "big": 2**62,
    "f": 1.5,
    "b": True,
    "no": False,
    "list": ["a", "b"],
    "ints": [1, 2, 3],
    "unicode": "héllo ✓",
}


def logs():
    exporter = InMemoryLogRecordExporter()
    provider = LoggerProvider(resource=RESOURCE)
    provider.add_log_record_processor(SimpleLogRecordProcessor(exporter))
    logger = provider.get_logger("wire.test", "2.0")
    for i in range(3):
        logger.emit(
            body=f"message {i}",
            severity_number=SeverityNumber.WARN,
            severity_text="WARN",
            attributes=dict(VALUES, i=i),
        )
    logger.emit(
        body={"nested": {"k": 1}, "list": [1, "two"]},
        severity_number=SeverityNumber.INFO,
    )
    logger.emit(body="no attributes")
    return exporter.get_finished_logs()


def spans():
    exporter = InMemorySpanExporter()
    provider = TracerProvider(resource=RESOURCE)
    provider.add_span_processor(SimpleSpanProcessor(exporter))
    tracer = provider.get_tracer("wire.test", "2.0")
    with tracer.start_as_current_span(
        "parent", kind=SpanKind.SERVER, attributes=VALUES
    ) as parent:
        parent.add_event("event", {"e": 1})
        with tracer.start_as_current_span(
            "child",
            kind=SpanKind.CLIENT,
            links=[Link(parent.get_span_context(), {"l": "x"})],
        ) as child:
            child.set_status(Status(StatusCode.ERROR, "broken"))
    with tracer.start_as_current_span("ok") as s:
        s.set_status(Status(StatusCode.OK))
    return exporter.get_finished_spans()


def test_logs_match_the_exporters_encoding():
    batch = logs()
    ours = ExportLogsServiceRequest.FromString(otlp_wire.encode_logs(batch))
    assert ours == encode_logs(batch)


def test_spans_match_the_exporters_encoding():
    batch = spans()
    ours = ExportTraceServiceRequest.FromString(otlp_wire.encode_spans(batch))
    assert ours == encode_spans(batch)


def test_exporters_send_the_wire_encoding(monkeypatch):
    from telemetry._private.exporters import log_exporter, span_exporter

    sent = []

    class Result:
        success = True

    for exporter, batch, decode, expected in (
        (
            log_exporter("127.0.0.1:4318", False, True, {}),
            logs(),
            ExportLogsServiceRequest,
            encode_logs,
        ),
        (
            span_exporter("127.0.0.1:4318", False, True, {}),
            spans(),
            ExportTraceServiceRequest,
            encode_spans,
        ),
    ):
        monkeypatch.setattr(
            exporter._client, "export", lambda data: sent.append(data) or Result()
        )
        assert exporter.export(batch).name == "SUCCESS"
        assert decode.FromString(sent[-1]) == expected(batch)


def test_without_grpc_export_falls_back_to_http(monkeypatch):
    import importlib.util

    from telemetry._private import exporters

    real = importlib.util.find_spec
    monkeypatch.setattr(
        importlib.util,
        "find_spec",
        lambda name, *a: (
            None
            if name.startswith("opentelemetry.exporter.otlp.proto.grpc")
            else real(name, *a)
        ),
    )
    monkeypatch.setattr(exporters, "_warned", True)
    assert exporters._protocol("collector:4317", False) == ("collector:4318", True)
    assert exporters._protocol("collector:9000", False) == ("collector:9000", True)
    assert exporters._protocol("collector:4318", True) == ("collector:4318", True)
