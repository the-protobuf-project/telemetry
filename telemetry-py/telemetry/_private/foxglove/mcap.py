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
        """Setup MCAP schemas by loading from JSON files"""
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
        """Write a log entry to MCAP using Foxglove Log schema"""
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
        """Write a metric to MCAP using mahcanirobotics.metric schema"""
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
        """Write a trace span to MCAP"""
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
        """Check if writer is closed"""
        return self._closed

    def close(self):
        """Close the MCAP writer and file"""
        if not self._closed:
            self.writer.finish()
            self.file.close()
            self._closed = True
