package tracing

import (
	"context"
	"reflect"
	"strings"

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
	labels  map[string]string // Service labels to add as span attributes
}

// NewTracing creates a new Tracing instance
func NewTracing(serviceOpts options.ServiceOptions, opts options.TracingOptions, mcap *foxglove.UnifiedMcapWriter, tracer *telemetry.Tracer) *Tracing {
	return &Tracing{
		tracer:  tracer,
		mcap:    mcap,
		opts:    opts,
		service: serviceOpts,
		labels:  serviceOpts.Labels,
	}
}

// Span is a convenience wrapper around trace.Span with helper methods
type Span struct {
	span trace.Span
}

// End ends the span
func (s *Span) End() {
	s.span.End()
}

// SetError records an error and sets the span status to error
func (s *Span) SetError(err error) {
	if err != nil {
		s.span.RecordError(err)
		s.span.SetStatus(codes.Error, err.Error())
	}
}

// SetOK sets the span status to OK
func (s *Span) SetOK() {
	s.span.SetStatus(codes.Ok, "")
}

// AddEvent adds an event to the span
func (s *Span) AddEvent(name string) {
	s.span.AddEvent(name)
}

// SetAttribute sets a single attribute on the span
func (s *Span) SetAttribute(key string, value interface{}) {
	s.span.SetAttributes(convertToAttribute(key, value))
}

// SetAttributes sets multiple attributes on the span
func (s *Span) SetAttributes(attrs map[string]interface{}) {
	attributes := make([]attribute.KeyValue, 0, len(attrs))
	for k, v := range attrs {
		attributes = append(attributes, convertToAttribute(k, v))
	}
	s.span.SetAttributes(attributes...)
}

// SetAttributesWithLabels sets multiple attributes on the span including service labels
func (s *Span) SetAttributesWithLabels(attrs map[string]interface{}, labels map[string]string) {
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
func (t *Tracing) Start(ctx context.Context, spanName string, data ...interface{}) (context.Context, *Span) {
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
		labelAttrs := make([]attribute.KeyValue, 0, len(t.labels))
		for k, v := range t.labels {
			labelAttrs = append(labelAttrs, attribute.String(k, v))
		}
		otelSpan.SetAttributes(labelAttrs...)
	}

	return newCtx, &Span{span: otelSpan}
}

// StartWithAttrs creates a new span with explicit attributes (no struct tag parsing)
func (t *Tracing) StartWithAttrs(ctx context.Context, spanName string, attrs map[string]interface{}) (context.Context, *Span) {
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
		for k, v := range t.labels {
			attributes = append(attributes, attribute.String(k, v))
		}

		otelSpan.SetAttributes(attributes...)
	}

	return newCtx, &Span{span: otelSpan}
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
func (t *Tracing) Trace(ctx context.Context, spanName string, data interface{}, fn func(context.Context, *Span) error) error {
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

// extractAttributes extracts attributes from a struct using the `telemetry:"trace:..."` tag
func extractAttributes(data interface{}) []attribute.KeyValue {
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
			for _, tagPart := range strings.Fields(tag) {
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

// convertToAttribute converts a Go value to an OpenTelemetry attribute
func convertToAttribute(key string, value interface{}) attribute.KeyValue {
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
	default:
		// For unsupported types, convert to string
		return attribute.String(key, reflect.ValueOf(value).String())
	}
}

func (t *Tracing) Close() error {
	return nil
}
