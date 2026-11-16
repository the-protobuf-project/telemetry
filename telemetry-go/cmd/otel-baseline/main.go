// Command otel-baseline writes the load telemetry-loadgen writes — n log
// records, spans or counter increments — through the upstream OpenTelemetry
// Go SDK alone, with its batch processors and OTLP/gRPC exporters, and prints
// one JSON line with how long it took. bench/bench_all.py runs it beside
// telemetry-loadgen to compare this SDK with plain OpenTelemetry. The batch
// processors read the standard OTEL_BSP_* and OTEL_BLRP_* variables, which is
// how the bench tunes them.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	otellog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func main() {
	signal := flag.String("signal", "logs", "logs, spans or metrics")
	n := flag.Int("n", 1_000_000, "records to write")
	endpoint := flag.String("endpoint", "127.0.0.1:4317", "OTLP gRPC host:port")
	workers := flag.Int("workers", 1, "goroutines writing concurrently")
	flag.Parse()
	if err := run(*signal, *n, *workers, *endpoint); err != nil {
		fmt.Fprintln(os.Stderr, "otel-baseline:", err)
		os.Exit(1)
	}
}

func run(signal string, n, workers int, endpoint string) error {
	ctx := context.Background()
	var emit func(worker, i int)
	var shutdown func(context.Context) error

	switch signal {
	case "logs":
		exp, err := otlploggrpc.New(ctx, otlploggrpc.WithEndpoint(endpoint), otlploggrpc.WithInsecure())
		if err != nil {
			return err
		}
		lp := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewBatchProcessor(exp)))
		logger := lp.Logger("otel-baseline")
		emit = func(w, i int) {
			var r otellog.Record
			r.SetTimestamp(time.Now())
			r.SetSeverity(otellog.SeverityInfo)
			r.SetBody(attribute.StringValue("load"))
			r.AddAttributes(attribute.Int("i", i), attribute.Int("worker", w))
			logger.Emit(ctx, r)
		}
		shutdown = lp.Shutdown
	case "spans":
		exp, err := otlptracegrpc.New(ctx, otlptracegrpc.WithEndpoint(endpoint), otlptracegrpc.WithInsecure())
		if err != nil {
			return err
		}
		tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exp))
		tracer := tp.Tracer("otel-baseline")
		emit = func(int, int) {
			_, span := tracer.Start(ctx, "load.op")
			span.End()
		}
		shutdown = tp.Shutdown
	case "metrics":
		exp, err := otlpmetricgrpc.New(ctx, otlpmetricgrpc.WithEndpoint(endpoint), otlpmetricgrpc.WithInsecure())
		if err != nil {
			return err
		}
		mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exp)))
		counter, err := mp.Meter("otel-baseline").Float64Counter("loadgen.ops")
		if err != nil {
			return err
		}
		emit = func(int, int) { counter.Add(ctx, 1) }
		shutdown = mp.Shutdown
	default:
		return fmt.Errorf("unknown signal %q", signal)
	}

	start := time.Now()
	var wg sync.WaitGroup
	per := n / workers
	for w := range workers {
		count := per
		if w == workers-1 {
			count = n - per*(workers-1)
		}
		wg.Go(func() {
			for i := range count {
				emit(w, i)
			}
		})
	}
	wg.Wait()
	emitted := time.Since(start)

	start = time.Now()
	sctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	err := shutdown(sctx)
	closed := time.Since(start)

	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"lang": "go", "signal": signal, "n": n, "workers": workers,
		"emit_s": emitted.Seconds(), "close_s": closed.Seconds(),
		"close_error": errString(err),
	})
}

func errString(err error) any {
	if err == nil {
		return nil
	}
	return err.Error()
}
