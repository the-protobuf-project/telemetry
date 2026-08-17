package telemetry

// meter.go bridges this SDK to runtime-go/telemetry.Meter — the
// backend-agnostic contract protoc-gen-telemetry's generated code (and any
// other runtime-go/telemetry consumer) binds to. Meter() returns an adapter
// over this instance's real OTel SDK meter (for OTLP export) *and* its MCAP
// writer (when WithMCAP was configured), so wiring one line —
//
//	metrics := jobsv1.NewJobMetrics(p.Meter())
//
// — sends every measurement through the exact pipeline Build() configured.
// No generated code changes: that's the entire point of generating against
// an interface instead of this SDK directly.

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/the-protobuf-project/runtime-go/telemetry"
	"github.com/the-protobuf-project/telemetry/telemetry-go/internal/metrics"
)

// Meter returns a telemetry.Meter backed by this instance's OTel SDK meter
// and, when configured (WithMCAP), its MCAP writer. Safe to call
// unconditionally: when metrics weren't enabled at Build() time, GetMetrics
// wraps OTel's own global no-op meter, and p.Metrics' MCAP writer is nil, so
// instrument creation and every Add/Set/Record below still succeed — they
// just don't record anything, matching telemetry.NoopMeter's own contract.
func (p *Telemetry) Meter() telemetry.Meter {
	if p.telemetry == nil || p.telemetry.GetMetrics() == nil {
		return telemetry.NoopMeter
	}
	return &meterAdapter{meter: p.telemetry.GetMetrics().Meter(), mcap: p.Metrics}
}

// meterAdapter implements telemetry.Meter over a real OTel SDK metric.Meter,
// dual-writing every measurement to mcap the same way this SDK's own
// struct-tag Metrics.Record path does (see internal/metrics.Metrics.
// recordCounter/recordHistogram/recordGauge) — mcap is never nil (Meter()
// only constructs a meterAdapter once p.Metrics exists), but its own
// WriteMcap is a no-op when MCAP wasn't enabled at Build() time.
type meterAdapter struct {
	meter metric.Meter
	mcap  *metrics.Metrics
}

func (a *meterAdapter) Counter(name string, opts ...telemetry.InstrumentOption) telemetry.Counter {
	cfg := telemetry.NewInstrumentConfig(opts...)
	c, err := a.meter.Float64Counter(name, counterOpts(cfg)...)
	if err != nil {
		return telemetry.NoopMeter.Counter(name)
	}
	return counterAdapter{c: c, name: name, mcap: a.mcap}
}

func (a *meterAdapter) UpDownCounter(name string, opts ...telemetry.InstrumentOption) telemetry.UpDownCounter {
	cfg := telemetry.NewInstrumentConfig(opts...)
	c, err := a.meter.Float64UpDownCounter(name, upDownCounterOpts(cfg)...)
	if err != nil {
		return telemetry.NoopMeter.UpDownCounter(name)
	}
	return upDownCounterAdapter{c: c, name: name, mcap: a.mcap}
}

func (a *meterAdapter) Gauge(name string, opts ...telemetry.InstrumentOption) telemetry.Gauge {
	cfg := telemetry.NewInstrumentConfig(opts...)
	g, err := a.meter.Float64Gauge(name, gaugeOpts(cfg)...)
	if err != nil {
		return telemetry.NoopMeter.Gauge(name)
	}
	return gaugeAdapter{g: g, name: name, mcap: a.mcap}
}

func (a *meterAdapter) Histogram(name string, opts ...telemetry.InstrumentOption) telemetry.Histogram {
	cfg := telemetry.NewInstrumentConfig(opts...)
	h, err := a.meter.Float64Histogram(name, histogramOpts(cfg)...)
	if err != nil {
		return telemetry.NoopMeter.Histogram(name)
	}
	return histogramAdapter{h: h, name: name, mcap: a.mcap}
}

// counterOpts/upDownCounterOpts/gaugeOpts/histogramOpts translate a
// telemetry.InstrumentConfig into the OTel option slice each instrument
// constructor takes. metric.WithDescription/WithUnit return metric.InstrumentOption,
// which embeds every one of these per-instrument option interfaces, so they're
// directly assignable here without a conversion.

