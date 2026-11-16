package metrics

import (
	"context"
	"sync"
	"testing"

	"github.com/the-protobuf-project/telemetry/telemetry-go/internal/telemetry"
	"github.com/the-protobuf-project/telemetry/telemetry-go/options"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func newTestMetrics(t *testing.T, labels map[string]string) (*Metrics, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	m := NewMetrics(options.ServiceOptions{Name: "svc", Labels: labels}, nil, telemetry.NewMetrics(mp.Meter("test")))
	return m, reader
}

// point is one exported data point of a sum or gauge.
type point struct {
	attrs attribute.Set
	value float64
}

func collect(t *testing.T, reader *sdkmetric.ManualReader, name string) (metricdata.Aggregation, []point) {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	for _, sm := range rm.ScopeMetrics {
		for _, md := range sm.Metrics {
			if md.Name != name {
				continue
			}
			var pts []point
			switch d := md.Data.(type) {
			case metricdata.Sum[float64]:
				for _, p := range d.DataPoints {
					pts = append(pts, point{p.Attributes, p.Value})
				}
			case metricdata.Gauge[float64]:
				for _, p := range d.DataPoints {
					pts = append(pts, point{p.Attributes, p.Value})
				}
			}
			return md.Data, pts
		}
	}
	return nil, nil
}

// Every increment from many goroutines, through a handle and through a
// struct tag of the same name, lands in one monotonic cumulative sum.
func TestCounterSumsEveryIncrementAcrossGoroutines(t *testing.T) {
	m, reader := newTestMetrics(t, nil)
	type ops struct {
		N int `telemetry:"metric:counter:ops"`
	}
	const workers, per = 16, 20_000
	c := m.Counter("ops")
	var wg sync.WaitGroup
	for w := range workers {
		wg.Go(func() {
			for i := range per {
				if w%2 == 0 {
					c.Add(1)
				} else if err := m.Record(ops{N: 1}); err != nil {
					t.Error(err)
					return
				}
				if i%1000 == 0 {
					c.Add(0.5) // fractional deltas take the other path
				}
			}
		})
	}
	wg.Wait()
	data, pts := collect(t, reader, "svc.ops")
	sum, ok := data.(metricdata.Sum[float64])
	if !ok || !sum.IsMonotonic || sum.Temporality != metricdata.CumulativeTemporality {
		t.Fatalf("svc.ops is %#v, want a monotonic cumulative sum", data)
	}
	want := float64(workers*per) + 0.5*workers*per/1000
	if len(pts) != 1 || pts[0].value != want {
		t.Errorf("points %v, want one of %v", pts, want)
	}
	c.Add(-3) // a counter never goes down
	if _, pts := collect(t, reader, "svc.ops"); pts[0].value != want {
		t.Errorf("after a negative Add the sum is %v, want %v", pts[0].value, want)
	}
}

func TestGaugeReportsTheLastValue(t *testing.T) {
	m, reader := newTestMetrics(t, nil)
	type depth struct {
		D float64 `telemetry:"metric:gauge:depth"`
	}
	g := m.Gauge("depth")
	if _, pts := collect(t, reader, "svc.depth"); len(pts) != 0 {
		t.Errorf("a gauge never set reports %v", pts)
	}
	g.Set(5)
	if err := m.Record(depth{D: 7}); err != nil {
		t.Fatal(err)
	}
	g.Set(3)
	data, pts := collect(t, reader, "svc.depth")
	if _, ok := data.(metricdata.Gauge[float64]); !ok || len(pts) != 1 || pts[0].value != 3 {
		t.Errorf("gauge %#v points %v, want 3", data, pts)
	}
}

// Distinct attribute sets are distinct series, each carrying the service
// labels; the same set through a handle and through Record is one series.
func TestLabelsKeepSeriesSeparate(t *testing.T) {
	m, reader := newTestMetrics(t, map[string]string{"robot": "r1"})
	type req struct {
		N int `telemetry:"metric:counter:requests"`
	}
	m.Counter("requests", attribute.String("code", "200")).Add(2)
	m.Counter("requests", attribute.String("code", "500")).Add(1)
	if err := m.Record(req{N: 4}, WithAttributes(attribute.String("code", "200"))); err != nil {
		t.Fatal(err)
	}
	if err := m.Record(req{N: 10}); err != nil {
		t.Fatal(err)
	}
	_, pts := collect(t, reader, "svc.requests")
	got := map[string]float64{}
	for _, p := range pts {
		if v, _ := p.attrs.Value("robot"); v.AsString() != "r1" {
			t.Errorf("series %v lost the service label", p.attrs.ToSlice())
		}
		code, _ := p.attrs.Value("code")
		got[code.AsString()] = p.value
	}
	want := map[string]float64{"200": 6, "500": 1, "": 10}
	if len(got) != len(want) {
		t.Fatalf("series %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("series code=%q is %v, want %v", k, got[k], v)
		}
	}
}

func TestHistogramHandleRecords(t *testing.T) {
	m, reader := newTestMetrics(t, map[string]string{"robot": "r1"})
	h := m.Histogram("latency", attribute.String("op", "read"))
	h.Record(1)
	h.Record(3)
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	for _, md := range rm.ScopeMetrics[0].Metrics {
		if md.Name != "svc.latency" {
			continue
		}
		dp := md.Data.(metricdata.Histogram[float64]).DataPoints
		if len(dp) != 1 || dp[0].Count != 2 || dp[0].Sum != 4 || dp[0].Attributes.Len() != 2 {
			t.Errorf("histogram points %+v", dp)
		}
		return
	}
	t.Error("svc.latency not exported")
}

// Metrics never enabled leave no meter; recording still works.
func TestRecordingWithoutAMeter(t *testing.T) {
	m := NewMetrics(options.ServiceOptions{Name: "svc"}, nil, nil)
	m.Counter("ops").Add(1)
	m.Gauge("g").Set(1)
	m.Histogram("h").Record(1)
	type all struct {
		C int `telemetry:"metric:counter:c"`
		G int `telemetry:"metric:gauge:g2"`
		H int `telemetry:"metric:histogram:h2"`
	}
	if err := m.Record(all{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkCounterAdd(b *testing.B) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	m := NewMetrics(options.ServiceOptions{Name: "svc"}, nil, telemetry.NewMetrics(mp.Meter("bench")))
	c := m.Counter("ops")
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			c.Add(1)
		}
	})
}
