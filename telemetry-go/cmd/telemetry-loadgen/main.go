// Command telemetry-loadgen writes a large number of logs, spans and metric
// measurements through the SDK's public API and reports how many reached the
// collector, how fast, and at what cost — with the profiles to explain it.
//
// With no -endpoint it starts an in-process OTLP collector that counts what it
// receives, so "emitted vs delivered" is exact:
//
//	go run ./cmd/telemetry-loadgen -n 1000000 -signals logs,spans,metrics
//
// Point it at a real collector to load-test the whole pipeline:
//
//	go run ./cmd/telemetry-loadgen -n 1000000 -endpoint localhost:4317
//
// Profiling, for the root cause when a run falls short:
//
//	-cpuprofile cpu.out -memprofile mem.out -mutexprofile mutex.out \
//	-blockprofile block.out -trace trace.out -pprof localhost:6060
//
// then `go tool pprof -http=: cpu.out` or `go tool trace trace.out`.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	_ "net/http/pprof" // live profiles at -pprof
	"os"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	telemetry "github.com/the-protobuf-project/telemetry/telemetry-go"
	"github.com/the-protobuf-project/telemetry/telemetry-go/internal/loadtest"
)

type loadMetric struct {
	Ops int `telemetry:"metric:counter:ops"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "telemetry-loadgen:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		n            = flag.Int("n", 1_000_000, "records to write per signal")
		workers      = flag.Int("workers", runtime.GOMAXPROCS(0), "goroutines writing concurrently")
		signals      = flag.String("signals", "logs,spans,metrics", "comma-separated: logs, spans, metrics")
		endpoint     = flag.String("endpoint", "", "OTLP gRPC host:port; empty starts an in-process counting collector")
		delay        = flag.Duration("collector-delay", 0, "in-process collector: hold each export this long (a slow backend)")
		console      = flag.Bool("console", false, "also print every log line to stderr (slow; off measures the pipeline)")
		lossless     = flag.Bool("lossless", false, "block producers when an export queue is full instead of dropping")
		queue        = flag.Int("queue", 0, "export queue size per signal; 0 keeps the SDK default")
		batch        = flag.Int("batch", 0, "export batch size per signal; 0 keeps the SDK default")
		exporters    = flag.Int("exporters", 1, "exporters sharing each signal's records (ExportOptions.Exporters)")
		mcapPath     = flag.String("mcap", "", "also record every log, metric and span to this MCAP file")
		otlpOn       = flag.Bool("otlp", true, "export over OTLP; -otlp=false with -mcap measures MCAP recording alone")
		shutdown     = flag.Duration("shutdown", 60*time.Second, "how long Close may take to flush")
		cpuProfile   = flag.String("cpuprofile", "", "write a CPU profile here")
		memProfile   = flag.String("memprofile", "", "write a heap (allocs) profile here")
		mutexProfile = flag.String("mutexprofile", "", "write a mutex contention profile here")
		blockProfile = flag.String("blockprofile", "", "write a goroutine blocking profile here")
		traceFile    = flag.String("trace", "", "write a runtime execution trace here")
		pprofAddr    = flag.String("pprof", "", "serve net/http/pprof on this address while running, e.g. localhost:6060")
		jsonOut      = flag.Bool("json", false, "print one JSON line per signal instead of the table (for bench/bench_all.py)")
		serve        = flag.String("serve", "", "only run the counting collector on this OTLP gRPC address (for other SDKs' load tests)")
		countsAddr   = flag.String("counts", "127.0.0.1:4320", "with -serve: HTTP address answering GET /counts (JSON) and POST /reset")
		httpAddr     = flag.String("http", "", "with -serve: also receive OTLP/HTTP on this address, e.g. 127.0.0.1:4318")
		recordAPI    = flag.Bool("record-api", false, "metrics through the struct-tag Record API instead of a Counter handle")
	)
	flag.Parse()

	if *serve != "" {
		return serveCollector(*serve, *countsAddr, *httpAddr, *delay)
	}

	want := map[string]bool{}
	for s := range strings.SplitSeq(*signals, ",") {
		want[strings.TrimSpace(s)] = true
	}

	stopProfiles, err := startProfiles(*cpuProfile, *traceFile, *mutexProfile, *blockProfile, *pprofAddr)
	if err != nil {
		return err
	}

	addr := *endpoint
	var col *loadtest.Collector
	if !*otlpOn {
		addr = "127.0.0.1:0" // unused
	} else if addr == "" {
		if col, err = loadtest.Start(); err != nil {
			return err
		}
		col.Delay = *delay
		defer col.Stop()
		addr = col.Addr()
	}
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("endpoint %q: %w", addr, err)
	}
	port, _ := strconv.Atoi(portStr)

	b := telemetry.New().
		WithService("loadgen", "1.0.0").
		WithTracing().
		WithShutdownTimeout(*shutdown).
		WithExport(telemetry.ExportOptions{Lossless: *lossless, QueueSize: *queue, BatchSize: *batch, Exporters: *exporters})
	if *otlpOn {
		b = b.WithOTLP(host, port)
	}
	if *mcapPath != "" {
		b = b.WithMCAP(*mcapPath)
	}
	if !*console {
		b = b.WithLogHandler(slog.NewTextHandler(io.Discard, nil))
	}
	tel, err := b.Build()
	if err != nil {
		return err
	}

	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	ctx := context.Background()
	start := time.Now()
	durations := map[string]time.Duration{}
	for _, sig := range []string{"logs", "spans", "metrics"} {
		if !want[sig] {
			continue
		}
		var emit func(w, i int)
		switch sig {
		case "logs":
			emit = func(w, i int) { tel.Logger.Info("load", map[string]any{"i": i, "worker": w}) }
		case "spans":
			emit = func(int, int) {
				_, s := tel.Tracing.StartSpan(ctx, "load.op")
				s.End()
			}
		case "metrics":
			// The handle API recommended for a hot path; -record-api measures
			// the struct-tag API instead.
			if *recordAPI {
				emit = func(int, int) { _ = tel.Metrics.Record(loadMetric{Ops: 1}) }
			} else {
				ops := tel.Metrics.Counter("ops")
				emit = func(int, int) { ops.Add(1) }
			}
		}
		t0 := time.Now()
		fanOut(*n, *workers, emit)
		durations[sig] = time.Since(t0)
	}
	emitted := time.Since(start)
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	t0 := time.Now()
	closeErr := tel.Close()
	flushed := time.Since(t0)
	stats := tel.Stats()

	stopProfiles()
	if *memProfile != "" {
		if err := writeProfile("allocs", *memProfile); err != nil {
			return err
		}
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		for _, sig := range []string{"logs", "spans", "metrics"} {
			if want[sig] {
				_ = enc.Encode(map[string]any{
					"lang": "go", "signal": sig, "n": *n, "workers": *workers,
					"emit_s": durations[sig].Seconds(), "close_s": flushed.Seconds(),
					"dropped_reported": dropped(stats, sig),
				})
			}
		}
		return nil
	}

	fmt.Printf("\n%-8s %12s %12s %12s %10s %14s\n", "signal", "emitted", "delivered", "dropped", "lost %", "emit rate/s")
	var got loadtest.Counts
	if col != nil {
		got = col.Counts()
	}
	for _, sig := range []string{"logs", "spans", "metrics"} {
		if !want[sig] {
			continue
		}
		delivered := "-"
		lost := "-"
		var d int64 = -1
		if col != nil {
			switch sig {
			case "logs":
				d = got.Logs
			case "spans":
				d = got.Spans
			case "metrics":
				d = int64(got.Sums["loadgen.ops"])
			}
		}
		if d >= 0 {
			delivered = strconv.FormatInt(d, 10)
			lost = fmt.Sprintf("%.2f", 100*float64(int64(*n)-d)/float64(*n))
		}
		fmt.Printf("%-8s %12d %12s %12d %10s %14.0f\n", sig, *n, delivered, dropped(stats, sig), lost,
			float64(*n)/durations[sig].Seconds())
	}
	fmt.Printf("\nemit %v · flush on Close %v · %.0f B/op · %.1f allocs/op · %d GCs\n",
		emitted.Round(time.Millisecond), flushed.Round(time.Millisecond),
		float64(after.TotalAlloc-before.TotalAlloc)/float64(*n*len(durations)),
		float64(after.Mallocs-before.Mallocs)/float64(*n*len(durations)),
		after.NumGC-before.NumGC)
	if col != nil {
		fmt.Printf("collector requests: logs %d · spans %d · metrics %d\n", got.LogRequests, got.SpanRequests, got.MetricRequests)
	}
	fmt.Printf("SDK stats: %+v\n", stats)
	if closeErr != nil {
		fmt.Printf("Close: %v\n", closeErr)
	}
	return nil
}

// serveCollector runs the counting collector alone until interrupted, so a
// load test in any language can export to it and read back what arrived.
func serveCollector(addr, countsAddr, httpAddr string, delay time.Duration) error {
	col, err := loadtest.StartAt(addr)
	if err != nil {
		return err
	}
	defer col.Stop()
	col.Delay = delay
	if httpAddr != "" {
		go func() { _ = http.ListenAndServe(httpAddr, col.HTTPHandler()) }()
		fmt.Fprintf(os.Stderr, "collector: OTLP/HTTP on %s\n", httpAddr)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /counts", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(col.Counts())
	})
	mux.HandleFunc("POST /reset", func(w http.ResponseWriter, _ *http.Request) { col.Reset() })
	fmt.Fprintf(os.Stderr, "collector: OTLP gRPC on %s, counts on http://%s/counts\n", col.Addr(), countsAddr)
	return http.ListenAndServe(countsAddr, mux)
}

func dropped(s telemetry.Stats, signal string) int64 {
	switch signal {
	case "logs":
		return s.Logs.Dropped
	case "spans":
		return s.Spans.Dropped
	}
	return 0
}

// fanOut calls fn n times across workers goroutines.
func fanOut(n, workers int, fn func(worker, i int)) {
	var wg sync.WaitGroup
	per := n / workers
	for w := range workers {
		count := per
		if w == workers-1 {
			count = n - per*(workers-1)
		}
		wg.Go(func() {
			for i := range count {
				fn(w, i)
			}
		})
	}
	wg.Wait()
}

func startProfiles(cpu, traceOut, mutex, block, addr string) (stop func(), err error) {
	var stops []func()
	stop = func() {
		for _, stop := range slices.Backward(stops) {
			stop()
		}
	}
	if mutex != "" {
		runtime.SetMutexProfileFraction(5)
		stops = append(stops, func() { _ = writeProfile("mutex", mutex) })
	}
	if block != "" {
		runtime.SetBlockProfileRate(int(time.Microsecond))
		stops = append(stops, func() { _ = writeProfile("block", block) })
	}
	if cpu != "" {
		f, err := os.Create(cpu)
		if err != nil {
			return nil, err
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			return nil, err
		}
		stops = append(stops, func() { pprof.StopCPUProfile(); _ = f.Close() })
	}
	if traceOut != "" {
		f, err := os.Create(traceOut)
		if err != nil {
			return nil, err
		}
		if err := trace.Start(f); err != nil {
			return nil, err
		}
		stops = append(stops, func() { trace.Stop(); _ = f.Close() })
	}
	if addr != "" {
		go func() { _ = http.ListenAndServe(addr, nil) }()
		fmt.Fprintf(os.Stderr, "pprof on http://%s/debug/pprof/\n", addr)
	}
	return stop, nil
}

func writeProfile(name, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := pprof.Lookup(name).WriteTo(f, 0); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
