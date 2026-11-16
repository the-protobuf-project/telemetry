package telemetry_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	telemetry "github.com/the-protobuf-project/telemetry/telemetry-go"
	"github.com/the-protobuf-project/telemetry/telemetry-go/internal/loadtest"
)

// exporting builds a Telemetry exporting logs and spans to an in-process
// collector, printing nothing.
func exporting(t testing.TB, export telemetry.ExportOptions) (*telemetry.Telemetry, *loadtest.Collector) {
	t.Helper()
	t.Setenv("OTEL_EXPORTER_OTLP_ENABLED", "")
	t.Setenv("OTEL_SDK_DISABLED", "")
	col, err := loadtest.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(col.Stop)
	host, port, _ := net.SplitHostPort(col.Addr())
	p, _ := strconv.Atoi(port)
	tel, err := telemetry.New().
		WithService("load-test", "1.0.0").
		WithOTLP(host, p).
		WithTracing().
		WithExport(export).
		WithLogHandler(slog.DiscardHandler).
		WithShutdownTimeout(30 * time.Second).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	return tel, col
}

func emit(tel *telemetry.Telemetry, n, workers int) {
	var wg sync.WaitGroup
	for w := range workers {
		wg.Go(func() {
			for i := range n / workers {
				tel.Logger.Info("load", map[string]any{"i": i, "worker": w})
				_, s := tel.Tracing.StartSpan(context.Background(), "load.op")
				s.End()
			}
		})
	}
	wg.Wait()
}

// A queue far smaller than the burst loses nothing when lossless: producers
// wait for the exporter instead.
func TestLosslessDeliversEverything(t *testing.T) {
	n := 200_000
	if testing.Short() {
		n = 20_000
	}
	tel, col := exporting(t, telemetry.ExportOptions{Lossless: true, QueueSize: 256, BatchSize: 64})
	emit(tel, n, 8)
	if err := tel.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	got, stats := col.Counts(), tel.Stats()
	if got.Logs != int64(n) || got.Spans != int64(n) {
		t.Errorf("collector got %d logs, %d spans; want %d each", got.Logs, got.Spans, n)
	}
	for name, s := range map[string]telemetry.SignalStats{"logs": stats.Logs, "spans": stats.Spans} {
		if s.Emitted != int64(n) || s.Exported != int64(n) || s.Dropped != 0 || s.Pending() != 0 {
			t.Errorf("%s stats %+v", name, s)
		}
	}
}

// Without Lossless, what does not fit is dropped — and every drop is counted
// and reported by Close, where it used to vanish without a word.
func TestDropsAreCountedAndReported(t *testing.T) {
	tel, col := exporting(t, telemetry.ExportOptions{QueueSize: 16, BatchSize: 16})
	col.Delay = 20 * time.Millisecond
	emit(tel, 20_000, 4)
	err := tel.Close()
	stats := tel.Stats()
	if stats.Spans.Dropped == 0 || stats.Logs.Dropped == 0 {
		t.Fatalf("expected drops against a slow collector, stats %+v", stats)
	}
	for name, s := range map[string]telemetry.SignalStats{"logs": stats.Logs, "spans": stats.Spans} {
		if s.Exported+s.Dropped+s.Failed != s.Emitted {
			t.Errorf("%s: not every record accounted for: %+v", name, s)
		}
	}
	if got := col.Counts(); got.Spans != stats.Spans.Exported || got.Logs != stats.Logs.Exported {
		t.Errorf("collector got %d logs, %d spans; stats say %d, %d exported", got.Logs, got.Spans, stats.Logs.Exported, stats.Spans.Exported)
	}
	if err == nil || !strings.Contains(err.Error(), "dropped (export queue full)") || !strings.Contains(err.Error(), "Lossless") {
		t.Errorf("Close = %v, want the drops reported with the fix", err)
	}
}

// recordingHandler keeps every record it is given.
type recordingHandler struct {
	mu   sync.Mutex
	recs []slog.Record
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	h.recs = append(h.recs, r.Clone())
	h.mu.Unlock()
	return nil
}
func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

