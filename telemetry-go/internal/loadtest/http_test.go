package loadtest

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	coltrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

func TestHTTPReceiverCountsLikeGRPC(t *testing.T) {
	c := &Collector{sums: map[string]float64{}, logTraces: map[string]int{}}
	srv := httptest.NewServer(c.HTTPHandler())
	defer srv.Close()

	req := &coltrace.ExportTraceServiceRequest{ResourceSpans: []*tracepb.ResourceSpans{{
		ScopeSpans: []*tracepb.ScopeSpans{{Spans: []*tracepb.Span{{Name: "a"}, {Name: "b"}, {Name: "c"}}}},
	}}}
	body, _ := proto.Marshal(req)
	resp, err := http.Post(srv.URL+"/v1/traces", "application/x-protobuf", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if got := c.Counts(); got.Spans != 3 || got.SpanRequests != 1 {
		t.Errorf("counts %+v, want 3 spans in 1 request", got)
	}
}
