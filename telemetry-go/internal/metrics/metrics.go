package metrics

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/the-protobuf-project/telemetry/telemetry-go/internal/foxglove"
	"github.com/the-protobuf-project/telemetry/telemetry-go/internal/telemetry"
	"github.com/the-protobuf-project/telemetry/telemetry-go/options"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Export OpenTelemetry metric functions for public use
var WithAttributes = metric.WithAttributes

// Export OpenTelemetry attribute creation functions for convenience
var StringAttribute = attribute.String

// Metrics wraps OpenTelemetry metrics with struct tag support and MCAP export
type Metrics struct {
	otelMetrics *telemetry.Metrics
	mcapWriter  *MetricMcapWriter
	ctx         context.Context
	registered  map[string]bool   // Track registered metrics
	serviceName string            // Service name prefix for metrics
	labels      map[string]string // Service labels to add as metric attributes
}

// NewMetrics creates a Metrics instance configured with the service options and OpenTelemetry metrics.
// NewMetrics creates a metrics wrapper configured with the service options and OpenTelemetry metrics provider.
// When a unified MCAP writer is provided, it enables MCAP metric output if initialization succeeds.
func NewMetrics(serviceOpts options.ServiceOptions, unifiedWriter *foxglove.UnifiedMcapWriter, otelMetrics *telemetry.Metrics) *Metrics {
	m := &Metrics{
		otelMetrics: otelMetrics,
		ctx:         context.Background(),
		registered:  make(map[string]bool),
		serviceName: serviceOpts.Name,
		labels:      serviceOpts.Labels,
	}

	// Initialize MCAP writer if unified writer is provided
	if unifiedWriter != nil {
		writer, err := NewMetricMcapWriter(serviceOpts, unifiedWriter)
		if err != nil {
			// Log error but continue - metrics will still work via OTEL
			// The error will be visible in the logs
			fmt.Printf("Warning: Failed to initialize MCAP metrics writer: %v\n", err)
		} else {
			m.mcapWriter = writer
		}
	}

	return m
}

// Record records a metric value from a struct with tags
// Tag format: `telemetry:"metric:type:name"` where type is counter, histogram, gauge
func (m *Metrics) Record(v any, attrs ...metric.AddOption) error {
	if v == nil {
		return nil
	}

	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}

	if rv.Kind() != reflect.Struct {
		return fmt.Errorf("Record requires a struct, got %T", v)
	}

	return m.extractAndRecordMetrics(rv, attrs...)
}

// extractAndRecordMetrics extracts metrics from struct tags and records them
func (m *Metrics) extractAndRecordMetrics(rv reflect.Value, attrs ...metric.AddOption) error {
	rt := rv.Type()

	for i := 0; i < rv.NumField(); i++ {
		field := rt.Field(i)
		fieldValue := rv.Field(i)

		if !field.IsExported() {
			continue
		}

		// Check if field has telemetry tag for metric
		tag := field.Tag.Get("telemetry")
		if tag != "" {
			// Parse tag format: "metric:type:name" or "metric:counter:requests attribute:session.id"
			// Split by space and extract only items with "metric:" prefix
			// Other prefixes (e.g., "attribute:", "trace:") are ignored by this function
			for _, tagPart := range strings.Fields(tag) {
				if metricDef, found := strings.CutPrefix(tagPart, "metric:"); found {
					// Parse metric definition: "type:name"
					parts := strings.Split(metricDef, ":")
					if len(parts) >= 2 {
						metricType := parts[0]
						metricName := parts[1]

						// Prefix metric name with service name
						if m.serviceName != "" {
							metricName = m.serviceName + "." + metricName
						}
						// Add service labels as attributes

						allAttrs := make([]metric.AddOption, 0, len(attrs)+1)

						allAttrs = append(allAttrs, attrs...)

						// Convert labels to metric attributes
						labelAttrs := make([]attribute.KeyValue, 0, len(m.labels))

						for key, value := range m.labels {
							labelAttrs = append(labelAttrs, attribute.String(key, value))
						}

						if len(labelAttrs) > 0 {
							allAttrs = append(allAttrs, metric.WithAttributeSet(attribute.NewSet(labelAttrs...)))
						}

						// Record metric based on type
						if err := m.recordMetric(metricType, metricName, fieldValue, allAttrs...); err != nil {
							return err
						}
					}
				}
			}
		}

		// Recursively process nested structs
		if fieldValue.Kind() == reflect.Struct {
			if err := m.extractAndRecordMetrics(fieldValue, attrs...); err != nil {
				return err
			}
		} else if fieldValue.Kind() == reflect.Pointer && !fieldValue.IsNil() && fieldValue.Elem().Kind() == reflect.Struct {
			if err := m.extractAndRecordMetrics(fieldValue.Elem(), attrs...); err != nil {
				return err
			}
		}
	}

	return nil
}

