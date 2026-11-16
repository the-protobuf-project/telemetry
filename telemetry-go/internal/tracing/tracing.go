package tracing

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/the-protobuf-project/telemetry/telemetry-go/internal/foxglove"
	"github.com/the-protobuf-project/telemetry/telemetry-go/internal/telemetry"
	"github.com/the-protobuf-project/telemetry/telemetry-go/options"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Tracing provides a simplified interface for distributed tracing with automatic attribute extraction
type Tracing struct {
	tracer  *telemetry.Tracer
	mcap    *foxglove.UnifiedMcapWriter
	opts    options.TracingOptions
	service options.ServiceOptions
	labels  []attribute.KeyValue // Service labels, added to every span
	spans   *spanMcap            // records ended spans to MCAP; nil without it
}

// NewTracing creates a new Tracing instance
func NewTracing(serviceOpts options.ServiceOptions, opts options.TracingOptions, mcap *foxglove.UnifiedMcapWriter, tracer *telemetry.Tracer) *Tracing {
	labels := make([]attribute.KeyValue, 0, len(serviceOpts.Labels))
	for k, v := range serviceOpts.Labels {
		labels = append(labels, attribute.String(k, v))
	}
	t := &Tracing{
		tracer:  tracer,
		mcap:    mcap,
		opts:    opts,
		service: serviceOpts,
		labels:  labels,
	}
	if mcap != nil {
		// Spans were never written to MCAP before, though the writer was here.
		t.spans, _ = newSpanMcap(mcap, serviceOpts.Name)
	}
	return t
}

// Span is a convenience wrapper around trace.Span with helper methods
type Span struct {
	span trace.Span

	// For the MCAP record written when the span ends; mcap nil without it.
	mcap   *spanMcap
	name   string
	parent string
	start  time.Time
	status string
}

// recorded wraps a span just started under parentCtx, recording it to MCAP
// when it ends if MCAP is on.
func (t *Tracing) recorded(parentCtx context.Context, s trace.Span, name string) *Span {
	sp := &Span{span: s}
	if t.spans != nil {
		sp.mcap, sp.name, sp.start, sp.status = t.spans, name, time.Now(), "unset"
		if p := trace.SpanContextFromContext(parentCtx); p.IsValid() {
			sp.parent = p.SpanID().String()
		}
	}
	return sp
}

// End ends the span
func (s *Span) End() {
	s.span.End()
	if s.mcap != nil {
		sc := s.span.SpanContext()
		s.mcap.write(SpanData{
			Timestamp: s.start, SpanName: s.name,
			TraceID: sc.TraceID().String(), SpanID: sc.SpanID().String(), ParentID: s.parent,
			Status: s.status, Duration: int64(time.Since(s.start)),
		})
		s.mcap = nil
	}
}

// SetError records an error and sets the span status to error
func (s *Span) SetError(err error) {
	if err != nil {
		s.span.RecordError(err)
		s.span.SetStatus(codes.Error, err.Error())
		s.status = "error"
	}
}

// SetOK sets the span status to OK
func (s *Span) SetOK() {
	s.span.SetStatus(codes.Ok, "")
	s.status = "ok"
}

// AddEvent adds an event to the span
func (s *Span) AddEvent(name string) {
	s.span.AddEvent(name)
}

// SetAttribute sets a single attribute on the span
func (s *Span) SetAttribute(key string, value any) {
	s.span.SetAttributes(convertToAttribute(key, value))
}

// SetAttributes sets multiple attributes on the span
func (s *Span) SetAttributes(attrs map[string]any) {
	attributes := make([]attribute.KeyValue, 0, len(attrs))
	for k, v := range attrs {
		attributes = append(attributes, convertToAttribute(k, v))
	}
	s.span.SetAttributes(attributes...)
}

// SetAttributesWithLabels sets multiple attributes on the span including service labels
func (s *Span) SetAttributesWithLabels(attrs map[string]any, labels map[string]string) {
	attributes := make([]attribute.KeyValue, 0, len(attrs)+len(labels))

	// Add provided attributes
	for k, v := range attrs {
		attributes = append(attributes, convertToAttribute(k, v))
	}

	// Add service labels as attributes
	for k, v := range labels {
		attributes = append(attributes, attribute.String(k, v))
	}

	s.span.SetAttributes(attributes...)
}

// Start creates a new span with the given name and automatically extracts attributes from the provided struct
// using the `telemetry:"trace:attribute.name"` tag. Returns a new context with the span and the span itself.
//
// Example usage:
//
//	type Request struct {
//	    UserID   string `telemetry:"trace:user.id"`
//	    Action   string `telemetry:"trace:action"`
//	    Internal bool   // ignored, no tag
//	}
//
//	ctx, span := tracing.Start(ctx, "ProcessRequest", Request{UserID: "123", Action: "login"})
//	defer span.End()
func (t *Tracing) Start(ctx context.Context, spanName string, data ...any) (context.Context, *Span) {
	if !t.opts.Enabled {
		// Return a no-op span if tracing is disabled
		return ctx, &Span{span: trace.SpanFromContext(ctx)}
	}

	// Start the span
	newCtx, otelSpan := t.tracer.Start(ctx, spanName)

	// Extract attributes from data structs using tags
	if len(data) > 0 {
		attrs := extractAttributes(data[0])
		if len(attrs) > 0 {
			otelSpan.SetAttributes(attrs...)
		}
	}

	// Add service labels as attributes at trace level
	if len(t.labels) > 0 {
		otelSpan.SetAttributes(t.labels...)
	}

	return newCtx, t.recorded(ctx, otelSpan, spanName)
}

