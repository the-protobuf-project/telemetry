// The orders service of the cross-language tracing demo (`just crosslang`):
// a checkout that loads the cart, prices it and places the order with the
// inventory service, which takes it on through billing, shipping and
// notification, each in another language. The call goes out through
// http.DefaultClient, which Build instruments to carry the trace on: there is
// no inject here, and every service after it continues the same trace.
//
// Each step is a span of its own, logs under it (so the logs carry its trace
// and span id) and counts or measures what it did, so the whole journey shows
// in Grafana as one trace, with its logs and metrics beside it.
//
// CROSSLANG_NEXT is the next service's URL; the program prints this
// service's trace and every hop's view of it, as JSON. Set
// TELEMETRY_DEMO_OTLP=localhost:4317 to export to a collector.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/trace"

	telemetry "github.com/the-protobuf-project/telemetry/telemetry-go"
)

// The order every run places.
var (
	orderID = "o-42"
	items   = []string{"sku-1", "sku-2"}
	prices  = map[string]float64{"sku-1": 34.95, "sku-2": 24.95}
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
	log := tel.Slog()

	ctx, checkout := tel.Tracing.StartSpan(context.Background(), "checkout",
		telemetry.WithSpanAttributes(map[string]any{"order.id": orderID}))
	defer checkout.End()

	// Load the cart.
	cctx, cart := tel.Tracing.StartSpan(ctx, "cart.load",
		telemetry.WithSpanAttributes(map[string]any{"order.id": orderID, "cart.items": len(items)}))
	time.Sleep(8 * time.Millisecond)
	log.InfoContext(cctx, "cart loaded", "order.id", orderID, "items", strings.Join(items, ","))
	cart.End()

	// Price it.
	pctx, pricing := tel.Tracing.StartSpan(ctx, "pricing.compute")
	total := 0.0
	for _, sku := range items {
		total += prices[sku]
	}
	time.Sleep(5 * time.Millisecond)
	pricing.SetAttribute("order.total", total)
	tel.Metrics.Histogram("order_value").Record(total)
	log.InfoContext(pctx, "order priced", "order.id", orderID, "total", total)
	pricing.End()

	// Place it with inventory, which hands it on down the chain.
	query := url.Values{
		"order": {orderID},
		"items": {strings.Join(items, ",")},
		"total": {strconv.FormatFloat(total, 'f', 2, 64)},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, os.Getenv("CROSSLANG_NEXT")+"/hop?"+query.Encode(), nil)
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
	tel.Metrics.Counter("orders_placed_total").Add(1)
	log.InfoContext(ctx, "order placed", "order.id", orderID, "services", len(hops))
	checkout.SetOK()

	sc := trace.SpanContextFromContext(ctx)
	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"service":  "orders-go",
		"trace_id": sc.TraceID().String(),
		"span_id":  sc.SpanID().String(),
		"hops":     hops,
	})
}
