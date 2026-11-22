package telemetry_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	telemetry "github.com/the-protobuf-project/telemetry/telemetry-go"
)

// What another language's SDK sends: a sampled trace this process never saw.
const (
	upstreamTrace = "0af7651916cd43dd8448eb211c80319c"
	upstreamSpan  = "b7ad6b7169203331"
	upstream      = "00-" + upstreamTrace + "-" + upstreamSpan + "-01"
)

// headerServer answers every request, keeping the last traceparent it got.
func headerServer(t *testing.T, wrap func(http.Handler) http.Handler, then func(*http.Request)) (*httptest.Server, *string) {
	t.Helper()
	var got string
	var h http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("traceparent")
		if then != nil {
			then(r)
		}
		_, _ = w.Write([]byte("ok"))
	})
	if wrap != nil {
		h = wrap(h)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, &got
}

func get(t *testing.T, ctx context.Context, rt http.RoundTripper, url string, header map[string]string) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	res, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
}

func traceOf(traceparent string) string {
	parts := strings.Split(traceparent, "-")
	if len(parts) != 4 {
		return ""
	}
	return parts[1]
}

func spanOf(spans []telemetry.RecordedSpan, match func(telemetry.RecordedSpan) bool) (telemetry.RecordedSpan, bool) {
	for _, s := range spans {
		if match(s) {
			return s, true
		}
	}
	return telemetry.RecordedSpan{}, false
}

func TestTheDefaultClientCarriesTheTrace(t *testing.T) {
	tel, rec := recorded(t)
	srv, got := headerServer(t, func(h http.Handler) http.Handler { return tel.HTTPHandler("inventory", h) }, nil)

	ctx, checkout := tel.Tracing.StartSpan(context.Background(), "checkout")
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	checkout.End()

	spans := rec.Spans()
	root, _ := spanOf(spans, func(s telemetry.RecordedSpan) bool { return s.Name == "checkout" })
	if traceOf(*got) != root.TraceID {
		t.Fatalf("the server got traceparent %q, not trace %s", *got, root.TraceID)
	}
	client, ok := spanOf(spans, func(s telemetry.RecordedSpan) bool { return s.Kind == telemetry.SpanKindClient })
	if !ok || client.ParentSpanID != root.SpanID {
		t.Fatalf("no client span under checkout: %+v", spans)
	}
	server, ok := spanOf(spans, func(s telemetry.RecordedSpan) bool { return s.Kind == telemetry.SpanKindServer })
	if !ok || server.ParentSpanID != client.SpanID || server.TraceID != root.TraceID {
		t.Fatalf("the server span does not continue the client's: %+v", spans)
	}
}

func TestAServerContinuesAnotherServicesTraceAndHandsItOn(t *testing.T) {
	tel, rec := recorded(t)
	next, onward := headerServer(t, nil, nil)
	here, _ := headerServer(t,
		func(h http.Handler) http.Handler { return tel.HTTPHandler("orders", h) },
		func(r *http.Request) { get(t, r.Context(), tel.HTTPTransport(nil), next.URL, nil) })

	// As from a service in another language: plain transport, its own traceparent.
	get(t, context.Background(), http.DefaultTransport, here.URL, map[string]string{"traceparent": upstream})

	if traceOf(*onward) != upstreamTrace {
		t.Fatalf("the next service got %q, not trace %s", *onward, upstreamTrace)
	}
	server, ok := spanOf(rec.Spans(), func(s telemetry.RecordedSpan) bool { return s.Kind == telemetry.SpanKindServer })
	if !ok || server.TraceID != upstreamTrace || server.ParentSpanID != upstreamSpan {
		t.Fatalf("the server span does not continue the caller's: %+v", rec.Spans())
	}
}

func TestGRPCCarriesTheTrace(t *testing.T) {
	tel, rec := recorded(t)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer(tel.GRPCServerOptions()...)
	healthpb.RegisterHealthServer(srv, health.NewServer())
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient(lis.Addr().String(),
		append(tel.GRPCDialOptions(), grpc.WithTransportCredentials(insecure.NewCredentials()))...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	ctx, checkout := tel.Tracing.StartSpan(context.Background(), "checkout")
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{}); err != nil {
		t.Fatal(err)
	}
	checkout.End()

	// The server span ends after the client has its answer.
	deadline := time.Now().Add(2 * time.Second)
	var server telemetry.RecordedSpan
	var ok bool
	for ; !ok && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		server, ok = spanOf(rec.Spans(), func(s telemetry.RecordedSpan) bool { return s.Kind == telemetry.SpanKindServer })
	}
	spans := rec.Spans()
	root, _ := spanOf(spans, func(s telemetry.RecordedSpan) bool { return s.Name == "checkout" })
	client, _ := spanOf(spans, func(s telemetry.RecordedSpan) bool { return s.Kind == telemetry.SpanKindClient })
	if client.ParentSpanID != root.SpanID || server.ParentSpanID != client.SpanID || server.TraceID != root.TraceID {
		t.Fatalf("checkout → client → server is not one trace: %+v", spans)
	}
	if server.Name != "grpc.health.v1.Health/Check" {
		t.Errorf("server span %q", server.Name)
	}
}

func TestAutoInstrumentationCanBeTurnedOff(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENABLED", "false")
	before := http.DefaultClient.Transport
	tel, err := telemetry.New().WithService("off", "1.0.0").WithAutoInstrumentation(false).Build()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tel.Close() }()
	if http.DefaultClient.Transport != before {
		t.Fatal("http.DefaultClient was wrapped with auto-instrumentation off")
	}

	on, err := telemetry.New().WithService("on", "1.0.0").Build()
	if err != nil {
		t.Fatal(err)
	}
	if http.DefaultClient.Transport == before {
		t.Fatal("http.DefaultClient was not wrapped")
	}
	_ = on.Close()
	if http.DefaultClient.Transport != before {
		t.Fatal("Close did not restore http.DefaultClient")
	}
}
