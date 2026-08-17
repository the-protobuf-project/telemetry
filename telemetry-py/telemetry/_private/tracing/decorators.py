"""Tracing decorators for automatic span creation and event tracking.

This module provides decorator-based tracing that automatically creates spans
and tracks function execution as events, eliminating the need for manual
span.add_event() calls.

Typical usage example:

    import telemetry

    @telemetry.trace("process_data", auto_events=True)
    def process_data(data):
        # Function execution is automatically tracked
        return result
"""

import functools
import time
from collections.abc import Callable
from contextvars import ContextVar
from typing import TYPE_CHECKING, Any

if TYPE_CHECKING:
    from .tracing import TelemetryTracing

# Global context variable to store the current Telemetry instance
_current_telemetry: ContextVar[Any | None] = ContextVar(
    "current_telemetry", default=None
)


def trace(
    name: str | None = None,
    attributes: dict[str, Any] | None = None,
    auto_events: bool = True,
):
    """
    Create a decorator that traces function execution with an automatically managed span.

    Parameters:
        name (Optional[str]): Span name; defaults to the wrapped function's name.
        attributes (Optional[Dict[str, Any]]): Attributes to attach to the span.
        auto_events (bool): Whether to record start, completion, and failure events.

    Returns:
        A decorator that wraps a function with tracing.
    """

    def decorator(func: Callable) -> Callable:
        """
        Wrap a function with optional telemetry span tracing and lifecycle events.

        Returns:
                Callable: The wrapped function.
        """
        span_name = name or func.__name__

        @functools.wraps(func)
        def wrapper(*args, **kwargs):
            """
            Executes the wrapped function within a telemetry span when tracing is enabled.

            Returns:
                result (Any): The wrapped function's result.
            """
            # Get Telemetry instance from context
            telemetry_instance = _current_telemetry.get()

            if not telemetry_instance or not telemetry_instance.tracing.enabled:
                return func(*args, **kwargs)

            tracing = telemetry_instance.tracing

            # Build attributes
            span_attrs = attributes.copy() if attributes else {}

            # Create span
            with tracing.start_span(span_name, span_attrs) as span:
                if auto_events:
                    span.add_event(f"{span_name}_started")

                try:
                    result = func(*args, **kwargs)

                    if auto_events:
                        span.add_event(f"{span_name}_completed")

                    return result
                except Exception as e:
                    if auto_events:
                        span.add_event(f"{span_name}_failed", {"error": str(e)})
                    raise

        return wrapper

    return decorator


# Alias for backwards compatibility
def traced(
    name: str | None = None,
    attributes: dict[str, Any] | None = None,
    auto_events: bool = True,
):
    """
    Create a tracing decorator using the legacy `traced` name.

    Args:
        name: Optional span name; defaults to the decorated function's name.
        attributes: Optional span attributes.
        auto_events: Whether to record start and completion events automatically.

    Returns:
        A decorator that traces the decorated function.
    """
    return trace(name, attributes, auto_events)


def set_current_telemetry(telemetry_instance):
    """Set the current Telemetry instance for decorator context.

    This is called automatically by the Telemetry context manager.

    Args:
        telemetry_instance: The Telemetry instance to set as current.

    Returns:
        A token that can be used to reset the context.
    """
    return _current_telemetry.set(telemetry_instance)


def reset_current_telemetry(token):
    """Reset the current Telemetry instance.

    Args:
        token: Token returned from set_current_telemetry.
    """
    _current_telemetry.reset(token)


def trace_step(event_name: str):
    """
    Mark a function as a traced step within a larger operation.

    Args:
        event_name: Name assigned to the traced step.

    Returns:
        A decorator that preserves the wrapped function's behavior and records the step name as metadata.
    """

    def decorator(func: Callable) -> Callable:
        """Decorator that wraps a function as a traced step."""

        @functools.wraps(func)
        def wrapper(*args, **kwargs):
            """Wrapper that adds event tracking."""
            # Try to get tracing from context
            # Note: tracing context is checked but actual event is added by caller
            if args and hasattr(args[0], "tracing"):
                _ = args[0].tracing  # noqa: F841

            result = func(*args, **kwargs)
            return result

        # Store event name as metadata
        wrapper._trace_event_name = event_name
        return wrapper

    return decorator


class TracedOperation:
    """Context manager for traced operations with automatic event tracking.

    This provides a cleaner API for creating spans with automatic event
    tracking for sub-operations.

    Example:
        with TracedOperation(telemetry.tracing, "process_pipeline") as op:
            op.step("validate_input")
            result = validate(data)

            op.step("transform_data")
            transformed = transform(result)

            op.step("save_output")
            save(transformed)
    """

    def __init__(
        self,
        tracing: "TelemetryTracing",
        name: str,
        attributes: dict[str, Any] | None = None,
    ):
        """Initialize the traced operation.

        Args:
            tracing: TelemetryTracing instance.
            name: Name of the operation.
            attributes: Optional span attributes.
        """
        self.tracing = tracing
        self.name = name
        self.attributes = attributes or {}
        self.span = None
        self.start_time = None

    def __enter__(self):
        """Enter the traced operation context."""
        self.span = self.tracing.start_span(self.name, self.attributes)
        self.span.__enter__()
        self.start_time = time.time()
        self.span.add_event(f"{self.name}_started")
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        """Exit the traced operation context."""
        if exc_type:
            self.span.add_event(f"{self.name}_failed", {"error": str(exc_val)})
        else:
            elapsed_ms = (time.time() - self.start_time) * 1000
            self.span.add_event(f"{self.name}_completed", {"duration_ms": elapsed_ms})

        self.span.__exit__(exc_type, exc_val, exc_tb)
        return False

    def step(self, event_name: str, attributes: dict[str, Any] | None = None):
        """Add a step event to the operation.

        Args:
            event_name: Name of the step/event.
            attributes: Optional event attributes.
        """
        if self.span:
            self.span.add_event(event_name, attributes)

    def set_attribute(self, key: str, value: Any):
        """Set an attribute on the operation's span.

        Args:
            key: Attribute key.
            value: Attribute value.
        """
        if self.span:
            self.span.set_attribute(key, value)

    def add_event(self, name: str, attributes: dict[str, Any] | None = None):
        """Add an event to the operation's span.

        Args:
            name: Event name.
            attributes: Optional event attributes.
        """
        if self.span:
            self.span.add_event(name, attributes)
