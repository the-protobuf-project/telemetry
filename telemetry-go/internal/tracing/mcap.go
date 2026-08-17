package tracing

import (
	"time"
)

// SpanData represents a trace span for MCAP logging
type SpanData struct {
	Timestamp   time.Time              `json:"timestamp"`
	SpanName    string                 `json:"span_name"`
	TraceID     string                 `json:"trace_id"`
	SpanID      string                 `json:"span_id"`
	ParentID    string                 `json:"parent_id,omitempty"`
	Attributes  map[string]interface{} `json:"attributes,omitempty"`
	Status      string                 `json:"status"`
	Duration    int64                  `json:"duration_ns"`
	ServiceName string                 `json:"service_name"`
}
