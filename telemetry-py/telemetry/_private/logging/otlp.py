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

import json
import logging
import time
from typing import Any

from opentelemetry._logs import (
    LogRecord,
    SeverityNumber,
    get_logger_provider,
    set_logger_provider,
)
from opentelemetry.sdk._logs import LoggerProvider, LoggingHandler
from opentelemetry.sdk._logs.export import BatchLogRecordProcessor
from opentelemetry.sdk.resources import Resource

from ...options import ExportOptions
from ..accounting import (
    LOGS,
    AccountedLogProcessor,
    CountingLogExporter,
    ShardedLogProcessor,
)
from ..exporters import _protocol, log_exporter
from .pipeline import LogPipeline

_SEVERITY = {
    "DEBUG": SeverityNumber.DEBUG,
    "INFO": SeverityNumber.INFO,
    "WARNING": SeverityNumber.WARN,
    "ERROR": SeverityNumber.ERROR,
    "CRITICAL": SeverityNumber.FATAL,
}
# The same as plain numbers, for the pipeline: ints, unlike enum members, are
# not shared objects every writer thread has to count references to.
_SEVERITY_NUMBER = {k: v.value for k, v in _SEVERITY.items()}
_LEVEL = {
    "DEBUG": logging.DEBUG,
    "INFO": logging.INFO,
    "WARNING": logging.WARNING,
    "ERROR": logging.ERROR,
    "CRITICAL": logging.CRITICAL,
}
_RESERVED = frozenset(
    {
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
)


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
        export=None,
        use_http: bool = False,
        headers: dict[str, str] | None = None,
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

        headers = dict(headers or {})
        if auth_token:
            headers["authorization"] = f"Bearer {auth_token}"

        # Without a port: 4317 for gRPC, 4318 for HTTP.
        if "://" not in endpoint and ":" not in endpoint:
            endpoint = f"{endpoint}:{4318 if use_http else 4317}"

        # Counted, and lossless when asked: the SDK's batch processor alone
        # drops what overflows its queue with no more than a single warning.
        # Each exporter has its own queue, counters and gate.
        export = export or ExportOptions()
        # Over HTTP, this logger's own records take the pipeline: per-thread
        # buffers encoded straight to OTLP bytes. The provider's processors
        # still carry what the stdlib logging bridge emits, so one exporter
        # is enough for them then.
        endpoint, use_http = _protocol(endpoint, use_http)
        self._pipeline = None
        if use_http:
            try:
                self._pipeline = LogPipeline(
                    resource, service_name, endpoint, headers, export
                )
            except AttributeError:  # the exporter's transport is not where it was
                self._pipeline = None
        shards = []
        for _ in range(1 if self._pipeline else max(1, export.exporters)):
            counters = LOGS.part()
            batch = BatchLogRecordProcessor(
                CountingLogExporter(
                    log_exporter(endpoint, secure, use_http, headers, export.timeout),
                    counters,
                ),
                max_queue_size=export.queue_size,
                max_export_batch_size=min(export.batch_size, export.queue_size),
            )
            shards.append(
                AccountedLogProcessor(
                    batch, export.lossless, export.queue_size, counters
                )
            )
        logger_provider.add_log_record_processor(
            shards[0] if len(shards) == 1 else ShardedLogProcessor(shards)
        )
        if self._pipeline:
            logger_provider.add_log_record_processor(self._pipeline)

        # Only set the global logger provider once to avoid override warnings.
        # Each service still gets its own LoggerProvider with correct service.name resource.
        if not isinstance(get_logger_provider(), LoggerProvider):
            set_logger_provider(logger_provider)

        # Determine log level based on service environment
        log_level = self._get_log_level_for_environment(service_environment)

        self._min_level = log_level

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
        if _LEVEL.get(level, logging.INFO) < self._min_level:
            return

        # Service name, version, environment, description and labels are on
        # the resource every record carries; repeating them per record doubled
        # the attributes the SDK validates and encodes for each log.
        extra_attrs = {
            "code.filepath": caller_file,
            "code.lineno": caller_line,
        }

        # Add structured data as a nested 'data' attribute
        if data:
            extra_attrs["data"] = json.dumps(data)

            # Also add individual fields for easier querying
            for key, value in data.items():
                attr_key = f"field.{key}" if key in _RESERVED else key

                if isinstance(value, (dict, list)):
                    extra_attrs[attr_key] = json.dumps(value)
                else:
                    extra_attrs[attr_key] = value

        # Straight to the OpenTelemetry logger: going through the stdlib
        # logger built a logging.LogRecord only for LoggingHandler to translate
        # it back. The current span, if any, is picked up from the context.
        now = time.time_ns()
        if self._pipeline:
            self._pipeline.emit(
                now, _SEVERITY_NUMBER.get(level, 9), level, message, extra_attrs
            )
            return
        self.logger.emit(
            LogRecord(
                timestamp=now,
                observed_timestamp=now,
                severity_text=level,
                severity_number=_SEVERITY.get(level, SeverityNumber.INFO),
                body=message,
                attributes=extra_attrs,
            )
        )
