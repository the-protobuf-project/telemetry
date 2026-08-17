from typing import Optional, Dict

from .options import (
    ServiceOptions,
    TelemetryOptions,
    Environment,
    LogLevel,
    ModuleOptions,
    from_config,
)
from ._private.logging import TelemetryLogger
from ._private.metrics import (
    TelemetryMetrics,
    set_current_telemetry_metrics,
    reset_current_telemetry_metrics,
)
from ._private.tracing import (
    TelemetryTracing,
    set_current_telemetry,
    reset_current_telemetry,
)
from ._private.foxglove import UnifiedMcapWriter


class TelemetryBuilder:
    """Builder for creating Telemetry instances with fluent API.

    Configuration Priority (Lowest to Highest):
    1. Defaults (lowest priority)
    2. Config file (telemetry.toml / telemetry.yaml / telemetry.json)
    3. Environment variables (TELEMETRY_*)
    4. Code-based (builder methods) - highest priority

    Example:
        telemetry = Telemetry.new() \\
            .with_service("my-service", "1.0.0") \\
            .environment(Environment.PRODUCTION) \\
            .with_otlp("localhost", 4317) \\
            .build()
    """

    def __init__(self):
        self._config_path: Optional[str] = None
        self._name: Optional[str] = None
        self._version: Optional[str] = None
        self._description: Optional[str] = None
        self._environment: Optional[Environment] = None
        self._labels: Dict[str, str] = {}
        self._otlp_endpoint: Optional[str] = None
        self._otlp_auth_token: Optional[str] = None
        self._otlp_secure: Optional[bool] = None
        self._otlp_use_http: Optional[bool] = None
        self._mcap_path: Optional[str] = None
        self._tracing_enabled: bool = False
        self._log_level: Optional[LogLevel] = None
        self._service_from_code: bool = False

    def with_config(self, config_path: str) -> "TelemetryBuilder":
        """Load configuration from a specific file path."""
        self._config_path = config_path
        return self

    def with_service(self, name: str, version: str) -> "TelemetryBuilder":
        """Set service name and version.

        When this is called, it indicates the user wants to configure service via code,
        so we'll clear any service-level configuration from the config file to avoid collisions.
        """
        self._name = name
        self._version = version
        # Mark that service should be configured via code only
        self._service_from_code = True
        return self

    def description(self, desc: str) -> "TelemetryBuilder":
        """Set service description."""
        self._description = desc
        return self

    def environment(self, env: Environment) -> "TelemetryBuilder":
        """Set deployment environment."""
        self._environment = env
        return self

    def with_label(self, key: str, value: str) -> "TelemetryBuilder":
        """Add a custom label to all telemetry."""
        self._labels[key] = value
        return self

    def with_labels(self, labels: Dict[str, str]) -> "TelemetryBuilder":
        """Add multiple custom labels to all telemetry."""
        self._labels.update(labels)
        return self

    def with_otlp(self, host: str, port: int) -> "TelemetryBuilder":
        """Enable OTLP export to the specified endpoint."""
        self._otlp_endpoint = f"{host}:{port}"
        return self

    def with_otlp_endpoint(self, endpoint: str) -> "TelemetryBuilder":
        """Enable OTLP export to the specified endpoint string."""
        self._otlp_endpoint = endpoint
        return self

    def with_otlp_auth(self, token: str) -> "TelemetryBuilder":
        """Set OTLP authentication token."""
        self._otlp_auth_token = token
        return self

    def with_otlp_secure(self, secure: bool = True) -> "TelemetryBuilder":
        """Enable/disable TLS for OTLP connection."""
        self._otlp_secure = secure
        return self

    def with_otlp_http(self, use_http: bool = True) -> "TelemetryBuilder":
        """Use HTTP instead of gRPC for OTLP."""
        self._otlp_use_http = use_http
        return self

    def with_mcap(self, path: str) -> "TelemetryBuilder":
        """Enable MCAP recording to the specified file path."""
        self._mcap_path = path
        return self

    def with_log_level(self, level: LogLevel) -> "TelemetryBuilder":
        """Set the log level for this service/module.

        This acts as the code-level default. It can be overridden by the
        config file via [logging.modules.<service-name>] or env vars.

        Priority chain (highest to lowest):
            env var > TOML per-module override > with_log_level() > environment default

        Example:
            telemetry = Telemetry.new() \\
                .with_service("vision", "1.0.0") \\
                .with_log_level(LogLevel.MODULE_LEVEL_3) \\
                .build()
        """
        self._log_level = level
        return self

    def with_tracing(self) -> "TelemetryBuilder":
        """Enable distributed tracing."""
        self._tracing_enabled = True
        return self

    def build(self) -> "Telemetry":
        """Build and return the Telemetry instance."""
        # Load config from file (auto-discovery or specified path)
        service_opts, telemetry_opts = from_config(self._config_path)

        # If with_service was called, ignore service-level config from file
        if self._service_from_code:
            service_opts = ServiceOptions(
                name=self._name or "telemetry-service",
                version=self._version or "1.0.0",
                description=self._description or "",
                environment=self._environment or Environment.DEVELOPMENT,
                labels=dict(self._labels),  # Copy builder labels
            )
        else:
            # Override with builder values (highest priority)
            if self._name:
                service_opts.name = self._name
            if self._version:
                service_opts.version = self._version
            if self._description:
                service_opts.description = self._description
            if self._environment:
                service_opts.environment = self._environment

            # Merge labels from builder with config
            if self._labels:
                service_opts.labels.update(self._labels)

        # Configure OTLP if specified via builder
        if self._otlp_endpoint:
            telemetry_opts.telemetry.otlp.enabled = True
            telemetry_opts.telemetry.otlp.endpoint = self._otlp_endpoint

        if self._otlp_auth_token:
            telemetry_opts.telemetry.otlp.auth_token = self._otlp_auth_token

        if self._otlp_secure is not None:
            telemetry_opts.telemetry.otlp.secure = self._otlp_secure

        if self._otlp_use_http is not None:
            telemetry_opts.telemetry.otlp.use_http = self._otlp_use_http

        # Apply code-level log level (only if config didn't already set a per-module override)
        if self._log_level is not None and self._name:
            modules = telemetry_opts.telemetry.logging.modules
            if self._name not in modules:
                modules[self._name] = ModuleOptions(level=self._log_level)

        # Enable tracing if requested
        if self._tracing_enabled:
            telemetry_opts.telemetry.tracing.enabled = True
            telemetry_opts.telemetry.otlp.enabled = True

        # Configure MCAP if specified
        if self._mcap_path:
            telemetry_opts.foxglove.enabled = True
            telemetry_opts.foxglove.mcap_path = self._mcap_path

        return Telemetry(service_opts, telemetry_opts)


