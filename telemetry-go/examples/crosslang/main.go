// Service A of the cross-language tracing demo: a Go service starts a trace
// and sends it to service B — written in Rust, Python or anything else that
// speaks W3C Trace Context — in the headers of its request.
//
// Here the "request" is stdout, so the demo needs no network:
//
//	go run ./examples/crosslang | cargo run -q -p telemetry-examples --bin crosslang
//
// In a real service the same map goes on the HTTP request, gRPC metadata or
// message headers. Set TELEMETRY_DEMO_OTLP=localhost:4317 on both sides to
// export the spans and see one trace spanning both services in Grafana Tempo.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"

	telemetry "github.com/the-protobuf-project/telemetry/telemetry-go"
)

func main() {
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
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer func() { _ = tel.Close() }()

	ctx, span := tel.Tracing.StartSpan(context.Background(), "checkout",
		telemetry.WithSpanKind(telemetry.SpanKindClient),
		telemetry.WithSpanAttributes(map[string]any{"order.id": "o-42"}))
	defer span.End()

	// Everything service B needs to continue the trace: traceparent,
	// tracestate and baggage.
	headers := map[string]string{}
	tel.Tracing.Inject(ctx, headers)

	tel.Slog().InfoContext(ctx, "calling inventory service", "traceparent", headers["traceparent"])
	_ = json.NewEncoder(os.Stdout).Encode(headers)
}
