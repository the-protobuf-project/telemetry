package metrics

import (
	"context"
	"fmt"
	"math"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/the-protobuf-project/telemetry/telemetry-go/internal/foxglove"
	"github.com/the-protobuf-project/telemetry/telemetry-go/internal/telemetry"
	"github.com/the-protobuf-project/telemetry/telemetry-go/options"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
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
	labelSet    attribute.Set            // the same labels: the attributes of a series given none
	labelKVs    []attribute.KeyValue
	plans       sync.Map                 // reflect.Type -> *typePlan
	lastPlan    atomic.Pointer[typePlan] // the plan Record used last, found without hashing its type
	counters    sync.Map                 // seriesKey -> *counterSeries
	gauges      sync.Map                 // seriesKey -> *gaugeSeries
	histograms  sync.Map                 // name -> metric.Float64Histogram

	mu          sync.Mutex // serializes creating series and instruments
	counterSets map[string]*seriesList[*counterSeries]
	gaugeSets   map[string]*seriesList[*gaugeSeries]
}

// seriesKey names one series: an instrument and an attribute set.
type seriesKey struct {
	name  string
	attrs attribute.Distinct
}

type typePlan struct {
	t    reflect.Type
	plan []fieldMetric
}

// NewMetrics creates a Metrics instance configured with the service options and OpenTelemetry metrics.
// When a unified MCAP writer is provided, it also configures MCAP metric output when initialization succeeds.
func NewMetrics(serviceOpts options.ServiceOptions, unifiedWriter *foxglove.UnifiedMcapWriter, otelMetrics *telemetry.Metrics) *Metrics {
	labels := make([]attribute.KeyValue, 0, len(serviceOpts.Labels))
	for k, v := range serviceOpts.Labels {
		labels = append(labels, attribute.String(k, v))
	}
	set := attribute.NewSet(labels...)
	m := &Metrics{
		otelMetrics: otelMetrics,
		ctx:         context.Background(),
		serviceName: serviceOpts.Name,
		labels:      metric.WithAttributeSet(set),
		labelSet:    set,
		labelKVs:    labels,
		counterSets: map[string]*seriesList[*counterSeries]{},
		gaugeSets:   map[string]*seriesList[*gaugeSeries]{},
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

// meter is the OTel meter instruments are created on: a no-op one when
// metrics were not enabled at all, so recording still works.
func (m *Metrics) meter() metric.Meter {
	if m.otelMetrics == nil {
		return noop.Meter{}
	}
	return m.otelMetrics.Meter()
}

// fieldMetric is one tagged field of a struct type: where it is and what it
// records, worked out once per type rather than on every Record.
type fieldMetric struct {
	index []int  // reflect.Value.FieldByIndex path, through nested structs
	ptrs  []bool // whether each step but the last goes through a pointer
	kind  string // counter, histogram or gauge
	name  string // full metric name, service-prefixed

	// The series the field records to when Record is given no attributes,
	// resolved with the plan so that path looks nothing up.
	counter   *counterSeries
	gauge     *gaugeSeries
	histogram metric.Float64Histogram
}

// Record records a metric value from a struct with tags
// Tag format: `telemetry:"metric:type:name"` where type is counter, histogram, gauge
//
// Record reflects over v on every call; for a hot path, a handle from
// Counter, Gauge or Histogram records the same series for less.
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
	// Attributes given with the call make a series of their own, found by
	// the attribute set, built once for all the fields. The labels go last
	// so they win over a caller's attribute of the same key, as before.
	var set attribute.Set
	if len(attrs) > 0 {
		set = metric.NewAddConfig(append(attrs[:len(attrs):len(attrs)], m.labels)).Attributes()
	}
	for _, f := range plan {
		fv, ok := fieldAt(rv, f)
		if !ok {
			continue
		}
		val, err := toFloat(f.kind, fv)
		if err != nil {
			return err
		}
		if len(attrs) == 0 {
			m.recordPlanned(f, val)
		} else if err := m.recordWith(f.kind, f.name, set, val); err != nil {
			return err
		}
		if m.mcapWriter != nil {
			if err := m.WriteMcap(f.kind, f.name, val); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *Metrics) recordPlanned(f fieldMetric, val float64) {
	switch {
	case f.counter != nil:
		f.counter.add(val)
	case f.gauge != nil:
		f.gauge.store(val)
	case f.histogram != nil:
		f.histogram.Record(m.ctx, val, m.labels)
	}
}

func (m *Metrics) recordWith(kind, name string, set attribute.Set, val float64) error {
	switch kind {
	case "counter":
		c, err := m.counterSeries(name, set)
		if err != nil {
			return err
		}
		c.add(val)
	case "gauge":
		g, err := m.gaugeSeries(name, set)
		if err != nil {
			return err
		}
		g.store(val)
	case "histogram":
		h, err := m.histogram(name)
		if err != nil {
			return err
		}
		h.Record(m.ctx, val, metric.WithAttributeSet(set))
	}
	return nil
}

func (m *Metrics) plan(t reflect.Type) ([]fieldMetric, error) {
	if last := m.lastPlan.Load(); last != nil && last.t == t {
		return last.plan, nil
	}
	p, ok := m.plans.Load(t)
	if !ok {
		var plan []fieldMetric
		if err := m.collect(t, nil, nil, &plan); err != nil {
			return nil, err
		}
		for i := range plan {
			if err := m.resolve(&plan[i]); err != nil {
				return nil, err
			}
		}
		p, _ = m.plans.LoadOrStore(t, &typePlan{t: t, plan: plan})
	}
	m.lastPlan.Store(p.(*typePlan))
	return p.(*typePlan).plan, nil
}

// resolve finds, creating it if need be, the series f records to when
// Record is given no attributes.
func (m *Metrics) resolve(f *fieldMetric) error {
	var err error
	switch f.kind {
	case "counter":
		f.counter, err = m.counterSeries(f.name, m.labelSet)
	case "gauge":
		f.gauge, err = m.gaugeSeries(f.name, m.labelSet)
	case "histogram":
		f.histogram, err = m.histogram(f.name)
	}
	return err
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
			name = m.fullName(name)
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

// counterSeries is the series of counter name with attributes set, created
// with its observable instrument the first time either is used.
func (m *Metrics) counterSeries(name string, set attribute.Set) (*counterSeries, error) {
	key := seriesKey{name: name, attrs: set.Equivalent()}
	if s, ok := m.counters.Load(key); ok {
		return s.(*counterSeries), nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.counters.Load(key); ok {
		return s.(*counterSeries), nil
	}
	list := m.counterSets[name]
	if list == nil {
		list = &seriesList[*counterSeries]{}
		_, err := m.meter().Float64ObservableCounter(name, metric.WithFloat64Callback(
			func(_ context.Context, o metric.Float64Observer) error {
				list.each(func(s *counterSeries) { o.Observe(s.sum(), s.attrs) })
				return nil
			}))
		if err != nil {
			return nil, err
		}
		m.counterSets[name] = list
	}
	s := newCounterSeries(set)
	list.add(s)
	m.counters.Store(key, s)
	return s, nil
}

// gaugeSeries is counterSeries for a gauge, reported by an observable gauge
// as the value last set.
func (m *Metrics) gaugeSeries(name string, set attribute.Set) (*gaugeSeries, error) {
	key := seriesKey{name: name, attrs: set.Equivalent()}
	if s, ok := m.gauges.Load(key); ok {
		return s.(*gaugeSeries), nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.gauges.Load(key); ok {
		return s.(*gaugeSeries), nil
	}
	list := m.gaugeSets[name]
	if list == nil {
		list = &seriesList[*gaugeSeries]{}
		_, err := m.meter().Float64ObservableGauge(name, metric.WithFloat64Callback(
			func(_ context.Context, o metric.Float64Observer) error {
				list.each(func(s *gaugeSeries) {
					if s.set.Load() {
						o.Observe(math.Float64frombits(s.bits.Load()), s.attrs)
					}
				})
				return nil
			}))
		if err != nil {
			return nil, err
		}
		m.gaugeSets[name] = list
	}
	s := &gaugeSeries{attrs: metric.WithAttributeSet(set)}
	list.add(s)
	m.gauges.Store(key, s)
	return s, nil
}

// histogram is the synchronous OTel histogram named name: a distribution
// needs the SDK's bucketing, so it is recorded there directly.
func (m *Metrics) histogram(name string) (metric.Float64Histogram, error) {
	if h, ok := m.histograms.Load(name); ok {
		return h.(metric.Float64Histogram), nil
	}
	h, err := m.meter().Float64Histogram(name)
	if err != nil {
		return nil, err
	}
	actual, _ := m.histograms.LoadOrStore(name, h)
	return actual.(metric.Float64Histogram), nil
}

// seriesSet is the attributes of a series recorded with attrs: attrs and
// the service labels, which win on a shared key as they do in Record.
func (m *Metrics) seriesSet(attrs []attribute.KeyValue) attribute.Set {
	if len(attrs) == 0 {
		return m.labelSet
	}
	return attribute.NewSet(append(append(make([]attribute.KeyValue, 0, len(attrs)+len(m.labelKVs)), attrs...), m.labelKVs...)...)
}

// fullName is name with the service prefix the struct tags get too, so a
// handle and a tag of the same name record the same series.
func (m *Metrics) fullName(name string) string {
	if m.serviceName != "" {
		return m.serviceName + "." + name
	}
	return name
}

// WriteMcap writes a raw metric value to this instance's MCAP writer (a
// no-op when MCAP wasn't enabled at Build() time), independent of the
// struct-tag Record path — for a caller that already went through OTel's own
// typed instruments (e.g. the root package's runtime-go/telemetry.Meter
// adapter) and just needs the same MCAP side-channel the Counter, Gauge and
// Histogram handles write to.
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
