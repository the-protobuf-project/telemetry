"""
Unified MCAP writer for logs, metrics, and traces.
Loads schemas from JSON files for better maintainability.
"""

import time
from typing import Any, Dict, Optional
from mcap.writer import Writer
from mcap.well_known import SchemaEncoding, MessageEncoding
import json

from .schemas import load_schema


class UnifiedMcapWriter:
    """
    Unified MCAP writer for logs, metrics, and traces.
    Writes telemetry data to MCAP files for visualization in Foxglove Studio.
    """

    def __init__(self, mcap_path: str, service_name: str):
        """
        Initialize an MCAP writer for the specified service.
        
        Parameters:
            mcap_path (str): Path to the MCAP file to create.
            service_name (str): Name of the service associated with the telemetry data.
        """
        self.mcap_path = mcap_path
        self.service_name = service_name
        self._closed = False

        # Open file and create MCAP writer
        self.file = open(mcap_path, "wb")
        self.writer = Writer(self.file)
        self.writer.start()

        # Register schemas
        self._setup_schemas()

    def _setup_schemas(self):
        """Register the JSON schemas and channels used for logs, metrics, and traces."""
        # Load schemas from JSON files
        log_schema = load_schema("log")
        metric_schema = load_schema("metric")
        trace_schema = load_schema("trace")

        # Register log schema
        self.log_schema_id = self.writer.register_schema(
            name="foxglove.Log",
            encoding=SchemaEncoding.JSONSchema,
            data=log_schema.encode(),
        )

        self.log_channel_id = self.writer.register_channel(
            topic="/logs",
            message_encoding=MessageEncoding.JSON,
            schema_id=self.log_schema_id,
        )

        # Register metric schema
        self.metric_schema_id = self.writer.register_schema(
            name="mahcanirobotics.metric",
            encoding=SchemaEncoding.JSONSchema,
            data=metric_schema.encode(),
        )

        self.metric_channel_id = self.writer.register_channel(
            topic="/metrics",
            message_encoding=MessageEncoding.JSON,
            schema_id=self.metric_schema_id,
        )

        # Register trace schema
        self.trace_schema_id = self.writer.register_schema(
            name="mahcanirobotics.trace",
            encoding=SchemaEncoding.JSONSchema,
            data=trace_schema.encode(),
        )

        self.trace_channel_id = self.writer.register_channel(
            topic="/traces",
            message_encoding=MessageEncoding.JSON,
            schema_id=self.trace_schema_id,
        )

    def write_log(
        self,
        level: str,
        message: str,
        data: Dict[str, Any],
        timestamp: Optional[int] = None,
        name: str = "",
        file: str = "",
        line: int = 0,
        service_version: str = "",
        service_environment: str = "",
    ):
        """
        Write a timestamped structured log entry to the MCAP file.
        
        Parameters:
            level (str): Textual log level, such as ``DEBUG``, ``INFO``, ``WARNING``, ``ERROR``, or ``FATAL``.
            message (str): Log message.
            data (Dict[str, Any]): Additional structured log data.
            timestamp (Optional[int]): Timestamp in nanoseconds since the Unix epoch. Uses the current time when omitted.
            name (str): Logger name. Uses the service name when empty.
            file (str): Source file associated with the log entry.
            line (int): Source line associated with the log entry.
            service_version (str): Version of the service that produced the entry.
            service_environment (str): Environment in which the service is running.
        """
        if self._closed:
            return

        ts_ns = timestamp or time.time_ns()
        sec = ts_ns // 1_000_000_000
        nsec = ts_ns % 1_000_000_000

        # Map log levels to integers (1=DEBUG, 2=INFO, 3=WARN, 4=ERROR, 5=FATAL)
        level_map = {
            "DEBUG": 1,
            "INFO": 2,
            "WARNING": 3,
            "WARN": 3,
            "ERROR": 4,
            "CRITICAL": 5,
            "FATAL": 5,
        }

        log_data = {
            "timestamp": {"sec": int(sec), "nsec": int(nsec)},
            "level": level_map.get(level.upper(), 2),
            "message": message,
            "name": name or self.service_name,
            "file": file,
            "line": line,
            "service_version": service_version,
            "service_environment": service_environment,
            "data": data,
        }

        self.writer.add_message(
            channel_id=self.log_channel_id,
            log_time=ts_ns,
            data=json.dumps(log_data).encode(),
            publish_time=ts_ns,
        )

    def write_metric(
        self,
        name: str,
        value: float,
        metric_type: str = "",
        labels: Optional[Dict[str, Any]] = None,
        timestamp: Optional[int] = None,
    ):
        """
        Write a timestamped metric record to the MCAP file.
        
        Parameters:
            name (str): Metric name.
            value (float): Metric value.
            metric_type (str): Metric type, accepted for interface compatibility.
            labels (Optional[Dict[str, Any]]): Metric labels, accepted for interface compatibility.
            timestamp (Optional[int]): Timestamp in nanoseconds since the Unix epoch. Uses the current time when omitted.
        """
        if self._closed:
            return

        ts_ns = timestamp or time.time_ns()
        sec = ts_ns // 1_000_000_000
        nsec = ts_ns % 1_000_000_000

        metric_data = {
            "timestamp": {"sec": int(sec), "nsec": int(nsec)},
            "name": name,
            "value": value,
        }

        self.writer.add_message(
            channel_id=self.metric_channel_id,
            log_time=ts_ns,
            data=json.dumps(metric_data).encode(),
            publish_time=ts_ns,
        )

    def write_trace(
        self,
        trace_id: str,
        span_id: str,
        name: str,
        parent_span_id: Optional[str] = None,
        attributes: Optional[Dict[str, Any]] = None,
        timestamp: Optional[int] = None,
    ):
        """
        Write a trace span to the MCAP file.
        
        Parameters:
            trace_id (str): Identifier of the trace containing the span.
            span_id (str): Identifier of the span.
            name (str): Name of the span.
            parent_span_id (Optional[str]): Identifier of the parent span.
            attributes (Optional[Dict[str, Any]]): Key-value attributes associated with the span.
            timestamp (Optional[int]): Timestamp in nanoseconds since the Unix epoch.
        
        """
        if self._closed:
            return

        ts_ns = timestamp or time.time_ns()
        sec = ts_ns // 1_000_000_000
        nsec = ts_ns % 1_000_000_000

        trace_data = {
            "timestamp": {"sec": int(sec), "nsec": int(nsec)},
            "trace_id": trace_id,
            "span_id": span_id,
            "parent_span_id": parent_span_id or "",
            "name": name,
            "attributes": attributes or {},
        }

        self.writer.add_message(
            channel_id=self.trace_channel_id,
            log_time=ts_ns,
            data=json.dumps(trace_data).encode(),
            publish_time=ts_ns,
        )

    def is_closed(self) -> bool:
        """
        Determine whether the writer has been closed.
        
        Returns:
        	bool: `True` if the writer is closed, `False` otherwise.
        """
        return self._closed

    def close(self):
        """
        Close the MCAP writer and its underlying file. Repeated calls have no effect.
        """
        if not self._closed:
            self.writer.finish()
            self.file.close()
            self._closed = True
