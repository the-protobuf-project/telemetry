"""
Main logging client integrating logbook, OTLP, and MCAP.
"""

from typing import Dict, Any, Optional
from logbook import Logger as LogbookLogger, StderrHandler

from ...options import (
    ServiceOptions,
    LoggingOptions,
    OTLPOptions,
    Environment,
    LogLevel,
)
from .formatter import get_custom_formatter, format_data, get_caller_info, _caller_info
from .otlp import OTLPLogger
from .mcap import MCAPLogger


class TelemetryLogger:
    """
    Logging wrapper that integrates logbook with OpenTelemetry and MCAP.

    Features:
    - Uses logbook for local logging
    - Sends logs to OTLP collector when enabled
    - Writes logs to MCAP file when enabled
    """

    def __init__(
        self,
        service_opts: ServiceOptions,
        logging_opts: LoggingOptions,
        otlp_opts: Optional[OTLPOptions] = None,
        mcap_writer=None,
    ):
        self.service_opts = service_opts
        self.logging_opts = logging_opts

        # Initialize logbook logger with formatted name
        logger_name = f"{service_opts.name} ({service_opts.version} | {service_opts.environment.value})"
        self.logger = LogbookLogger(logger_name)
        self.logger.frame_correction = 1

        # Set log level based on configuration or environment
        log_level = self._get_log_level(service_opts.environment, logging_opts.level)
        self.logger.level = log_level

        # Enable colored output with custom format
        handler = StderrHandler(level=log_level, bubble=False)
        handler.formatter = get_custom_formatter()
        handler.push_application()

        # Initialize OTLP logging if enabled
        self.otel_logger = None
        if otlp_opts and otlp_opts.enabled:
            self.otel_logger = OTLPLogger(
                service_name=service_opts.name,
                service_version=service_opts.version,
                service_environment=service_opts.environment.value,
                service_description=service_opts.description,
                service_labels=service_opts.labels,
                endpoint=otlp_opts.endpoint,
                auth_token=otlp_opts.auth_token,
                secure=otlp_opts.secure,
            )

        # Initialize MCAP logging if enabled
        self.mcap_logger = None
        if mcap_writer:
            self.mcap_logger = MCAPLogger(
                mcap_writer=mcap_writer,
                service_name=service_opts.name,
                service_version=service_opts.version,
                service_environment=service_opts.environment.value,
            )

    @staticmethod
    def _log_level_to_logbook(level: LogLevel):
        """Convert a telemetry LogLevel to a logbook log level."""
        from logbook import DEBUG, INFO, ERROR

        if level == LogLevel.MODULE_LEVEL_1:
            return ERROR
        elif level == LogLevel.MODULE_LEVEL_2:
            return INFO
        elif level == LogLevel.MODULE_LEVEL_3:
            return DEBUG
        return INFO

    def _get_log_level(self, environment, configured_level: str):
        """Determine log level using the priority chain.

        Priority (highest to lowest):
            1. Per-module override from config [logging.modules.<service-name>]
            2. Global logging.module_level
            3. Environment-based default

        Environment defaults:
        - development/jetson: DEBUG
        - staging: INFO
        - production: INFO
        """
        from logbook import DEBUG, INFO

        # 1. Start with environment-based default (lowest priority)
        if environment in (Environment.DEVELOPMENT, Environment.JETSON):
            level = DEBUG
        else:
            level = INFO

        # 2. If global module_level is set, override
        if self.logging_opts.module_level != LogLevel.UNSET:
            level = self._log_level_to_logbook(self.logging_opts.module_level)

        # 3. If per-module override exists for this service, it wins (highest)
        service_name = self.service_opts.name
        if service_name in self.logging_opts.modules:
            mod_opts = self.logging_opts.modules[service_name]
            if mod_opts.level != LogLevel.UNSET:
                level = self._log_level_to_logbook(mod_opts.level)

        return level

    def debug(self, message: str, data: Optional[Dict[str, Any]] = None):
        """Log debug message"""
        caller_file, caller_line = get_caller_info()
        _caller_info.file, _caller_info.line = caller_file, caller_line
        msg = f"{message}{format_data(data)}" if data else message
        self.logger.debug(msg)

        if self.otel_logger:
            self.otel_logger.write_log("DEBUG", message, data, caller_file, caller_line)
        if self.mcap_logger:
            self.mcap_logger.write_log("DEBUG", message, data)

    def info(self, message: str, data: Optional[Dict[str, Any]] = None):
        """Log info message"""
        caller_file, caller_line = get_caller_info()
        _caller_info.file, _caller_info.line = caller_file, caller_line
        msg = f"{message}{format_data(data)}" if data else message
        self.logger.info(msg)

        if self.otel_logger:
            self.otel_logger.write_log("INFO", message, data, caller_file, caller_line)
        if self.mcap_logger:
            self.mcap_logger.write_log("INFO", message, data)

    def warning(self, message: str, data: Optional[Dict[str, Any]] = None):
        """Log warning message"""
        caller_file, caller_line = get_caller_info()
        _caller_info.file, _caller_info.line = caller_file, caller_line
        msg = f"{message}{format_data(data)}" if data else message
        self.logger.warning(msg)

        if self.otel_logger:
            self.otel_logger.write_log(
                "WARNING", message, data, caller_file, caller_line
            )
        if self.mcap_logger:
            self.mcap_logger.write_log("WARNING", message, data)

    def error(self, message: str, data: Optional[Dict[str, Any]] = None):
        """Log error message"""
        caller_file, caller_line = get_caller_info()
        _caller_info.file, _caller_info.line = caller_file, caller_line
        msg = f"{message}{format_data(data)}" if data else message
        self.logger.error(msg)

        if self.otel_logger:
            self.otel_logger.write_log("ERROR", message, data, caller_file, caller_line)
        if self.mcap_logger:
            self.mcap_logger.write_log("ERROR", message, data)

    def critical(self, message: str, data: Optional[Dict[str, Any]] = None):
        """Log critical message"""
        caller_file, caller_line = get_caller_info()
        _caller_info.file, _caller_info.line = caller_file, caller_line
        msg = f"{message}{format_data(data)}" if data else message
        self.logger.critical(msg)

        if self.otel_logger:
            self.otel_logger.write_log(
                "CRITICAL", message, data, caller_file, caller_line
            )
        if self.mcap_logger:
            self.mcap_logger.write_log("CRITICAL", message, data)

    def close(self):
        """Close logger and flush any pending logs"""
        pass
