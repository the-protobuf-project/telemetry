"""Metric field decorators for Pydantic models.

This module provides decorator-style annotations for defining metrics on Pydantic
model fields, offering a cleaner API than using Field() directly.

It also provides function decorators for automatic metric recording.

Typical usage example:

    import telemetry
    from pydantic import BaseModel

    # Field decorators for Pydantic models
    class MyMetrics(BaseModel):
        request_count: int = counter("requests.total", "Total requests")
        response_time: float = histogram("response.time", "Response time in ms")
        active_users: int = gauge("users.active", "Active users")

    # Function decorator for automatic metric recording
    @telemetry.metric("api_request", metric_type="counter")
    def handle_request():
        return "processed"
"""

from pydantic import BaseModel, Field
from typing import Any, Callable, Optional, Dict
import functools
import time
from contextvars import ContextVar

# Global context variable to store the current Telemetry instance
_current_telemetry_metrics: ContextVar[Optional[Any]] = ContextVar(
    "current_telemetry_metrics", default=None
)


def counter(name: str, description: str = "") -> Any:
    """Decorator for counter metric fields.

    Counters are monotonically increasing values that represent cumulative totals.
    Use for: request counts, error counts, bytes processed, etc.

    Args:
        name: Metric name (e.g., "requests.total", "errors.count").
        description: Human-readable description of the metric.

    Returns:
        A Pydantic Field with counter metric metadata.

    Example:
        class Metrics(BaseModel):
            requests: int = counter("http.requests", "Total HTTP requests")
    """
    return Field(
        default=0,
        json_schema_extra={
            "metric_type": "counter",
            "metric_name": name,
            "description": description,
        },
    )


def histogram(
    name: str, description: str = "", buckets: Optional[list[float]] = None
) -> Any:
    """Decorator for histogram metric fields.

    Histograms track the distribution of values over time.
    Use for: response times, request sizes, latencies, etc.

    Args:
        name: Metric name (e.g., "response.time", "request.size").
        description: Human-readable description of the metric.
        buckets: Optional explicit bucket boundaries for the histogram.

    Returns:
        A Pydantic Field with histogram metric metadata.

    Example:
        class Metrics(BaseModel):
            latency: float = histogram("api.latency", "API latency in ms", buckets=[10, 50, 100, 500])
    """
    return Field(
        default=0.0,
        json_schema_extra={
            "metric_type": "histogram",
            "metric_name": name,
            "description": description,
            "buckets": buckets,
        },
    )


def gauge(name: str, description: str = "") -> Any:
    """Decorator for gauge metric fields.

    Gauges represent point-in-time values that can go up or down.
    Use for: memory usage, active connections, queue depth, temperature, etc.

    Args:
        name: Metric name (e.g., "memory.used", "connections.active").
        description: Human-readable description of the metric.

    Returns:
        A Pydantic Field with gauge metric metadata.

    Example:
        class Metrics(BaseModel):
            memory_mb: float = gauge("memory.used_mb", "Memory used in MB")
    """
    return Field(
        default=0.0,
        json_schema_extra={
            "metric_type": "gauge",
            "metric_name": name,
            "description": description,
        },
    )


def metric(
    name: Optional[str] = None,
    metric_type: str = "counter",
    labels: Optional[Dict[str, Any]] = None,
    record_duration: bool = False,
):
    """
    Create a decorator that records metrics for function execution.
    
    Parameters:
        name (Optional[str]): Metric name; defaults to the decorated function's name.
        metric_type (str): Metric type to record: ``"counter"``, ``"histogram"``, or
            ``"gauge"``.
        labels (Optional[Dict[str, Any]]): Labels attached to recorded metrics.
        record_duration (bool): Whether to record execution duration in milliseconds.
    
    Returns:
        Callable: A decorator that wraps a function and records configured metrics
            while preserving its return value and raised exceptions.
    """

    def decorator(func: Callable) -> Callable:
        """
        Decorate a function to record execution metrics.
        
        Parameters:
            func (Callable): Function whose execution metrics are recorded.
        
        Returns:
            Callable: Wrapped function that preserves the original behavior while recording configured metrics.
        """
        metric_name = name or func.__name__

        @functools.wraps(func)
        def wrapper(*args, **kwargs):
            """
            Execute the wrapped function and record its configured telemetry metrics.
            
            Returns:
                The wrapped function's result.
            
            Raises:
                Exception: An exception raised by the wrapped function, after an error metric is recorded.
            """
            # Get Telemetry instance from context
            telemetry_instance = _current_telemetry_metrics.get()

            if not telemetry_instance or not telemetry_instance.metrics:
                return func(*args, **kwargs)

            metrics_client = telemetry_instance.metrics
            metric_labels = labels.copy() if labels else {}

            # Record counter increment
            if metric_type == "counter":
                # We'll increment after successful execution
                pass

            # Track duration if requested
            start_time = time.time() if record_duration else None

            try:
                result = func(*args, **kwargs)

                # Record counter increment on success
                if metric_type == "counter":
                    from pydantic import BaseModel, Field

                    # Create a dynamic metric model
                    class FunctionMetric(BaseModel):
                        count: int = Field(
                            default=1,
                            json_schema_extra={
                                "metric_type": "counter",
                                "metric_name": metric_name,
                                "description": f"Call count for {func.__name__}",
                            },
                        )

                    metrics_client.record(FunctionMetric(count=1), labels=metric_labels)

                # Record duration if requested
                if record_duration and start_time:
                    duration_ms = (time.time() - start_time) * 1000

                    class DurationMetric(BaseModel):
                        duration: float = Field(
                            default=duration_ms,
                            json_schema_extra={
                                "metric_type": "histogram",
                                "metric_name": f"{metric_name}.duration",
                                "description": f"Duration for {func.__name__} in ms",
                            },
                        )

                    metrics_client.record(
                        DurationMetric(duration=duration_ms), labels=metric_labels
                    )

                # Record gauge if result is numeric
                if metric_type == "gauge" and isinstance(result, (int, float)):

                    class GaugeMetric(BaseModel):
                        value: float = Field(
                            default=float(result),
                            json_schema_extra={
                                "metric_type": "gauge",
                                "metric_name": metric_name,
                                "description": f"Value from {func.__name__}",
                            },
                        )

                    metrics_client.record(
                        GaugeMetric(value=float(result)), labels=metric_labels
                    )

                return result
            except Exception:
                # Record error counter
                class ErrorMetric(BaseModel):
                    errors: int = Field(
                        default=1,
                        json_schema_extra={
                            "metric_type": "counter",
                            "metric_name": f"{metric_name}.errors",
                            "description": f"Error count for {func.__name__}",
                        },
                    )

                metrics_client.record(ErrorMetric(errors=1), labels=metric_labels)
                raise

        return wrapper

    return decorator


