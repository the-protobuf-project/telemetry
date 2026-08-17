"""Context management for distributed tracing.

This module provides context variable management for trace and span IDs,
enabling automatic trace propagation across function calls.

Typical usage example:

    from telemetry._private.tracing.context import get_trace_context, set_trace_context

    ctx = get_trace_context()
    trace_id = ctx.get("trace_id")
"""

from contextvars import ContextVar

# Context variable to store current trace/span IDs
_trace_context: ContextVar[dict[str, str] | None] = ContextVar(
    "trace_context", default=None
)


def get_trace_context() -> dict[str, str]:
    """Get the current trace context.

    Returns:
        A dictionary containing trace_id and span_id if available,
        otherwise an empty dictionary.
    """
    return _trace_context.get() or {}


def set_trace_context(context: dict[str, str]):
    """Set the trace context.

    Args:
        context: Dictionary containing trace_id and span_id.

    Returns:
        A token that can be used to reset the context.
    """
    return _trace_context.set(context)


def reset_trace_context(token):
    """Reset the trace context to a previous state.

    Args:
        token: Token returned from set_trace_context.
    """
    _trace_context.reset(token)


def get_current_trace_id() -> str:
    """Get the current trace ID from context.

    Returns:
        The current trace ID as a hex string, or empty string if not set.
    """
    ctx = get_trace_context()
    return ctx.get("trace_id", "")


def get_current_span_id() -> str:
    """Get the current span ID from context.

    Returns:
        The current span ID as a hex string, or empty string if not set.
    """
    ctx = get_trace_context()
    return ctx.get("span_id", "")
