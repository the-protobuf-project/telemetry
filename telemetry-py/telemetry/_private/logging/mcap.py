"""MCAP logging writer for Foxglove Studio.

This module provides MCAP file logging for visualization in Foxglove Studio.
Logs are written to MCAP files using the Foxglove Log schema, allowing
time-series visualization and analysis.

Typical usage example:

    mcap_logger = MCAPLogger(
        mcap_writer=writer,
        service_name="my-service",
        service_version="1.0.0",
        service_environment="production"
    )
    mcap_logger.write_log("INFO", "User action", {"user_id": "123"})
"""

import time
from typing import Any


class MCAPLogger:
    """MCAP logging writer for Foxglove Studio.

    Wraps the UnifiedMcapWriter to provide a simple interface for writing
    log entries to MCAP files with service metadata.

    Attributes:
        mcap_writer: The UnifiedMcapWriter instance.
        service_name: Name of the service.
        service_version: Version of the service.
        service_environment: Deployment environment.
    """

    def __init__(
        self,
        mcap_writer,
        service_name: str,
        service_version: str,
        service_environment: str,
    ):
        """Initialize the MCAP logger.

        Args:
            mcap_writer: UnifiedMcapWriter instance for writing to MCAP file.
            service_name: Name of the service for metadata.
            service_version: Version of the service.
            service_environment: Deployment environment (e.g., "production").
        """
        self.mcap_writer = mcap_writer
        self.service_name = service_name
        self.service_version = service_version
        self.service_environment = service_environment

    def write_log(self, level: str, message: str, data: dict[str, Any] | None = None):
        """
        Write a timestamped Foxglove log record with service metadata.

        Args:
            level: Log severity.
            message: Log message text.
            data: Optional structured data to include in the record. Defaults to an empty dictionary.

        Writes are skipped when the MCAP writer is unavailable or closed.
        """
        if self.mcap_writer and not self.mcap_writer.is_closed():
            self.mcap_writer.write_log(
                level=level,
                message=message,
                data=data or {},
                timestamp=time.time_ns(),
                name=self.service_name,
                file="",
                line=0,
                service_version=self.service_version,
                service_environment=self.service_environment,
            )
