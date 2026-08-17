from ._private.metrics import (
    Counter,
    Gauge,
    Histogram,
    MetricsBaseModel,
    counter,
    gauge,
    histogram,
    metric,
)
from ._private.tracing import TracedOperation, trace, trace_step, traced
from .options import (
    Environment,
    FoxgloveOptions,
    LoggingOptions,
    LogLevel,
    MetricsOptions,
    ModuleOptions,
    OpenTelemetryOptions,
    OTLPOptions,
    ServiceOptions,
    TelemetryOptions,
    TracingOptions,
    from_config,
    from_env,
)
from .telemetry import Telemetry, TelemetryBuilder

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