func attrsOf(r slog.Record) map[string]any {
	m := map[string]any{}
	r.Attrs(func(a slog.Attr) bool { m[a.Key] = a.Value.Any(); return true })
	return m
}

func TestWithLogHandlerReceivesEveryRecordAtTheServiceLevel(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENABLED", "false")
	h := &recordingHandler{}
	tel, err := telemetry.New().
		WithService("own-logger", "1.0.0").
		WithLogLevel(telemetry.ModuleLevel_2). // Info
		WithLogHandler(h).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tel.Close() }()

	tel.Logger.Debug("filtered out")
	tel.Logger.Info("user joined", map[string]any{"user_id": "alice", "room": 7})
	tel.Logger.Warnf("retry %d", 3)
	tel.Slog().Error("from slog", "code", 500)

	if len(h.recs) != 3 {
		t.Fatalf("records %d, want 3 (Debug is below Info)", len(h.recs))
	}
	joined := h.recs[0]
	if joined.Message != "user joined" || attrsOf(joined)["user_id"] != "alice" || attrsOf(joined)["room"] != int64(7) {
		t.Errorf("record %q attrs %v", joined.Message, attrsOf(joined))
	}
	if h.recs[1].Message != "retry 3" || h.recs[1].Level != slog.LevelWarn {
		t.Errorf("Warnf record %q at %v", h.recs[1].Message, h.recs[1].Level)
	}
	for _, r := range h.recs {
		f, _ := runtimeFrame(r.PC)
		if !strings.HasSuffix(f, "load_test.go") {
			t.Errorf("record %q reports caller %q, want this file", r.Message, f)
		}
	}
}

// A struct-tag gauge reports the value last set, and a histogram keeps the
// service labels.
func TestStructTagGaugeAndHistogramLabels(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENABLED", "false")
	tel, err := telemetry.New().
		WithService("tags", "1.0.0").
		WithLabel("robot_id", "r1").
		WithPrometheus().
		WithLogHandler(slog.DiscardHandler).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tel.Close() }()
	type reading struct {
		Depth   int     `telemetry:"metric:gauge:queue_depth"`
		Latency float64 `telemetry:"metric:histogram:latency_ms"`
	}
	for _, v := range []int{5, 7, 3} {
		if err := tel.Metrics.Record(reading{Depth: v, Latency: 12}); err != nil {
			t.Fatal(err)
		}
	}
	rec := httptest.NewRecorder()
	tel.MetricsHandler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body, _ := io.ReadAll(rec.Body)
	text := string(body)
	if !strings.Contains(text, `tags_queue_depth{otel_scope_name="tags",otel_scope_schema_url="",otel_scope_version="",robot_id="r1"} 3`) &&
		!lineHas(text, "tags_queue_depth{", `robot_id="r1"`, "} 3") {
		t.Errorf("gauge is not the last value set (3):\n%s", grep(text, "queue_depth"))
	}
	if !lineHas(text, "tags_latency_ms_milliseconds_count{", `robot_id="r1"`, "} 3") && !lineHas(text, "tags_latency_ms_count{", `robot_id="r1"`, "} 3") {
		t.Errorf("histogram lost its labels:\n%s", grep(text, "latency"))
	}
}

func lineHas(text string, parts ...string) bool {
	for line := range strings.SplitSeq(text, "\n") {
		ok := true
		for _, p := range parts {
			ok = ok && strings.Contains(line, p)
		}
		if ok {
			return true
		}
	}
	return false
}

