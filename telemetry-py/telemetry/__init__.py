from .telemetry import Telemetry, TelemetryBuilder
from .options import (
    ServiceOptions,
    TelemetryOptions,
    Environment,
    LogLevel,
    ModuleOptions,
    LoggingOptions,
    MetricsOptions,
    TracingOptions,
    OpenTelemetryOptions,
    FoxgloveOptions,
    OTLPOptions,
    from_config,
    from_env,
)
from ._private.metrics import (
    counter,
    histogram,
    gauge,
    metric,
    Counter,
    Histogram,
    Gauge,
    MetricsBaseModel,
)
from ._private.tracing import trace, traced, trace_step, TracedOperation

__all__ = [
    "Telemetry",
    "TelemetryBuilder",
    "ServiceOptions",
    "TelemetryOptions",
    "Environment",
    "LogLevel",
    "ModuleOptions",
    "LoggingOptions",
    "MetricsOptions",
    "TracingOptions",
    "OpenTelemetryOptions",
    "FoxgloveOptions",
    "OTLPOptions",
    "from_config",
    "from_env",
    # Metrics - lowercase (explicit names)
    "counter",
    "histogram",
    "gauge",
    "metric",
    # Metrics - capitalized (auto-inferred names)
    "Counter",
    "Histogram",
    "Gauge",
    "MetricsBaseModel",
    # Tracing
    "trace",
    "traced",
    "trace_step",
    "TracedOperation",
]
