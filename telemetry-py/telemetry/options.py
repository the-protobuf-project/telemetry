"""Configuration options for Telemetry SDK.

This module defines all configuration dataclasses and options for the Telemetry SDK,
including service metadata, telemetry settings, and configuration loading.

Configuration Priority (Lowest to Highest):
1. Defaults (lowest priority)
2. Config file (telemetry.toml / telemetry.yaml / telemetry.json)
3. Environment variables (TELEMETRY_*)
4. Code-based (builder methods) - highest priority

Typical usage example:

    from telemetry import Telemetry, Environment

    # Auto-discovers telemetry.toml config file
    telemetry = Telemetry.new() \\
        .with_service("my-service", "1.0.0") \\
        .environment(Environment.PRODUCTION) \\
        .build()

    # Or load from config file only
    service_opts, telemetry_opts = from_config()
"""

from dataclasses import dataclass, field
from enum import IntEnum, StrEnum


class Environment(StrEnum):
    """Deployment environment enumeration.

    Defines the possible deployment environments for a service.
    The environment affects default log levels and other behavior.

    Attributes:
        DEVELOPMENT: Development environment (verbose logging).
        STAGING: Staging/testing environment.
        PRODUCTION: Production environment (minimal logging).
        JETSON: NVIDIA Jetson device environment.
    """

    DEVELOPMENT = "development"
    STAGING = "staging"
    PRODUCTION = "production"
    JETSON = "jetson"


@dataclass
class ExportOptions:
    """How log records and spans queue for OTLP export.

    By default a full queue drops what does not fit, so telemetry never stalls
    the program; every drop is counted (``Telemetry.stats()``) and reported by
    ``Telemetry.close()``. With ``lossless`` the writer waits for room instead.

    Attributes:
        lossless: Wait for room when a queue is full instead of dropping.
        queue_size: Records each signal may hold awaiting export.
        batch_size: Records sent per export request.
        exporters: Exporters sharing each signal's records, each with its own
            queue and connection: one sends one batch at a time, n send n at
            once. They run in parallel on free-threaded Python (3.14t); with
            the GIL they share one core and more of them do not help.
        timeout: Seconds one export request may take, retries included,
            before its batch counts as failed (the OpenTelemetry exporters'
            own default is 10, which a large batch could exceed under load).
    """

    lossless: bool = False
    queue_size: int = 2048
    batch_size: int = 512
    exporters: int = 1
    timeout: float = 30.0


@dataclass
class OTLPOptions:
    """OpenTelemetry Protocol (OTLP) exporter configuration.

    Configures the OTLP exporter for sending telemetry data (logs, metrics, traces)
    to an OpenTelemetry collector.

    Attributes:
        endpoint: OTLP endpoint (e.g., "localhost:4317" or "otel.example.com").
        auth_token: Bearer token for authentication.
        enabled: Whether OTLP export is enabled.
        secure: Use TLS for connection.
        use_http: Use HTTP instead of gRPC.
        headers: Custom headers for OTLP requests.
    """

    endpoint: str = "localhost:4317"
    auth_token: str = ""
    enabled: bool = False
    secure: bool = False
    use_http: bool = False
    headers: dict = field(default_factory=dict)
    export: ExportOptions = field(default_factory=ExportOptions)

    @property
    def host(self) -> str:
        """Extract host from endpoint."""
        if ":" in self.endpoint:
            return self.endpoint.split(":")[0]
        return self.endpoint

    @property
    def port(self) -> int:
        """Extract port from endpoint."""
        if ":" in self.endpoint:
            try:
                return int(self.endpoint.split(":")[1])
            except (ValueError, IndexError):
                pass
        return 443 if self.secure else 4317


class LogLevel(IntEnum):
    """Log level for a service or module.

    Higher levels produce more verbose output.

    Attributes:
        UNSET: No explicit level; fall back to environment-based default.
        LEVEL1: Error only — stable, production-ready module.
        LEVEL2: Info — normal operation.
        LEVEL3: Debug — active development, full observability.
    """

    UNSET = 0
    MODULE_LEVEL_1 = 1
    MODULE_LEVEL_2 = 2
    MODULE_LEVEL_3 = 3


@dataclass
class ModuleOptions:
    """Per-module logging overrides.

    When set in config, these take highest priority (after env vars).

    Attributes:
        level: Log level override for this module.
    """

    level: LogLevel = LogLevel.UNSET


