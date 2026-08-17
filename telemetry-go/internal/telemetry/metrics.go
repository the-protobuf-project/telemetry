package telemetry

import (
	"context"

	"go.opentelemetry.io/otel/metric"
)

// Metrics provides a simplified interface for OpenTelemetry metrics
type Metrics struct {
	meter metric.Meter
}

// NewMetrics creates a new Metrics instance
func NewMetrics(meter metric.Meter) *Metrics {
	return &Metrics{
		meter: meter,
	}
}

// Meter returns the underlying OTel SDK meter this instance wraps, for a
// caller (the root package's runtime-go/telemetry.Meter adapter) that needs
// to create instruments directly rather than through Metrics' own
// error-returning constructors.
func (m *Metrics) Meter() metric.Meter { return m.meter }

// Counter creates a new counter metric
func (m *Metrics) Counter(name string, opts ...metric.Int64CounterOption) (metric.Int64Counter, error) {
	return m.meter.Int64Counter(name, opts...)
}

// UpDownCounter creates a new up-down counter metric
func (m *Metrics) UpDownCounter(name string, opts ...metric.Int64UpDownCounterOption) (metric.Int64UpDownCounter, error) {
	return m.meter.Int64UpDownCounter(name, opts...)
}

// Histogram creates a new histogram metric
func (m *Metrics) Histogram(name string, opts ...metric.Int64HistogramOption) (metric.Int64Histogram, error) {
	return m.meter.Int64Histogram(name, opts...)
}

// FloatCounter creates a new float counter metric
func (m *Metrics) FloatCounter(name string, opts ...metric.Float64CounterOption) (metric.Float64Counter, error) {
	return m.meter.Float64Counter(name, opts...)
}

// FloatUpDownCounter creates a new float up-down counter metric
func (m *Metrics) FloatUpDownCounter(name string, opts ...metric.Float64UpDownCounterOption) (metric.Float64UpDownCounter, error) {
	return m.meter.Float64UpDownCounter(name, opts...)
}

// FloatHistogram creates a new float histogram metric
func (m *Metrics) FloatHistogram(name string, opts ...metric.Float64HistogramOption) (metric.Float64Histogram, error) {
	return m.meter.Float64Histogram(name, opts...)
}

// Gauge creates a new observable gauge metric
func (m *Metrics) Gauge(name string, opts ...metric.Int64ObservableGaugeOption) (metric.Int64ObservableGauge, error) {
	return m.meter.Int64ObservableGauge(name, opts...)
}

// FloatGauge creates a new observable float gauge metric
func (m *Metrics) FloatGauge(name string, opts ...metric.Float64ObservableGaugeOption) (metric.Float64ObservableGauge, error) {
	return m.meter.Float64ObservableGauge(name, opts...)
}

// RecordInt64 is a helper to record a single int64 value
func (m *Metrics) RecordInt64(ctx context.Context, name string, value int64, opts ...metric.Int64CounterOption) error {
	counter, err := m.Counter(name, opts...)
	if err != nil {
		return err
	}
	counter.Add(ctx, value)
	return nil
}

// RecordFloat64 is a helper to record a single float64 value
func (m *Metrics) RecordFloat64(ctx context.Context, name string, value float64, opts ...metric.Float64CounterOption) error {
	counter, err := m.FloatCounter(name, opts...)
	if err != nil {
		return err
	}
	counter.Add(ctx, value)
	return nil
}