func counterOpts(cfg telemetry.InstrumentConfig) []metric.Float64CounterOption {
	var opts []metric.Float64CounterOption
	if cfg.Description != "" {
		opts = append(opts, metric.WithDescription(cfg.Description))
	}
	if cfg.Unit != "" {
		opts = append(opts, metric.WithUnit(cfg.Unit))
	}
	return opts
}

func upDownCounterOpts(cfg telemetry.InstrumentConfig) []metric.Float64UpDownCounterOption {
	var opts []metric.Float64UpDownCounterOption
	if cfg.Description != "" {
		opts = append(opts, metric.WithDescription(cfg.Description))
	}
	if cfg.Unit != "" {
		opts = append(opts, metric.WithUnit(cfg.Unit))
	}
	return opts
}

func gaugeOpts(cfg telemetry.InstrumentConfig) []metric.Float64GaugeOption {
	var opts []metric.Float64GaugeOption
	if cfg.Description != "" {
		opts = append(opts, metric.WithDescription(cfg.Description))
	}
	if cfg.Unit != "" {
		opts = append(opts, metric.WithUnit(cfg.Unit))
	}
	return opts
}

func histogramOpts(cfg telemetry.InstrumentConfig) []metric.Float64HistogramOption {
	var opts []metric.Float64HistogramOption
	if cfg.Description != "" {
		opts = append(opts, metric.WithDescription(cfg.Description))
	}
	if cfg.Unit != "" {
		opts = append(opts, metric.WithUnit(cfg.Unit))
	}
	if len(cfg.Buckets) > 0 {
		opts = append(opts, metric.WithExplicitBucketBoundaries(cfg.Buckets...))
	}
	return opts
}

// attrsFromLabels converts telemetry.Labels into OTel attributes. nil for an
// empty map so a label-less measurement doesn't allocate.
func attrsFromLabels(labels telemetry.Labels) []attribute.KeyValue {
	if len(labels) == 0 {
		return nil
	}
	attrs := make([]attribute.KeyValue, 0, len(labels))
	for k, v := range labels {
		attrs = append(attrs, attribute.String(k, v))
	}
	return attrs
}

type counterAdapter struct {
	c    metric.Float64Counter
	name string
	mcap *metrics.Metrics
}

func (a counterAdapter) Add(ctx context.Context, delta float64, labels telemetry.Labels) {
	a.c.Add(ctx, delta, metric.WithAttributes(attrsFromLabels(labels)...))
	_ = a.mcap.WriteMcap("counter", a.name, delta)
}

type upDownCounterAdapter struct {
	c    metric.Float64UpDownCounter
	name string
	mcap *metrics.Metrics
}

func (a upDownCounterAdapter) Add(ctx context.Context, delta float64, labels telemetry.Labels) {
	a.c.Add(ctx, delta, metric.WithAttributes(attrsFromLabels(labels)...))
	// MCAP has no separate up-down-counter shape; this SDK's own struct-tag
	// path (internal/metrics.Metrics.recordGauge) already records an
	// UpDownCounter under its "gauge" MCAP kind for the same reason (a value
	// that goes up and down), so this mirrors that rather than inventing a
	// fourth MCAP kind.
	_ = a.mcap.WriteMcap("gauge", a.name, delta)
}

// gaugeAdapter implements telemetry.Gauge's Set over OTel's synchronous
// Float64Gauge, whose own method is named Record (it "records the
// instantaneous value") rather than Set — same operation, different name.
type gaugeAdapter struct {
	g    metric.Float64Gauge
	name string
	mcap *metrics.Metrics
}

func (a gaugeAdapter) Set(ctx context.Context, value float64, labels telemetry.Labels) {
	a.g.Record(ctx, value, metric.WithAttributes(attrsFromLabels(labels)...))
	_ = a.mcap.WriteMcap("gauge", a.name, value)
}

type histogramAdapter struct {
	h    metric.Float64Histogram
	name string
	mcap *metrics.Metrics
}

func (a histogramAdapter) Record(ctx context.Context, value float64, labels telemetry.Labels) {
	a.h.Record(ctx, value, metric.WithAttributes(attrsFromLabels(labels)...))
	_ = a.mcap.WriteMcap("histogram", a.name, value)
}