@dataclass
class LoggingOptions:
    """Logging configuration options.

    Configures the logging system including log levels and output.

    Attributes:
        enabled: Whether logging is enabled.
        level: Log level string. Can be: DEBUG, INFO, WARNING, ERROR, CRITICAL.
        module_level: Global LogLevel (overrides environment-based default).
        modules: Per-module log level overrides keyed by service name.
        console: Print each log line to stderr. Off, records still go to OTLP
            and MCAP.
    """

    enabled: bool = True
    level: str = "INFO"
    module_level: LogLevel = LogLevel.UNSET
    modules: dict[str, ModuleOptions] = field(default_factory=dict)
    console: bool = True


@dataclass
class MetricsOptions:
    """Metrics collection and export configuration.

    Configures the metrics system including collection and export intervals.

    Attributes:
        enabled: Whether metrics collection is enabled.
        export_interval_seconds: How often to export metrics to OTLP collector.
    """

    enabled: bool = True
    export_interval_seconds: int = 10


@dataclass
class TracingOptions:
    """Distributed tracing configuration.

    Configures the distributed tracing system for tracking requests across services.

    Attributes:
        enabled: Whether distributed tracing is enabled.
    """

    enabled: bool = True


@dataclass
class FoxgloveOptions:
    """Foxglove Studio MCAP recording configuration.

    Configures MCAP file recording for visualization in Foxglove Studio.
    MCAP files contain logs, metrics, and traces in a unified format.

    Attributes:
        enabled: Whether MCAP recording is enabled.
        mcap_path: File path where MCAP file will be written.
    """

    enabled: bool = False
    mcap_path: str = ""


@dataclass
class ProfilingOptions:
    """Continuous profiling to Pyroscope.

    Needs the ``profiling`` extra (``pip install telemetry-py[profiling]``).

    Attributes:
        enabled: Push CPU profiles to Pyroscope.
        server_address: Pyroscope URL; the single-image stack serves it on its
            OTLP port too (``http://host:6009``).
        basic_auth_user: Basic auth user (Grafana Cloud).
        basic_auth_password: Basic auth password (Grafana Cloud).
        tenant_id: Tenant for a multi-tenant Pyroscope.
        tags: Extra tags on every profile.
    """

    enabled: bool = False
    server_address: str = "http://localhost:4040"
    basic_auth_user: str = ""
    basic_auth_password: str = ""
    tenant_id: str = ""
    tags: dict[str, str] = field(default_factory=dict)


@dataclass
class OpenTelemetryOptions:
    """Unified telemetry configuration.

    Aggregates all telemetry-related configuration options including logging,
    metrics, tracing, and OTLP export.

    Attributes:
        logging: Logging configuration options.
        metrics: Metrics configuration options.
        tracing: Tracing configuration options.
        otlp: OTLP exporter configuration.
    """

    logging: LoggingOptions = field(default_factory=LoggingOptions)
    metrics: MetricsOptions = field(default_factory=MetricsOptions)
    tracing: TracingOptions = field(default_factory=TracingOptions)
    otlp: OTLPOptions = field(default_factory=OTLPOptions)


@dataclass
class ServiceOptions:
    """Service metadata and configuration.

    Defines the service identity and deployment environment. This information
    is included in all telemetry data for service identification.

    Attributes:
        name: Service name (required). Used in logs, metrics, and traces.
        description: Optional service description.
        version: Service version string.
        environment: Deployment environment (development, staging, production, jetson).
        labels: Optional dictionary of service labels for additional metadata.
    """

    name: str
    description: str = ""
    version: str = "1.0.0"
    environment: Environment = Environment.DEVELOPMENT
    labels: dict[str, str] = field(default_factory=dict)


@dataclass
class TelemetryOptions:
    """Main Telemetry SDK configuration.

    Top-level configuration for the Telemetry SDK, aggregating all subsystem options.

    Attributes:
        telemetry: Unified telemetry configuration (logging, metrics, tracing, OTLP).
        foxglove: Foxglove Studio MCAP recording configuration.
        logging: Optional override for logging configuration.
        tracing: Optional override for tracing configuration.
    """

    telemetry: OpenTelemetryOptions = field(default_factory=OpenTelemetryOptions)
    foxglove: FoxgloveOptions = field(default_factory=FoxgloveOptions)
    profiling: ProfilingOptions = field(default_factory=ProfilingOptions)
    logging: LoggingOptions | None = None
    tracing: TracingOptions | None = None


