package tracing

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// SpanKind says what side of an interaction a span records.
type SpanKind int

const (
	// SpanKindInternal is work inside one process. It is the zero value.
	SpanKindInternal SpanKind = iota
	// SpanKindServer is a request this process serves.
	SpanKindServer
	// SpanKindClient is a request this process makes and waits on.
	SpanKindClient
	// SpanKindProducer hands a message to a broker for someone else to receive.
	SpanKindProducer
	// SpanKindConsumer receives a message a producer sent.
	SpanKindConsumer
)

func (k SpanKind) otel() trace.SpanKind {
	switch k {
	case SpanKindServer:
		return trace.SpanKindServer
	case SpanKindClient:
		return trace.SpanKindClient
	case SpanKindProducer:
		return trace.SpanKindProducer
	case SpanKindConsumer:
		return trace.SpanKindConsumer
	default:
		return trace.SpanKindInternal
	}
}

func kindOf(k trace.SpanKind) SpanKind {
	switch k {
	case trace.SpanKindServer:
		return SpanKindServer
	case trace.SpanKindClient:
		return SpanKindClient
	case trace.SpanKindProducer:
		return SpanKindProducer
	case trace.SpanKindConsumer:
		return SpanKindConsumer
	default:
		return SpanKindInternal
	}
}

// spanConfig is what SpanOptions set.
type spanConfig struct {
	kind  SpanKind
	start time.Time
	attrs map[string]interface{}
	links []context.Context
}

// SpanOption configures one span started with [Tracing.StartSpan].
type SpanOption func(*spanConfig)

// WithSpanKind sets the span's kind; internal when not given.
func WithSpanKind(kind SpanKind) SpanOption {
	return func(c *spanConfig) { c.kind = kind }
}

// WithStartTime starts the span at t rather than now.
func WithStartTime(t time.Time) SpanOption {
	return func(c *spanConfig) { c.start = t }
}

// WithSpanAttributes sets attributes the span starts with.
func WithSpanAttributes(attrs map[string]interface{}) SpanOption {
	return func(c *spanConfig) {
		if c.attrs == nil {
			c.attrs = make(map[string]interface{}, len(attrs))
		}
		for k, v := range attrs {
			c.attrs[k] = v
		}
	}
}

// WithLinks links the span to the span each context carries, for work done
// on behalf of several callers; a context without a span is skipped.
func WithLinks(ctxs ...context.Context) SpanOption {
	return func(c *spanConfig) { c.links = append(c.links, ctxs...) }
}

// StartSpan begins a span named spanName under the span in ctx, configured by
// opts, and returns a context carrying it.
//
// With tracing disabled the span records nothing, and ending it leaves the
// caller's own span alone.
func (t *Tracing) StartSpan(ctx context.Context, spanName string, opts ...SpanOption) (context.Context, *Span) {
	if !t.opts.Enabled || t.tracer == nil {
		return ctx, &Span{span: trace.SpanFromContext(context.Background())}
	}
	var cfg spanConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	start := []trace.SpanStartOption{trace.WithSpanKind(cfg.kind.otel())}
	if !cfg.start.IsZero() {
		start = append(start, trace.WithTimestamp(cfg.start))
	}
	attributes := make([]attribute.KeyValue, 0, len(cfg.attrs)+len(t.labels))
	for k, v := range cfg.attrs {
		attributes = append(attributes, convertToAttribute(k, v))
	}
	for k, v := range t.labels {
		attributes = append(attributes, attribute.String(k, v))
	}
	if len(attributes) > 0 {
		start = append(start, trace.WithAttributes(attributes...))
	}
	for _, linked := range cfg.links {
		if l := trace.LinkFromContext(linked); l.SpanContext.IsValid() {
			start = append(start, trace.WithLinks(l))
		}
	}

	newCtx, span := t.tracer.Start(ctx, spanName, start...)
	return newCtx, &Span{span: span}
}

// Inject writes the trace context in ctx into headers, W3C traceparent and
// tracestate and baggage, for the receiver of a message or request to
// continue the trace. It writes nothing when ctx carries no span.
func (t *Tracing) Inject(ctx context.Context, headers map[string]string) {
	otel.GetTextMapPropagator().Inject(ctx, propagation.MapCarrier(headers))
}

// Extract returns ctx carrying the remote trace context headers hold, so a
// span started from it is the remote span's child. headers may be nil.
func (t *Tracing) Extract(ctx context.Context, headers map[string]string) context.Context {
	if len(headers) == 0 {
		return ctx
	}
	return otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier(headers))
}
