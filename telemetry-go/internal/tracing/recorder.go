package tracing

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// SpanRecorder keeps every span a Telemetry ends in memory, so a program's
// tests can check what it traced without an exporter or a collector.
type SpanRecorder struct {
	exporter *tracetest.InMemoryExporter
}

// NewSpanRecorder returns an empty recorder.
func NewSpanRecorder() *SpanRecorder {
	return &SpanRecorder{exporter: tracetest.NewInMemoryExporter()}
}

// Exporter is the span exporter the SDK's tracer provider writes to. Shutting
// the provider down leaves the recorded spans readable.
func (r *SpanRecorder) Exporter() sdktrace.SpanExporter { return keep{r.exporter} }

// keep is the recorder's exporter with a Shutdown that keeps what it holds:
// the in-memory exporter forgets its spans on Shutdown, and a test reads them
// after the Telemetry under test has closed.
type keep struct{ *tracetest.InMemoryExporter }

func (keep) Shutdown(context.Context) error { return nil }

// Reset forgets every recorded span.
func (r *SpanRecorder) Reset() { r.exporter.Reset() }

// RecordedSpan is one ended span, in plain types.
type RecordedSpan struct {
	Name         string
	Kind         SpanKind
	TraceID      string
	SpanID       string
	ParentSpanID string // empty for a root span
	Start, End   time.Time
	Failed       bool   // status set to error
	OK           bool   // status set to ok
	Status       string // the status description
	Attributes   map[string]interface{}
	Events       []string
	Links        []string // the span ids linked to
}

// Spans returns the spans ended so far, in the order they ended.
func (r *SpanRecorder) Spans() []RecordedSpan {
	stubs := r.exporter.GetSpans()
	out := make([]RecordedSpan, 0, len(stubs))
	for _, s := range stubs {
		rs := RecordedSpan{
			Name:       s.Name,
			Kind:       kindOf(s.SpanKind),
			TraceID:    s.SpanContext.TraceID().String(),
			SpanID:     s.SpanContext.SpanID().String(),
			Start:      s.StartTime,
			End:        s.EndTime,
			Failed:     s.Status.Code == codes.Error,
			OK:         s.Status.Code == codes.Ok,
			Status:     s.Status.Description,
			Attributes: make(map[string]interface{}, len(s.Attributes)),
		}
		if s.Parent.IsValid() {
			rs.ParentSpanID = s.Parent.SpanID().String()
		}
		for _, a := range s.Attributes {
			rs.Attributes[string(a.Key)] = a.Value.AsInterface()
		}
		for _, e := range s.Events {
			rs.Events = append(rs.Events, e.Name)
		}
		for _, l := range s.Links {
			rs.Links = append(rs.Links, l.SpanContext.SpanID().String())
		}
		out = append(out, rs)
	}
	return out
}
