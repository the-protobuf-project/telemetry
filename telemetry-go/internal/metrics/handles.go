package metrics

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

// Counter is a counter series bound once to its name and attributes, the
// fastest way to count: Add is a few nanoseconds and scales across
// goroutines. Get it once and keep it:
//
//	ops := tel.Metrics.Counter("ops")
//	...
//	ops.Add(1)
type Counter struct {
	s    *counterSeries
	name string
	mcap *MetricMcapWriter
}

// Gauge is a gauge series bound to its name and attributes; it reports the
// value last set.
type Gauge struct {
	s    *gaugeSeries
	name string
	mcap *MetricMcapWriter
}

// Histogram is a histogram series bound to its name and attributes.
type Histogram struct {
	h     metric.Float64Histogram
	attrs metric.RecordOption
	name  string
	mcap  *MetricMcapWriter
}

// Counter returns the counter name (service-prefixed, as a struct tag's
// would be) with attrs and the service labels. It records the same series
// as Record does for a tag of that name. An instrument OTel refuses is
// reported to the OTel error handler, and the counter then counts unexported.
func (m *Metrics) Counter(name string, attrs ...attribute.KeyValue) *Counter {
	name = m.fullName(name)
	s, err := m.counterSeries(name, m.seriesSet(attrs))
	if err != nil {
		otel.Handle(err)
		s = newCounterSeries(m.seriesSet(attrs))
	}
	return &Counter{s: s, name: name, mcap: m.mcapWriter}
}

// Gauge returns the gauge name with attrs and the service labels; see Counter.
func (m *Metrics) Gauge(name string, attrs ...attribute.KeyValue) *Gauge {
	name = m.fullName(name)
	s, err := m.gaugeSeries(name, m.seriesSet(attrs))
	if err != nil {
		otel.Handle(err)
		s = &gaugeSeries{}
	}
	return &Gauge{s: s, name: name, mcap: m.mcapWriter}
}

// Histogram returns the histogram name with attrs and the service labels;
// see Counter.
func (m *Metrics) Histogram(name string, attrs ...attribute.KeyValue) *Histogram {
	name = m.fullName(name)
	h, err := m.histogram(name)
	if err != nil {
		otel.Handle(err)
		h = noop.Float64Histogram{}
	}
	return &Histogram{h: h, attrs: metric.WithAttributeSet(m.seriesSet(attrs)), name: name, mcap: m.mcapWriter}
}

// Add adds v to the counter. A negative or NaN v is dropped: a counter only
// goes up.
func (c *Counter) Add(v float64) {
	c.s.add(v)
	if c.mcap != nil {
		_ = c.mcap.WriteCounter(c.name, v)
	}
}

// Set sets the gauge to v.
func (g *Gauge) Set(v float64) {
	g.s.store(v)
	if g.mcap != nil {
		_ = g.mcap.WriteGauge(g.name, v)
	}
}

// Record records v in the histogram.
func (h *Histogram) Record(v float64) {
	h.h.Record(context.Background(), v, h.attrs)
	if h.mcap != nil {
		_ = h.mcap.WriteHistogram(h.name, v)
	}
}
