"""
OpenTelemetry OTLP logging integration.

This module provides OTLP (OpenTelemetry Protocol) logging export functionality,
allowing logs to be sent to OpenTelemetry collectors for centralized observability.

The OTLPLogger class bridges Python's standard logging module with OpenTelemetry's
logging SDK, enabling automatic export of logs with service metadata and structured
attributes.

Typical usage example:

    otlp_logger = OTLPLogger(
        service_name="my-service",
        service_version="1.0.0",
        service_environment="production",
        otlp_host="localhost",
        otlp_port=4317,
        log_level="INFO"
    )
    otlp_logger.write_log("INFO", "User logged in", {"user_id": "123"})
"""

import logging
from typing import Any

from opentelemetry._logs import get_logger_provider, set_logger_provider
from opentelemetry.exporter.otlp.proto.grpc._log_exporter import OTLPLogExporter
from opentelemetry.sdk._logs import LoggerProvider, LoggingHandler
from opentelemetry.sdk._logs.export import BatchLogRecordProcessor
from opentelemetry.sdk.resources import Resource


class OTLPLogger:
    """OTLP logging handler for OpenTelemetry.

    Configures and manages OpenTelemetry logging export to an OTLP collector.
    Attaches a LoggingHandler to Python's root logger to capture all log records
    and export them with service metadata.

    Attributes:
        logger: OpenTelemetry logger instance.
        service_name: Name of the service.
        service_version: Version of the service.
        service_environment: Deployment environment.
    """

    def __init__(
        self,
        service_name: str,
        service_version: str,
        service_environment: str,
        endpoint: str,
        auth_token: str = "",
        secure: bool = False,
        service_description: str = "",
        service_labels: dict[str, str] = None,
    ):
        """Configure an OpenTelemetry logger for a service.

        Args:
            service_name: Name of the service.
            service_version: Version of the service.
            service_environment: Deployment environment used to determine the log level.
            endpoint: OTLP gRPC exporter endpoint.
            auth_token: Optional bearer token for exporter authentication.
            secure: Whether to use TLS for the exporter connection.
            service_description: Optional description of the service.
            service_labels: Optional labels added to exported log records.
        """
        # Store service description and labels for use in log attributes
        self.service_description = service_description
        self.service_labels = service_labels or {}

        resource = Resource.create(
            {
                "service.name": service_name,
                "service.version": service_version,
                "service.environment": service_environment,
                "service.description": service_description,
                **self.service_labels,
            }
        )

        logger_provider = LoggerProvider(resource=resource)

        # Build headers for authentication
        headers = []
        if auth_token:
            headers.append(("authorization", f"Bearer {auth_token}"))

        # Determine endpoint URL - auto-add port 4317 for gRPC if not specified
        if "://" in endpoint:
            otlp_endpoint = endpoint
        elif ":" in endpoint:
            # Port already specified
            otlp_endpoint = endpoint
        else:
            # No port - default to 4317 for gRPC
            otlp_endpoint = f"{endpoint}:4317"

        otlp_exporter = OTLPLogExporter(
            endpoint=otlp_endpoint,
            insecure=not secure,
            headers=headers if headers else None,
        )

        logger_provider.add_log_record_processor(BatchLogRecordProcessor(otlp_exporter))

        # Only set the global logger provider once to avoid override warnings.
        # Each service still gets its own LoggerProvider with correct service.name resource.
        if not isinstance(get_logger_provider(), LoggerProvider):
            set_logger_provider(logger_provider)

        # Determine log level based on service environment
        log_level = self._get_log_level_for_environment(service_environment)

        # Setup Python logging bridge to OTEL
        handler = LoggingHandler(
            level=log_level,
            logger_provider=logger_provider,
        )

        # Create a named logger instead of using root logger to avoid cross-service contamination
        logger = logging.getLogger(f"{service_name}")
        logger.setLevel(logging.DEBUG)
        logger.addHandler(handler)

        # Don't propagate to root logger to prevent duplicate logs
        logger.propagate = False
        self.logger = logger_provider.get_logger(service_name)
        self.service_name = service_name
        self.service_version = service_version
        self.service_environment = service_environment

    def _get_log_level_for_environment(self, environment: str) -> int:
        """
        Determine the logging level for a service environment.

        Parameters:
            environment (str): Service environment name.

        Returns:
            int: `logging.DEBUG` for development environments; `logging.INFO` otherwise.
        """
        env_lower = environment.lower()
        if env_lower == "development":
            return logging.DEBUG
        else:
            return logging.INFO

    def write_log(
        self,
        level: str,
        message: str,
        data: dict[str, Any] | None,
        caller_file: str = "",
        caller_line: int = 0,
    ):
        """
        Emit a structured log entry for the configured service.

        Parameters:
            level (str): Log severity name; unknown values use INFO.
            message (str): Log message text.
            data (Optional[Dict[str, Any]]): Structured fields to include in the log entry.
            caller_file (str): Source file path associated with the entry.
            caller_line (int): Source line number associated with the entry.
        """
        import json
        import logging as std_logging

        # Map logbook levels to standard logging levels
        level_map = {
            "DEBUG": std_logging.DEBUG,
            "INFO": std_logging.INFO,
            "WARNING": std_logging.WARNING,
            "ERROR": std_logging.ERROR,
            "CRITICAL": std_logging.CRITICAL,
        }

        # Build extra attributes with service metadata, code location, and service labels
        extra_attrs = {
            "service.name": self.service_name,
            "service.version": self.service_version,
            "service.environment": self.service_environment,
            "service.description": self.service_description,
            "code.filepath": caller_file,
            "code.lineno": caller_line,
        }

        # Add service labels as direct attributes
        if self.service_labels:
            extra_attrs.update(self.service_labels)

        # Add structured data as a nested 'data' attribute
        if data:
            extra_attrs["data"] = json.dumps(data)

            # Also add individual fields for easier querying
            reserved_keys = {
                "name",
                "msg",
                "args",
                "created",
                "filename",
                "funcName",
                "levelname",
                "levelno",
                "lineno",
                "module",
                "msecs",
                "message",
                "pathname",
                "process",
                "processName",
                "relativeCreated",
                "thread",
                "threadName",
            }

            for key, value in data.items():
                attr_key = f"field.{key}" if key in reserved_keys else key

                if isinstance(value, (dict, list)):
                    extra_attrs[attr_key] = json.dumps(value)
                else:
                    extra_attrs[attr_key] = value

        # Emit to service-specific logger instead of root logger
        service_logger = logging.getLogger(f"{self.service_name}")
        std_level = level_map.get(level, std_logging.INFO)
        service_logger.log(std_level, message, extra=extra_attrs)