// recordMetric records a single metric value
func (m *Metrics) recordMetric(metricType, name string, value reflect.Value, attrs ...metric.AddOption) error {
	switch metricType {
	case "counter":
		return m.recordCounter(name, value, attrs...)
	case "histogram":
		return m.recordHistogram(name, value, attrs...)
	case "gauge":
		return m.recordGauge(name, value, attrs...)
	default:
		return fmt.Errorf("unknown metric type: %s", metricType)
	}
}

// recordCounter records a counter metric
func (m *Metrics) recordCounter(name string, value reflect.Value, attrs ...metric.AddOption) error {
	var val float64
	switch value.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		val = float64(value.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		val = float64(value.Uint())
	case reflect.Float32, reflect.Float64:
		val = value.Float()
	default:
		return fmt.Errorf("counter requires numeric value, got %v", value.Kind())
	}

	counter, err := m.otelMetrics.FloatCounter(name)
	if err != nil {
		return err
	}
	counter.Add(m.ctx, val, attrs...)

	// Write to MCAP
	if m.mcapWriter != nil {
		return m.mcapWriter.WriteCounter(name, val)
	}
	return nil
}

// recordHistogram records a histogram metric
func (m *Metrics) recordHistogram(name string, value reflect.Value, attrs ...metric.AddOption) error {
	var val float64
	switch value.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		val = float64(value.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		val = float64(value.Uint())
	case reflect.Float32, reflect.Float64:
		val = value.Float()
	default:
		return fmt.Errorf("histogram requires numeric value, got %v", value.Kind())
	}

	hist, err := m.otelMetrics.FloatHistogram(name)
	if err != nil {
		return err
	}
	hist.Record(m.ctx, val)

	// Write to MCAP
	if m.mcapWriter != nil {
		return m.mcapWriter.WriteHistogram(name, val)
	}
	return nil
}

// recordGauge records a gauge metric (using UpDownCounter for simplicity)
func (m *Metrics) recordGauge(name string, value reflect.Value, attrs ...metric.AddOption) error {
	var val float64
	switch value.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		val = float64(value.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		val = float64(value.Uint())
	case reflect.Float32, reflect.Float64:
		val = value.Float()
	default:
		return fmt.Errorf("gauge requires numeric value, got %v", value.Kind())
	}

	// Use UpDownCounter as a gauge (can go up and down)
	gauge, err := m.otelMetrics.FloatUpDownCounter(name)
	if err != nil {
		return err
	}
	gauge.Add(m.ctx, val, attrs...)

	// Write to MCAP
	if m.mcapWriter != nil {
		return m.mcapWriter.WriteGauge(name, val)
	}
	return nil
}

// WriteMcap writes a raw metric value to this instance's MCAP writer (a
// no-op when MCAP wasn't enabled at Build() time), independent of the
// struct-tag Record path — for a caller that already went through OTel's own
// typed instruments (e.g. the root package's runtime-go/telemetry.Meter
// adapter) and just needs the same MCAP side-channel Record's
// recordCounter/recordHistogram/recordGauge dual-write into.
// kind is one of "counter", "histogram", "gauge"; any other value is a no-op.
func (m *Metrics) WriteMcap(kind, name string, value float64) error {
	if m.mcapWriter == nil {
		return nil
	}
	switch kind {
	case "counter":
		return m.mcapWriter.WriteCounter(name, value)
	case "histogram":
		return m.mcapWriter.WriteHistogram(name, value)
	case "gauge":
		return m.mcapWriter.WriteGauge(name, value)
	default:
		return nil
	}
}

// Close closes the metrics system
func (m *Metrics) Close() error {
	if m.mcapWriter != nil {
		return m.mcapWriter.Close()
	}
	return nil
}
