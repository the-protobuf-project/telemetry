#pragma once

#include <string>

namespace telemetry::mcap::schemas {

inline const char* LOG_SCHEMA = R"({
    "$schema": "http://json-schema.org/draft-07/schema#",
    "title": "Log",
    "type": "object",
    "properties": {
        "timestamp": { "type": "string", "format": "date-time" },
        "level": { "type": "string", "enum": ["trace", "debug", "info", "warn", "error", "fatal"] },
        "message": { "type": "string" },
        "name": { "type": "string" },
        "file": { "type": "string" },
        "line": { "type": "integer" },
        "data": { "type": "object" }
    },
    "required": ["timestamp", "level", "message"]
})";

inline const char* METRIC_SCHEMA = R"({
    "$schema": "http://json-schema.org/draft-07/schema#",
    "title": "Metric",
    "type": "object",
    "properties": {
        "timestamp": { "type": "string", "format": "date-time" },
        "name": { "type": "string" },
        "type": { "type": "string", "enum": ["counter", "histogram", "gauge"] },
        "value": { "type": "number" },
        "labels": { "type": "object" }
    },
    "required": ["timestamp", "name", "type", "value"]
})";

inline const char* SPAN_SCHEMA = R"({
    "$schema": "http://json-schema.org/draft-07/schema#",
    "title": "Span",
    "type": "object",
    "properties": {
        "trace_id": { "type": "string" },
        "span_id": { "type": "string" },
        "parent_span_id": { "type": "string" },
        "name": { "type": "string" },
        "start_time": { "type": "string", "format": "date-time" },
        "end_time": { "type": "string", "format": "date-time" },
        "status": { "type": "string", "enum": ["ok", "error", "unset"] },
        "attributes": { "type": "object" },
        "events": { "type": "array" }
    },
    "required": ["trace_id", "span_id", "name", "start_time"]
})";

}  // namespace telemetry::mcap::schemas
