"""Decorator-based automatic tracing.

This module provides a decorator for automatic function tracing with
OpenTelemetry. The decorator handles span creation, context propagation,
and error recording automatically.

Typical usage example:

    @telemetry.tracing.trace(name="process_data", attributes={"user_id": "123"})
    def process_data(data):
        return processed_result
"""

import functools
import time
from typing import Any, Callable, Dict, Optional, TYPE_CHECKING

from opentelemetry.trace import Status, StatusCode

from .context import get_trace_context, set_trace_context, reset_trace_context

if TYPE_CHECKING:
    from .tracing import TelemetryTracing


def create_trace_decorator(
    tracing: "TelemetryTracing",
    name: Optional[str] = None,
    attributes: Optional[Dict[str, Any]] = None,
) -> Callable:
    """Create a tracing decorator for automatic span management.

    This function returns a decorator that automatically creates spans for
    decorated functions, propagates trace context, and records errors.

    Args:
        tracing: The TelemetryTracing instance.
        name: Optional span name. If not provided, uses function name.
        attributes: Optional dictionary of span attributes.

    Returns:
        A decorator function that can be applied to functions.

    Example:
        @create_trace_decorator(telemetry.tracing, "my_operation")
        def my_function():
            pass
    """

    def decorator(func: Callable) -> Callable:
        """Decorator that wraps a function with tracing.

        Args:
            func: The function to be traced.

        Returns:
            The wrapped function with tracing enabled.
        """
        span_name = name or func.__name__

        @functools.wraps(func)
        def wrapper(*args, **kwargs):
            """Wrapper function that creates a span and executes the function.

            Args:
                *args: Positional arguments for the wrapped function.
                **kwargs: Keyword arguments for the wrapped function.

            Returns:
                The return value of the wrapped function.

            Raises:
                Any exception raised by the wrapped function is re-raised
                after being recorded in the span.
            """
            if not tracing.enabled:
                return func(*args, **kwargs)

            # Get current trace context
            ctx = get_trace_context()
            parent_span_id = ctx.get("span_id")

            # Start span
            if tracing.tracer:
                with tracing.tracer.start_as_current_span(span_name) as span:
                    # Set attributes
                    if attributes:
                        for key, value in attributes.items():
                            span.set_attribute(key, value)

                    # Get span context
                    span_ctx = span.get_span_context()
                    current_trace_id = format(span_ctx.trace_id, "032x")
                    current_span_id = format(span_ctx.span_id, "016x")

                    # Update context for nested calls
                    new_ctx = {
                        "trace_id": current_trace_id,
                        "span_id": current_span_id,
                    }
                    token = set_trace_context(new_ctx)

                    try:
                        # Write to MCAP
                        if tracing.mcap_writer and not tracing.mcap_writer.is_closed():
                            tracing.mcap_writer.write_trace(
                                trace_id=current_trace_id,
                                span_id=current_span_id,
                                name=span_name,
                                parent_span_id=parent_span_id,
                                attributes=attributes or {},
                                timestamp=time.time_ns(),
                            )

                        # Execute function
                        result = func(*args, **kwargs)

                        # Mark as successful
                        span.set_status(Status(StatusCode.OK))
                        return result

                    except Exception as e:
                        # Record error
                        span.record_exception(e)
                        span.set_status(Status(StatusCode.ERROR, str(e)))
                        raise

                    finally:
                        # Restore context
                        reset_trace_context(token)
            else:
                # No OTEL tracer, just execute function
                return func(*args, **kwargs)

        return wrapper

    return decorator