// StartWithAttrs creates a new span with explicit attributes (no struct tag parsing)
func (t *Tracing) StartWithAttrs(ctx context.Context, spanName string, attrs map[string]any) (context.Context, *Span) {
	if !t.opts.Enabled {
		return ctx, &Span{span: trace.SpanFromContext(ctx)}
	}

	newCtx, otelSpan := t.tracer.Start(ctx, spanName)

	if len(attrs) > 0 || len(t.labels) > 0 {
		attributes := make([]attribute.KeyValue, 0, len(attrs)+len(t.labels))

		// Add provided attributes
		for k, v := range attrs {
			attributes = append(attributes, convertToAttribute(k, v))
		}

		// Add service labels as attributes
		attributes = append(attributes, t.labels...)

		otelSpan.SetAttributes(attributes...)
	}

	return newCtx, t.recorded(ctx, otelSpan, spanName)
}

// Trace is a convenience function that wraps a function with a span
// It automatically handles span creation, error recording, and span ending
//
// Example usage:
//
//	err := tracing.Trace(ctx, "ProcessData", data, func(ctx context.Context, span *Span) error {
//	    // Your code here
//	    return nil
//	})
func (t *Tracing) Trace(ctx context.Context, spanName string, data any, fn func(context.Context, *Span) error) error {
	ctx, span := t.Start(ctx, spanName, data)
	defer span.End()

	err := fn(ctx, span)
	if err != nil {
		span.SetError(err)
	} else {
		span.SetOK()
	}

	return err
}

// TraceFunc is a convenience function that wraps a function with a span (no data struct)
func (t *Tracing) TraceFunc(ctx context.Context, spanName string, fn func(context.Context, *Span) error) error {
	ctx, span := t.StartWithAttrs(ctx, spanName, nil)
	defer span.End()

	err := fn(ctx, span)
	if err != nil {
		span.SetError(err)
	} else {
		span.SetOK()
	}

	return err
}

// extractAttributes collects OpenTelemetry attributes from exported fields tagged with
// `telemetry:"trace:<name>"`, including fields in nested structs and struct pointers.
// It returns no attributes for nil, non-struct, or nil pointer input.
func extractAttributes(data any) []attribute.KeyValue {
	if data == nil {
		return nil
	}

	v := reflect.ValueOf(data)

	// Handle pointers
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}

	// Only process structs
	if v.Kind() != reflect.Struct {
		return nil
	}

	rt := v.Type()
	attrs := make([]attribute.KeyValue, 0)

	for i := 0; i < v.NumField(); i++ {
		field := rt.Field(i)
		value := v.Field(i)

		// Get the telemetry tag
		tag := field.Tag.Get("telemetry")
		if tag != "" {
			// Parse tag format: "trace:attribute.name" or "trace:session.id attribute:session.id"
			// Split by space and extract only items with "trace:" prefix
			// Other prefixes (e.g., "attribute:") are ignored by this function
			for tagPart := range strings.FieldsSeq(tag) {
				if attrName, found := strings.CutPrefix(tagPart, "trace:"); found {
					// Convert field value to attribute only if field is exportable
					if value.CanInterface() {
						attr := convertToAttribute(attrName, value.Interface())
						attrs = append(attrs, attr)
					}
				}
			}
		}

		// Recursively process nested structs
		if value.Kind() == reflect.Struct {
			if value.CanInterface() {
				attrs = append(attrs, extractAttributes(value.Interface())...)
			}
		} else if value.Kind() == reflect.Pointer && !value.IsNil() && value.Elem().Kind() == reflect.Struct {
			if value.CanInterface() {
				attrs = append(attrs, extractAttributes(value.Interface())...)
			}
		}
	}

	return attrs
}

// convertToAttribute converts a Go value to an OpenTelemetry attribute. Any
// integer, float, string or bool type keeps its kind, whatever its exact Go
// type (int32, uint16, a named string type...); everything else is printed.
func convertToAttribute(key string, value any) attribute.KeyValue {
	switch v := value.(type) {
	case string:
		return attribute.String(key, v)
	case int:
		return attribute.Int(key, v)
	case int64:
		return attribute.Int64(key, v)
	case float64:
		return attribute.Float64(key, v)
	case bool:
		return attribute.Bool(key, v)
	case []string:
		return attribute.StringSlice(key, v)
	case []int:
		return attribute.IntSlice(key, v)
	case []int64:
		return attribute.Int64Slice(key, v)
	case []float64:
		return attribute.Float64Slice(key, v)
	case []bool:
		return attribute.BoolSlice(key, v)
	case fmt.Stringer:
		return attribute.String(key, v.String())
	case error:
		return attribute.String(key, v.Error())
	case nil:
		return attribute.String(key, "<nil>")
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.String:
		return attribute.String(key, rv.String())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return attribute.Int64(key, rv.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return attribute.Int64(key, int64(rv.Uint()))
	case reflect.Float32, reflect.Float64:
		return attribute.Float64(key, rv.Float())
	case reflect.Bool:
		return attribute.Bool(key, rv.Bool())
	default:
		return attribute.String(key, fmt.Sprintf("%+v", value))
	}
}

func (t *Tracing) Close() error {
	return nil
}
