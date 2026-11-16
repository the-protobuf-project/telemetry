package tracing

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/the-protobuf-project/telemetry/telemetry-go/internal/foxglove"
)

// SpanData represents a trace span for MCAP logging
type SpanData struct {
	Timestamp   time.Time      `json:"timestamp"`
	SpanName    string         `json:"span_name"`
	TraceID     string         `json:"trace_id"`
	SpanID      string         `json:"span_id"`
	ParentID    string         `json:"parent_id,omitempty"`
	Attributes  map[string]any `json:"attributes,omitempty"`
	Status      string         `json:"status"`
	Duration    int64          `json:"duration_ns"`
	ServiceName string         `json:"service_name"`
}

// spanMcap records ended spans to the MCAP file, one message per span on
// /traces/<service>.
type spanMcap struct {
	w       *foxglove.UnifiedMcapWriter
	channel uint16
	service string
}

func newSpanMcap(w *foxglove.UnifiedMcapWriter, service string) (*spanMcap, error) {
	id, err := w.CreateTraceChannel(fmt.Sprintf("/traces/%s", service), map[string]string{"service": service})
	if err != nil {
		return nil, err
	}
	return &spanMcap{w: w, channel: id, service: service}, nil
}

func (m *spanMcap) write(d SpanData) {
	if m == nil || m.w.IsClosed() {
		return
	}
	d.ServiceName = m.service
	b, err := json.Marshal(d)
	if err != nil {
		return
	}
	now := uint64(time.Now().UnixNano())
	_ = m.w.WriteMessage(m.channel, b, now, now)
}