class Telemetry:
    """
    Main Telemetry framework class providing unified observability.

    Integrates:
    - Logging (logbook + OpenTelemetry + MCAP)
    - Metrics (OpenTelemetry + MCAP with Pydantic support)
    - Tracing (OpenTelemetry + MCAP with decorator support)

    Example using builder pattern:
        telemetry = Telemetry.new() \\
            .with_service("my-service", "1.0.0") \\
            .environment(Environment.PRODUCTION) \\
            .build()

    Example using direct instantiation:
        telemetry = Telemetry(service_opts, telemetry_opts)
    """

    @classmethod
    def new(cls) -> "TelemetryBuilder":
        """Create a new TelemetryBuilder for fluent configuration."""
        return TelemetryBuilder()

    def __init__(
        self, service_opts: ServiceOptions, telemetry_opts: TelemetryOptions
    ):
        self.service_opts = service_opts
        self.telemetry_opts = telemetry_opts

        # Initialize unified MCAP writer if enabled
        self.mcap_writer: Optional[UnifiedMcapWriter] = None
        if (
            telemetry_opts.foxglove.enabled
            and telemetry_opts.foxglove.mcap_path
        ):
            self.mcap_writer = UnifiedMcapWriter(
                mcap_path=telemetry_opts.foxglove.mcap_path,
                service_name=service_opts.name,
            )

        # Initialize logging
        self.logger = TelemetryLogger(
            service_opts=service_opts,
            logging_opts=telemetry_opts.telemetry.logging,
            otlp_opts=telemetry_opts.telemetry.otlp
            if telemetry_opts.telemetry.otlp.enabled
            else None,
            mcap_writer=self.mcap_writer,
        )

        # Initialize metrics
        self.metrics = TelemetryMetrics(
            service_opts=service_opts,
            metrics_opts=telemetry_opts.telemetry.metrics,
            otlp_opts=telemetry_opts.telemetry.otlp
            if telemetry_opts.telemetry.otlp.enabled
            else None,
            mcap_writer=self.mcap_writer,
        )

        # Initialize tracing
        self.tracing = TelemetryTracing(
            service_opts=service_opts,
            tracing_opts=telemetry_opts.telemetry.tracing,
            otlp_opts=telemetry_opts.telemetry.otlp
            if telemetry_opts.telemetry.otlp.enabled
            else None,
            mcap_writer=self.mcap_writer,
        )

    def __enter__(self):
        """Enter context manager"""
        self._telemetry_token = set_current_telemetry(self)
        self._metrics_token = set_current_telemetry_metrics(self)
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        """Exit context manager and close resources"""
        reset_current_telemetry_metrics(self._metrics_token)
        reset_current_telemetry(self._telemetry_token)
        self.close()
        return False

    def close(self):
        """Close all Telemetry components and flush pending data"""
        # Close components in order
        self.tracing.close()
        self.metrics.close()
        self.logger.close()

        # Close MCAP writer last
        if self.mcap_writer:
            self.mcap_writer.close()
