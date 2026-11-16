package metrics

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"

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
	serviceName string                   // Service name prefix for metrics
	labels      metric.MeasurementOption // Service labels, as one attribute set built once
	plans       sync.Map                 // reflect.Type -> []fieldMetric
	instruments sync.Map                 // kind + ":" + name -> instrument
}

// NewMetrics creates a Metrics instance configured with the service options and OpenTelemetry metrics.
// When a unified MCAP writer is provided, it also configures MCAP metric output when initialization succeeds.
func NewMetrics(serviceOpts options.ServiceOptions, unifiedWriter *foxglove.UnifiedMcapWriter, otelMetrics *telemetry.Metrics) *Metrics {
	labels := make([]attribute.KeyValue, 0, len(serviceOpts.Labels))
	for k, v := range serviceOpts.Labels {
		labels = append(labels, attribute.String(k, v))
	}
	m := &Metrics{
		otelMetrics: otelMetrics,
		ctx:         context.Background(),
		serviceName: serviceOpts.Name,
		labels:      metric.WithAttributeSet(attribute.NewSet(labels...)),
	}

	// Initialize MCAP writer if unified writer is provided
	if unifiedWriter != nil {
		writer, err := NewMetricMcapWriter(serviceOpts, unifiedWriter)
		if err != nil {
			// Metrics still work via OTel without the MCAP side-channel.
			fmt.Fprintf(os.Stderr, "Warning: Failed to initialize MCAP metrics writer: %v\n", err)
		} else {
			m.mcapWriter = writer
		}
	}

	return m
}

// fieldMetric is one tagged field of a struct type: where it is and what it
// records, worked out once per type rather than on every Record.
type fieldMetric struct {
	index []int  // reflect.Value.FieldByIndex path, through nested structs
	ptrs  []bool // whether each step but the last goes through a pointer
	kind  string // counter, histogram or gauge
	name  string // full metric name, service-prefixed
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

	plan, err := m.plan(rv.Type())
	if err != nil {
		return err
	}
	opts := m.measurementOptions(attrs)
	for _, f := range plan {
		fv, ok := fieldAt(rv, f)
		if !ok {
			continue
		}
		val, err := toFloat(f.kind, fv)
		if err != nil {
			return err
		}
		if err := m.record(f.kind, f.name, val, opts); err != nil {
			return err
		}
	}
	return nil
}

// measurementOptions is the caller's options plus the service labels, as
// options every instrument accepts. An option that only applies to adding
// (not recording) is kept for counters alone.
func (m *Metrics) measurementOptions(attrs []metric.AddOption) []metric.AddOption {
	return append(append(make([]metric.AddOption, 0, len(attrs)+1), attrs...), m.labels)
}

func (m *Metrics) plan(t reflect.Type) ([]fieldMetric, error) {
	if p, ok := m.plans.Load(t); ok {
		return p.([]fieldMetric), nil
	}
	var plan []fieldMetric
	if err := m.collect(t, nil, nil, &plan); err != nil {
		return nil, err
	}
	m.plans.Store(t, plan)
	return plan, nil
}

// collect walks t's fields, and those of nested structs, for metric tags.
func (m *Metrics) collect(t reflect.Type, index []int, ptrs []bool, plan *[]fieldMetric) error {
	for i := range t.NumField() {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		idx := append(append([]int(nil), index...), i)
		for part := range strings.FieldsSeq(field.Tag.Get("telemetry")) {
			def, ok := strings.CutPrefix(part, "metric:")
			if !ok {
				continue
			}
			kind, name, ok := strings.Cut(def, ":")
			if !ok {
				continue
			}
			if name, _, _ = strings.Cut(name, ":"); name == "" {
				continue
			}
			switch kind {
			case "counter", "histogram", "gauge":
			default:
				return fmt.Errorf("unknown metric type: %s", kind)
			}
			if m.serviceName != "" {
				name = m.serviceName + "." + name
			}
			*plan = append(*plan, fieldMetric{index: idx, ptrs: append([]bool(nil), ptrs...), kind: kind, name: name})
		}
		switch ft := field.Type; {
		case ft.Kind() == reflect.Struct:
			if err := m.collect(ft, idx, append(ptrs, false), plan); err != nil {
				return err
			}
		case ft.Kind() == reflect.Pointer && ft.Elem().Kind() == reflect.Struct:
			if err := m.collect(ft.Elem(), idx, append(ptrs, true), plan); err != nil {
				return err
			}
		}
	}
	return nil
}

// fieldAt follows f's path through rv; false when a pointer on it is nil.
func fieldAt(rv reflect.Value, f fieldMetric) (reflect.Value, bool) {
	for i, idx := range f.index {
		rv = rv.Field(idx)
		if i < len(f.ptrs) && f.ptrs[i] {
			if rv.IsNil() {
				return reflect.Value{}, false
			}
			rv = rv.Elem()
		}
	}
	return rv, true
}

func toFloat(kind string, v reflect.Value) (float64, error) {
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(v.Int()), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(v.Uint()), nil
	case reflect.Float32, reflect.Float64:
		return v.Float(), nil
	default:
		return 0, fmt.Errorf("%s requires numeric value, got %v", kind, v.Kind())
	}
}

// record records one value through the instrument kind names, creating the
// instrument the first time it is used.
func (m *Metrics) record(kind, name string, val float64, opts []metric.AddOption) error {
	inst, err := m.instrument(kind, name)
	if err != nil {
		return err
	}
	switch i := inst.(type) {
	case metric.Float64Counter:
		i.Add(m.ctx, val, opts...)
		if m.mcapWriter != nil {
			return m.mcapWriter.WriteCounter(name, val)
		}
	case metric.Float64Histogram:
		i.Record(m.ctx, val, recordOptions(opts)...)
		if m.mcapWriter != nil {
			return m.mcapWriter.WriteHistogram(name, val)
		}
	case metric.Float64Gauge:
		i.Record(m.ctx, val, recordOptions(opts)...)
		if m.mcapWriter != nil {
			return m.mcapWriter.WriteGauge(name, val)
		}
	}
	return nil
}

// recordOptions keeps the options that also apply to recording: attributes
// given with metric.WithAttributes or WithAttributeSet, and the labels.
func recordOptions(opts []metric.AddOption) []metric.RecordOption {
	out := make([]metric.RecordOption, 0, len(opts))
	for _, o := range opts {
		if r, ok := o.(metric.RecordOption); ok {
			out = append(out, r)
		}
	}
	return out
}

func (m *Metrics) instrument(kind, name string) (any, error) {
	key := kind + ":" + name
	if inst, ok := m.instruments.Load(key); ok {
		return inst, nil
	}
	var (
		inst any
		err  error
	)
	switch kind {
	case "counter":
		inst, err = m.otelMetrics.FloatCounter(name)
	case "histogram":
		inst, err = m.otelMetrics.FloatHistogram(name)
	case "gauge":
		// A synchronous gauge: the value recorded is the value reported. (An
		// UpDownCounter here summed every value recorded into the next.)
		inst, err = m.otelMetrics.Meter().Float64Gauge(name)
	default:
		return nil, fmt.Errorf("unknown metric type: %s", kind)
	}
	if err != nil {
		return nil, err
	}
	actual, _ := m.instruments.LoadOrStore(key, inst)
	return actual, nil
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