def from_config(
    config_path: str | None = None,
) -> tuple[ServiceOptions, TelemetryOptions]:
    """
    Load service and telemetry settings from the configured source.

    Parameters:
        config_path (Optional[str]): Path to a configuration file. When omitted, uses the configuration subsystem's auto-discovered settings.

    Returns:
        tuple[ServiceOptions, TelemetryOptions]: Service metadata and top-level telemetry configuration.
    """
    from .config import load_config, settings

    # Load config (uses auto-discovery or specified path)
    if config_path:
        cfg = load_config(config_path)
    else:
        cfg = settings

    # Map environment string to enum
    env_map = {
        "development": Environment.DEVELOPMENT,
        "staging": Environment.STAGING,
        "production": Environment.PRODUCTION,
        "jetson": Environment.JETSON,
    }
    env_str = cfg.get("service.environment", "development").lower()
    service_environment = env_map.get(env_str, Environment.DEVELOPMENT)

    # Service configuration
    service_opts = ServiceOptions(
        name=cfg.get("service.name", "unnamed-service"),
        description=cfg.get("service.description", ""),
        version=cfg.get("service.version", "1.0.0"),
        environment=service_environment,
        labels=dict(cfg.get("service.labels", {})),
    )

    # OTLP configuration - auto-enable if endpoint is set to non-localhost
    otlp_endpoint = cfg.get("telemetry.otlp.endpoint", "")
    otlp_enabled = cfg.get("telemetry.otlp.enabled", None)

    # Auto-enable OTLP if endpoint is configured (and not localhost)
    if otlp_enabled is None:
        otlp_enabled = bool(
            otlp_endpoint
            and "localhost" not in otlp_endpoint
            and "127.0.0.1" not in otlp_endpoint
        )

    otlp_opts = OTLPOptions(
        endpoint=otlp_endpoint or "localhost:4317",
        auth_token=cfg.get("telemetry.otlp.auth_token", ""),
        enabled=otlp_enabled,
        secure=cfg.get("telemetry.otlp.secure", False),
        use_http=cfg.get("telemetry.otlp.use_http", False),
    )

    # Logging configuration
    raw_module_level = cfg.get("logging.level", 0)
    try:
        module_level = LogLevel(int(raw_module_level))
    except (ValueError, KeyError):
        module_level = LogLevel.UNSET

    # Parse per-module overrides from [logging.modules.<name>]
    module_overrides: dict[str, ModuleOptions] = {}
    raw_modules = cfg.get("logging.modules", {})
    if isinstance(raw_modules, dict):
        for module_name, module_cfg in raw_modules.items():
            if isinstance(module_cfg, dict):
                try:
                    mod_level = LogLevel(int(module_cfg.get("level", 0)))
                except (ValueError, KeyError):
                    mod_level = LogLevel.UNSET
                module_overrides[module_name] = ModuleOptions(level=mod_level)

    logging_opts = LoggingOptions(
        enabled=True,
        level=cfg.get("logging.log.level", "INFO").upper(),
        module_level=module_level,
        modules=module_overrides,
    )

    # Metrics configuration
    metrics_opts = MetricsOptions(
        enabled=cfg.get("telemetry.metrics.enabled", True),
        export_interval_seconds=cfg.get(
            "telemetry.metrics.export_interval_seconds", 10
        ),
    )

    # Tracing configuration
    tracing_opts = TracingOptions(
        enabled=cfg.get("tracing.enabled", True),
    )

    # Foxglove/MCAP configuration
    foxglove_opts = FoxgloveOptions(
        enabled=cfg.get("foxglove.enabled", False),
        mcap_path=cfg.get("foxglove.file_path", ""),
    )

    # Build telemetry options
    telemetry_opts = OpenTelemetryOptions(
        logging=logging_opts,
        metrics=metrics_opts,
        tracing=tracing_opts,
        otlp=otlp_opts,
    )

    profiling_opts = ProfilingOptions(
        enabled=cfg.get("profiling.enabled", False),
        server_address=cfg.get("profiling.server_address", "http://localhost:4040"),
        basic_auth_user=cfg.get("profiling.basic_auth_user", ""),
        basic_auth_password=cfg.get("profiling.basic_auth_password", ""),
        tenant_id=cfg.get("profiling.tenant_id", ""),
        tags=dict(cfg.get("profiling.tags", {}) or {}),
    )

    telemetry_opts = TelemetryOptions(
        telemetry=telemetry_opts,
        foxglove=foxglove_opts,
        profiling=profiling_opts,
    )

    return service_opts, telemetry_opts


# Keep from_env as alias for backward compatibility
def from_env() -> tuple[ServiceOptions, TelemetryOptions]:
    """Load Telemetry configuration from environment variables (deprecated).

    Use from_config() instead which supports both config files and env vars.
    """
    return from_config()
