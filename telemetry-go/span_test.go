package telemetry_test

import (
	"context"
	"errors"
	"testing"
	"time"

	telemetry "github.com/the-protobuf-project/telemetry/telemetry-go"
)

// recorded builds a Telemetry whose spans land in a SpanRecorder, with no
// exporter dialling out.
func recorded(t *testing.T) (*telemetry.Telemetry, *telemetry.SpanRecorder) {
	t.Helper()
	t.Setenv("OTEL_EXPORTER_OTLP_ENABLED", "false")
	rec := telemetry.NewSpanRecorder()
	tel, err := telemetry.New().
		WithService("span-test", "1.0.0").
		WithSpanRecorder(rec).
		WithShutdownTimeout(time.Second).
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(func() { _ = tel.Close() })
	return tel, rec
}

func TestATraceCrossesHeadersIntoAChildSpan(t *testing.T) {
	tel, rec := recorded(t)

	ctx, send := tel.Tracing.StartSpan(context.Background(), "streams.send",
		telemetry.WithSpanKind(telemetry.SpanKindProducer),
		telemetry.WithSpanAttributes(map[string]interface{}{"messaging.destination.name": "order.placed", "size": 12}))
	headers := map[string]string{}
	tel.Tracing.Inject(ctx, headers)
	send.SetOK()
	send.End()
	if headers["traceparent"] == "" {
		t.Fatalf("no traceparent injected: %v", headers)
	}

	received := time.Now().Add(-time.Millisecond)
	remote := tel.Tracing.Extract(context.Background(), headers)
	_, process := tel.Tracing.StartSpan(remote, "streams.process",
		telemetry.WithSpanKind(telemetry.SpanKindConsumer), telemetry.WithStartTime(received))
	process.SetError(errors.New("handler failed"))
	process.End()

	spans := rec.Spans()
	if len(spans) != 2 {
		t.Fatalf("spans %d", len(spans))
	}
	s, p := spans[0], spans[1]
	if s.Kind != telemetry.SpanKindProducer || p.Kind != telemetry.SpanKindConsumer {
		t.Errorf("kinds %v %v", s.Kind, p.Kind)
	}
	if p.TraceID != s.TraceID || p.ParentSpanID != s.SpanID {
		t.Error("the process span is not the send span's child")
	}
	if !p.Start.Equal(received) {
		t.Errorf("start %v, want %v", p.Start, received)
	}
	if !s.OK || !p.Failed || p.Status != "handler failed" {
		t.Errorf("statuses: send ok %v, process failed %v %q", s.OK, p.Failed, p.Status)
	}
	if s.Attributes["size"] != int64(12) || s.Attributes["messaging.destination.name"] != "order.placed" {
		t.Errorf("attributes %v", s.Attributes)
	}
}

func TestStartSpanLinksTheGivenContexts(t *testing.T) {
	tel, rec := recorded(t)

	a, sa := tel.Tracing.StartSpan(context.Background(), "producer.a")
	b, sb := tel.Tracing.StartSpan(context.Background(), "producer.b")
	sa.End()
	sb.End()
	_, batch := tel.Tracing.StartSpan(context.Background(), "batch", telemetry.WithLinks(a, b, context.Background()))
	batch.End()

	spans := rec.Spans()
	if got := spans[2].Links; len(got) != 2 || got[0] != spans[0].SpanID || got[1] != spans[1].SpanID {
		t.Errorf("links %v, want the two producers (a context without a span is skipped)", got)
	}
}

func TestSpansStayReadableAfterClose(t *testing.T) {
	tel, rec := recorded(t)
	_, s := tel.Tracing.StartSpan(context.Background(), "work")
	s.End()
	if err := tel.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if n := len(rec.Spans()); n != 1 {
		t.Errorf("spans after close %d", n)
	}
}

func TestWithTracingOffStartSpanLeavesTheCallersSpanAlone(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENABLED", "false")
	rec := telemetry.NewSpanRecorder()
	traced, err := telemetry.New().WithService("on", "1").WithSpanRecorder(rec).WithShutdownTimeout(time.Second).Build()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = traced.Close() }()
	untraced, err := telemetry.New().WithService("off", "1").WithShutdownTimeout(time.Second).Build()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = untraced.Close() }()

	ctx, parent := traced.Tracing.StartSpan(context.Background(), "parent")
	_, child := untraced.Tracing.StartSpan(ctx, "child")
	child.End()
	if n := len(rec.Spans()); n != 0 {
		t.Fatalf("ending a disabled span ended the caller's: %d spans", n)
	}
	parent.End()
	if n := len(rec.Spans()); n != 1 {
		t.Errorf("spans %d", n)
	}
}
