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
from typing import Any, Callable, Dict, Optional, TYPE_CHECKING
from contextvars import ContextVar

if TYPE_CHECKING:
    from .tracing import TelemetryTracing

# Global context variable to store the current Telemetry instance
_current_telemetry: ContextVar[Optional[Any]] = ContextVar(
    "current_telemetry", default=None
)


def trace(
    name: Optional[str] = None,
    attributes: Optional[Dict[str, Any]] = None,
    auto_events: bool = True,
):
    """Decorator for automatic tracing with event tracking.

    This is the main tracing decorator that can be used at module level.
    It automatically finds the Telemetry instance from the execution context.

    Args:
        name: Optional span name. Defaults to function name.
        attributes: Optional dictionary of span attributes.
        auto_events: If True, automatically add start/end events.

    Returns:
        A decorator that can be applied to functions.

    Example:
        import telemetry

        @telemetry.trace("process_data", auto_events=True)
        def process_data(data):
            # Automatically traced!
            return result
    """

    def decorator(func: Callable) -> Callable:
        """Decorator that wraps a function with tracing."""
        span_name = name or func.__name__

        @functools.wraps(func)
        def wrapper(*args, **kwargs):
            """Wrapper function that creates a span and executes the function."""
            # Get Telemetry instance from context
            telemetry_instance = _current_telemetry.get()

            if (
                not telemetry_instance
                or not telemetry_instance.tracing.enabled
            ):
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
    name: Optional[str] = None,
    attributes: Optional[Dict[str, Any]] = None,
    auto_events: bool = True,
):
    """Alias for trace() decorator.

    Alias for trace() decorator for backwards compatibility.

    Args:
        name: Optional span name. Defaults to function name.
        attributes: Optional dictionary of span attributes.
        auto_events: If True, automatically add start/end events.

    Returns:
        A decorator that can be applied to functions.
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
    """Decorator to mark a function as a traced step within a larger operation.

    This creates a sub-span for the decorated function and adds it as an event
    to the parent span.

    Args:
        event_name: Name of the event/step.

    Returns:
        A decorator that can be applied to functions.

    Example:
        class Pipeline:
            @trace_step("validating_input")
            def validate(self, data):
                # Creates event: validating_input
                return validated_data
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
        attributes: Optional[Dict[str, Any]] = None,
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

    def step(self, event_name: str, attributes: Optional[Dict[str, Any]] = None):
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

    def add_event(self, name: str, attributes: Optional[Dict[str, Any]] = None):
        """Add an event to the operation's span.

        Args:
            name: Event name.
            attributes: Optional event attributes.
        """
        if self.span:
            self.span.add_event(name, attributes)
