// The orders service of the cross-language tracing demo (`just crosslang`):
// it starts a trace and calls the next service with http.DefaultClient,
// which Build instruments to carry the trace on. There is no inject of its
// own: the request leaves with the W3C traceparent, and every service after
// it, in Rust, Python, C++ and TypeScript, continues the same trace.
//
// CROSSLANG_NEXT is the next service's URL; the program prints this
// service's trace and every hop's view of it, as JSON. Set
// TELEMETRY_DEMO_OTLP=localhost:4317 to export the spans to a collector.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"

	"go.opentelemetry.io/otel/trace"

	telemetry "github.com/the-protobuf-project/telemetry/telemetry-go"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	b := telemetry.New().
		WithService("orders-go", "1.0.0").
		WithLogHandler(slog.NewTextHandler(os.Stderr, nil))
	if host, port, err := net.SplitHostPort(os.Getenv("TELEMETRY_DEMO_OTLP")); err == nil {
		p, _ := strconv.Atoi(port)
		b = b.WithOTLP(host, p).WithTracing()
	} else {
		// No collector: record spans in memory, so they still get real ids.
		b = b.WithSpanRecorder(telemetry.NewSpanRecorder())
	}
	tel, err := b.Build()
	if err != nil {
		return err
	}
	defer func() { _ = tel.Close() }()

	ctx, span := tel.Tracing.StartSpan(context.Background(), "checkout",
		telemetry.WithSpanAttributes(map[string]any{"order.id": "o-42"}))
	defer span.End()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, os.Getenv("CROSSLANG_NEXT")+"/hop", nil)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req) // carries the trace: no inject here
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("the next service answered %s", res.Status)
	}
	var hops []map[string]any
	if err := json.NewDecoder(res.Body).Decode(&hops); err != nil {
		return err
	}
	sc := trace.SpanContextFromContext(ctx)
	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"service":  "orders-go",
		"trace_id": sc.TraceID().String(),
		"span_id":  sc.SpanID().String(),
		"hops":     hops,
	})
}