def set_current_telemetry_metrics(telemetry_instance):
    """Set the current Telemetry instance for metric decorator context.

    This is called automatically by the Telemetry context manager.

    Args:
        telemetry_instance: The Telemetry instance to set as current.

    Returns:
        A token that can be used to reset the context.
    """
    return _current_telemetry_metrics.set(telemetry_instance)


def reset_current_telemetry_metrics(token):
    """Reset the current Telemetry instance for metrics.

    Args:
        token: Token returned from set_current_telemetry_metrics.
    """
    _current_telemetry_metrics.reset(token)


# Capitalized field helpers for cleaner syntax
def Counter(name: Optional[str] = None, description: str = "") -> Any:
    """Define a counter metric field with an optional name.
    
    Parameters:
        name (Optional[str]): Metric name; inferred from the model field name when omitted.
        description (str): Human-readable description of the metric.
    
    Returns:
        Any: A Pydantic field configured with counter metric metadata.
    """
    return Field(
        default=0,
        description=description,
        json_schema_extra={
            "metric_type": "counter",
            "metric_name": name,  # Will be set by MetricModel decorator if None
            "description": description,
        },
    )


def Histogram(
    name: Optional[str] = None,
    description: str = "",
    buckets: Optional[list[float]] = None,
) -> Any:
    """
    Create a histogram metric field with optional name inference.
    
    Args:
        name: Optional metric name; inferred from the model field when omitted.
        description: Human-readable description of the metric.
        buckets: Optional explicit histogram bucket boundaries.
    
    Returns:
        A Pydantic field configured with histogram metric metadata.
    """
    return Field(
        default=0.0,
        json_schema_extra={
            "metric_type": "histogram",
            "metric_name": name,
            "description": description,
            "buckets": buckets,
        },
    )


def Gauge(name: Optional[str] = None, description: str = "") -> Any:
    """
    Create a gauge metric field with optional name inference.
    
    Parameters:
    	name (Optional[str]): Explicit metric name; when omitted, the field name is inferred.
    	description (str): Human-readable description of the metric.
    
    Returns:
    	A Pydantic field configured with gauge metric metadata.
    """
    return Field(
        default=0.0,
        json_schema_extra={
            "metric_type": "gauge",
            "metric_name": name,
            "description": description,
        },
    )


class MetricsBaseModel(BaseModel):
    """Base class for metric models with automatic name inference.

    Inherit from this class instead of BaseModel to create metric models.
    The prefix will be automatically set to the service name when recording,
    but can be overridden by passing a custom prefix.

    Example:
        import telemetry

        class LLMMetrics(telemetry.MetricsBaseModel):
            tokens: int = telemetry.Counter(description="Total tokens")
            latency: float = telemetry.Histogram(description="Response time")
            # With service name "my-service", generates:
            # - "my-service.tokens"
            # - "my-service.latency"
            # - "my-service.errors"

        # Override prefix:
        class CustomMetrics(telemetry.MetricsBaseModel, prefix="custom"):
            count: int = telemetry.Counter()
            # Generates: "custom.count"
    """

    _metric_prefix: Optional[str] = None

    def __init_subclass__(cls, prefix: Optional[str] = None, **kwargs):
        """Called when a class inherits from MetricsBaseModel."""
        super().__init_subclass__(**kwargs)
        cls._metric_prefix = prefix

    def _resolve_metric_names(self, service_name: Optional[str] = None):
        """Resolve metric names with appropriate prefix.

        Args:
            service_name: Service name to use as default prefix if no custom prefix set.
        """
        # Determine the prefix to use
        prefix = (
            self._metric_prefix if self._metric_prefix is not None else service_name
        )

        # Process each field and set metric names if not provided
        for field_name, field_info in self.model_fields.items():
            if (
                hasattr(field_info, "json_schema_extra")
                and field_info.json_schema_extra
            ):
                extra = field_info.json_schema_extra
                if "metric_type" in extra and extra.get("metric_name") is None:
                    # Auto-generate metric name from field name
                    base_name = field_name.replace("_", ".")
                    if prefix:
                        extra["metric_name"] = f"{prefix}.{base_name}"
                    else:
                        extra["metric_name"] = base_name
