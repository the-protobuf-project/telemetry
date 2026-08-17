"""Main tracing client with OpenTelemetry integration.

This module provides the main TelemetryTracing class that integrates decorator-based
tracing, manual span management, and MCAP recording.

Typical usage example:

    telemetry = Telemetry(service_opts, telemetry_opts)

    # Decorator-based tracing
    @telemetry.tracing.trace(name="operation")
    def my_function():
        pass

    # Manual span management
    with telemetry.tracing.start_span("operation") as span:
        span.add_event("checkpoint")
"""

from typing import Any, Callable, Dict, Optional

from opentelemetry import trace
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter
from opentelemetry.sdk.resources import Resource

from ...options import ServiceOptions, TracingOptions, OTLPOptions
from ..foxglove import UnifiedMcapWriter
from .decorator import create_trace_decorator
from .span_context import SpanContext
from .context import get_current_trace_id, get_current_span_id


class TelemetryTracing:
    """Tracing system with decorator support for automatic span management.

    This class provides distributed tracing capabilities using OpenTelemetry,
    with support for both decorator-based automatic tracing and manual span
    management. Traces can be exported to OTLP collectors and recorded to
    MCAP files for visualization.

    Attributes:
        service_opts: Service configuration options.
        tracing_opts: Tracing-specific configuration.
        mcap_writer: Optional MCAP writer for recording traces.
        enabled: Whether tracing is enabled.
        tracer: OpenTelemetry tracer instance (if OTLP is enabled).
    """

    def __init__(
        self,
        service_opts: ServiceOptions,
        tracing_opts: TracingOptions,
        otlp_opts: Optional[OTLPOptions] = None,
        mcap_writer: Optional[UnifiedMcapWriter] = None,
    ):
        """Initialize the tracing system.

        Args:
            service_opts: Service configuration including name, version, environment.
            tracing_opts: Tracing configuration options.
            otlp_opts: Optional OTLP exporter configuration.
            mcap_writer: Optional MCAP writer for recording traces.
        """
        self.service_opts = service_opts
        self.tracing_opts = tracing_opts
        self.mcap_writer = mcap_writer
        self.enabled = tracing_opts.enabled

        # Initialize OpenTelemetry tracing if enabled
        self.tracer = None
        if otlp_opts and otlp_opts.enabled and self.enabled:
            self._setup_otel_tracing(service_opts, otlp_opts)

    def _setup_otel_tracing(self, service_opts: ServiceOptions, otlp_opts: OTLPOptions):
        """Setup OpenTelemetry tracing with OTLP exporter.

        Configures the OpenTelemetry SDK with service metadata and OTLP
        exporter for sending traces to a collector.

        Args:
            service_opts: Service configuration for resource attributes.
            otlp_opts: OTLP exporter configuration (host, port).
        """
        # If a real TracerProvider is already set, reuse it to avoid override warnings
        current = trace.get_tracer_provider()
        if isinstance(current, TracerProvider):
            self.tracer = trace.get_tracer(service_opts.name)
            return

        resource = Resource.create(
            {
                "service.name": service_opts.name,
                "service.version": service_opts.version,
                "service.environment": service_opts.environment.value,
            }
        )

        provider = TracerProvider(resource=resource)

        exporter = OTLPSpanExporter(
            endpoint=f"{otlp_opts.host}:{otlp_opts.port}",
            insecure=True,
        )

        provider.add_span_processor(BatchSpanProcessor(exporter))
        trace.set_tracer_provider(provider)

        self.tracer = trace.get_tracer(service_opts.name)

    def trace(
        self,
        name: Optional[str] = None,
        attributes: Optional[Dict[str, Any]] = None,
    ) -> Callable:
        """Decorator to automatically trace a function.

        This decorator creates a span for each function call, automatically
        propagates trace context to nested calls, and records errors.

        Args:
            name: Optional span name. Defaults to function name.
            attributes: Optional dictionary of span attributes.

        Returns:
            A decorator that can be applied to functions.

        Example:
            @telemetry.tracing.trace(name="process_data", attributes={"user_id": "123"})
            def process_data(data):
                return result
        """
        return create_trace_decorator(self, name, attributes)

    def start_span(
        self, name: str, attributes: Optional[Dict[str, Any]] = None
    ) -> SpanContext:
        """Manually start a span using a context manager.

        This method provides manual control over span creation and lifecycle,
        as opposed to the automatic decorator-based approach.

        Args:
            name: Name of the span/operation.
            attributes: Optional dictionary of span attributes.

        Returns:
            A SpanContext that can be used as a context manager.

        Example:
            with telemetry.tracing.start_span("operation", {"key": "value"}) as span:
                # Do work
                span.add_event("checkpoint")
                span.set_attribute("result", "success")
        """
        return SpanContext(self, name, attributes)

    def get_current_trace_id(self) -> Optional[str]:
        """Get the current trace ID from context.

        Returns:
            The current trace ID as a hex string, or None if not in a trace.
        """
        trace_id = get_current_trace_id()
        return trace_id if trace_id else None

    def get_current_span_id(self) -> Optional[str]:
        """Get the current span ID from context.

        Returns:
            The current span ID as a hex string, or None if not in a span.
        """
        span_id = get_current_span_id()
        return span_id if span_id else None

    def close(self):
        """Close the tracing system.

        Performs cleanup operations. Currently a no-op as OpenTelemetry
        handles cleanup automatically.
        """
        pass
