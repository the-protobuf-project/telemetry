package telemetry

import (
	"context"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Tracer provides a simplified interface for OpenTelemetry tracing
type Tracer struct {
	tracer trace.Tracer
}

// NewTracer creates a new Tracer instance
func NewTracer(tracer trace.Tracer) *Tracer {
	return &Tracer{
		tracer: tracer,
	}
}

// Start creates a new span and returns it along with a context containing the span
func (t *Tracer) Start(ctx context.Context, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return t.tracer.Start(ctx, spanName, opts...)
}

// StartSpan is a convenience method that starts a span
func (t *Tracer) StartSpan(ctx context.Context, name string) (context.Context, trace.Span) {
	return t.Start(ctx, name)
}

// RecordError records an error on the span from the context
func (t *Tracer) RecordError(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
}

// SetStatus sets the status of a span
func (t *Tracer) SetStatus(span trace.Span, code codes.Code, description string) {
	span.SetStatus(code, description)
}

// AddEvent adds an event to the span
func (t *Tracer) AddEvent(span trace.Span, name string, opts ...trace.EventOption) {
	span.AddEvent(name, opts...)
}

// SpanFromContext returns the current span from the context
func (t *Tracer) SpanFromContext(ctx context.Context) trace.Span {
	return trace.SpanFromContext(ctx)
}

// ContextWithSpan returns a new context with the span
func (t *Tracer) ContextWithSpan(ctx context.Context, span trace.Span) context.Context {
	return trace.ContextWithSpan(ctx, span)
}