func grep(text, sub string) string {
	var out []string
	for line := range strings.SplitSeq(text, "\n") {
		if strings.Contains(line, sub) {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

func TestSpanAttributesKeepTheirKind(t *testing.T) {
	tel, rec := recorded(t)
	type state int32
	_, s := tel.Tracing.StartSpan(context.Background(), "typed",
		telemetry.WithSpanAttributes(map[string]any{"attempt": int32(5), "state": state(2), "ratio": float32(0.5), "ok": true}))
	s.End()
	a := rec.Spans()[0].Attributes
	if a["attempt"] != int64(5) || a["state"] != int64(2) || a["ratio"] != float64(0.5) || a["ok"] != true {
		t.Errorf("attributes %v", a)
	}
}

// Benchmarks for the cross-SDK table (see BENCHMARKS.md): the cost a program
// pays per call, with nothing exported (the pipeline's own overhead) and
// exporting to an in-process collector.

func benchTelemetry(b *testing.B, export bool) *telemetry.Telemetry {
	b.Helper()
	if export {
		tel, _ := exporting(b, telemetry.ExportOptions{QueueSize: 1 << 16})
		b.Cleanup(func() { _ = tel.Close() })
		return tel
	}
	b.Setenv("OTEL_EXPORTER_OTLP_ENABLED", "false")
	tel, err := telemetry.New().WithService("bench", "1.0.0").WithTracing().WithLogHandler(slog.DiscardHandler).Build()
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = tel.Close() })
	return tel
}

func BenchmarkLogInfo(b *testing.B) {
	for _, export := range []bool{false, true} {
		b.Run("export="+strconv.FormatBool(export), func(b *testing.B) {
			tel := benchTelemetry(b, export)
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					tel.Logger.Info("load", map[string]any{"user_id": "alice", "n": 1})
				}
			})
		})
	}
}

func BenchmarkSpan(b *testing.B) {
	for _, export := range []bool{false, true} {
		b.Run("export="+strconv.FormatBool(export), func(b *testing.B) {
			tel := benchTelemetry(b, export)
			ctx := context.Background()
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					_, s := tel.Tracing.StartSpan(ctx, "op")
					s.End()
				}
			})
		})
	}
}

func BenchmarkCounter(b *testing.B) {
	tel := benchTelemetry(b, true)
	type ops struct {
		N int `telemetry:"metric:counter:ops"`
	}
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = tel.Metrics.Record(ops{N: 1})
		}
	})
}

// Several exporters share the records and still lose none.
func TestShardedExportersDeliverEverything(t *testing.T) {
	tel, col := exporting(t, telemetry.ExportOptions{Lossless: true, QueueSize: 128, BatchSize: 32, Exporters: 3})
	emit(tel, 30_000, 6)
	if err := tel.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := col.Counts(); got.Logs != 30_000 || got.Spans != 30_000 {
		t.Errorf("collector got %d logs, %d spans; want 30000 each", got.Logs, got.Spans)
	}
}

// Distributed logging: a record logged under a span carries that span's
// trace id to the collector — through Slog's InfoContext and through the
// Logger bound to the context alike — so a trace finds its logs in every
// service it crossed.
func TestLogsCarryTheTraceTheyWereWrittenUnder(t *testing.T) {
	tel, col := exporting(t, telemetry.ExportOptions{})
	ctx, span := tel.Tracing.StartSpan(context.Background(), "checkout")
	tel.Slog().InfoContext(ctx, "via slog", "order_id", "o-1")
	tel.Logger.WithContext(ctx).Info("via Logger", map[string]any{"order_id": "o-1"})
	tel.Logger.Info("outside any span")
	span.End()
	headers := map[string]string{}
	tel.Tracing.Inject(ctx, headers)
	if err := tel.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	traceID := strings.Split(headers["traceparent"], "-")[1]
	if got := col.Counts().LogTraceIDs; got[traceID] != 2 || len(got) != 1 {
		t.Errorf("log records by trace id %v; want 2 under %s and none under another trace", got, traceID)
	}
}

func TestBaggageTravelsInTheHeaders(t *testing.T) {
	tel, _ := recorded(t)
	ctx, err := tel.Tracing.SetBaggage(context.Background(), "tenant", "acme")
	if err != nil {
		t.Fatal(err)
	}
	ctx, span := tel.Tracing.StartSpan(ctx, "call")
	defer span.End()
	headers := map[string]string{}
	tel.Tracing.Inject(ctx, headers)
	if headers["baggage"] != "tenant=acme" {
		t.Fatalf("headers %v", headers)
	}
	if got := tel.Tracing.Baggage(tel.Tracing.Extract(context.Background(), headers)); got["tenant"] != "acme" {
		t.Errorf("extracted baggage %v", got)
	}
}
