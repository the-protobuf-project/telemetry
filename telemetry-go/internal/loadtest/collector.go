// Package loadtest holds what the SDK's load tests and the telemetry-loadgen
// command share: an OTLP collector that counts what it receives, so a run can
// compare what the program emitted with what actually arrived.
package loadtest

import (
	"context"
	"encoding/hex"
	"maps"
	"net"
	"sync"
	"sync/atomic"
	"time"

	collogs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colmetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	coltrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
)

// Collector is an in-process OTLP/gRPC receiver that counts log records,
// spans and requests, and keeps the latest cumulative value of every sum
// metric, so a counter's total can be checked against what was added.
type Collector struct {
	collogs.UnimplementedLogsServiceServer
	coltrace.UnimplementedTraceServiceServer
	colmetrics.UnimplementedMetricsServiceServer

	// Delay holds every export this long before answering, to stand in for
	// a slow or overloaded backend. Set it before the first export.
	Delay time.Duration

	logs, spans       atomic.Int64
	logReqs, spanReqs atomic.Int64
	metricReqs        atomic.Int64
	mu                sync.Mutex
	sums              map[string]float64
	logTraces         map[string]int // trace id of log records -> count, first 1000 ids
	server            *grpc.Server
	listener          net.Listener
}

// Start listens on a free localhost port and serves until Stop.
func Start() (*Collector, error) { return StartAt("127.0.0.1:0") }

// StartAt listens on addr and serves until Stop.
func StartAt(addr string) (*Collector, error) {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	c := &Collector{
		sums:      map[string]float64{},
		logTraces: map[string]int{},
		listener:  lis,
		server:    grpc.NewServer(grpc.MaxRecvMsgSize(64 << 20)),
	}
	collogs.RegisterLogsServiceServer(c.server, c)
	coltrace.RegisterTraceServiceServer(c.server, traceServer{c})
	colmetrics.RegisterMetricsServiceServer(c.server, metricsServer{c})
	go func() { _ = c.server.Serve(lis) }()
	return c, nil
}

// Addr is the host:port the collector listens on.
func (c *Collector) Addr() string { return c.listener.Addr().String() }

// Stop stops serving.
func (c *Collector) Stop() { c.server.Stop() }

// Reset forgets everything received so far, between runs.
func (c *Collector) Reset() {
	c.logs.Store(0)
	c.spans.Store(0)
	c.logReqs.Store(0)
	c.spanReqs.Store(0)
	c.metricReqs.Store(0)
	c.mu.Lock()
	clear(c.sums)
	clear(c.logTraces)
	c.mu.Unlock()
}

// Counts is what the collector has received so far.
type Counts struct {
	Logs, Spans               int64
	LogRequests, SpanRequests int64
	MetricRequests            int64
	Sums                      map[string]float64 // latest cumulative value by metric name
	LogTraceIDs               map[string]int     // log records per trace id they carry (first 1000 ids)
}

// Counts returns what the collector has received so far.
func (c *Collector) Counts() Counts {
	c.mu.Lock()
	sums := maps.Clone(c.sums)
	logTraces := maps.Clone(c.logTraces)
	c.mu.Unlock()
	return Counts{
		Logs: c.logs.Load(), Spans: c.spans.Load(),
		LogRequests: c.logReqs.Load(), SpanRequests: c.spanReqs.Load(), MetricRequests: c.metricReqs.Load(),
		Sums: sums, LogTraceIDs: logTraces,
	}
}

func (c *Collector) wait(ctx context.Context) {
	if c.Delay <= 0 {
		return
	}
	select {
	case <-time.After(c.Delay):
	case <-ctx.Done():
	}
}

// Export receives log records.
func (c *Collector) Export(ctx context.Context, req *collogs.ExportLogsServiceRequest) (*collogs.ExportLogsServiceResponse, error) {
	c.wait(ctx)
	var n int64
	c.mu.Lock()
	for _, rl := range req.GetResourceLogs() {
		for _, sl := range rl.GetScopeLogs() {
			n += int64(len(sl.GetLogRecords()))
			for _, r := range sl.GetLogRecords() {
				if id := hex.EncodeToString(r.GetTraceId()); id != "" && (c.logTraces[id] > 0 || len(c.logTraces) < 1000) {
					c.logTraces[id]++
				}
			}
		}
	}
	c.mu.Unlock()
	c.logs.Add(n)
	c.logReqs.Add(1)
	return &collogs.ExportLogsServiceResponse{}, nil
}

// traceServer and metricsServer adapt the collector to the two services
// whose Export method has the same name as the logs service's.
type traceServer struct{ *Collector }

func (t traceServer) Export(ctx context.Context, req *coltrace.ExportTraceServiceRequest) (*coltrace.ExportTraceServiceResponse, error) {
	t.wait(ctx)
	var n int64
	for _, rs := range req.GetResourceSpans() {
		for _, ss := range rs.GetScopeSpans() {
			n += int64(len(ss.GetSpans()))
		}
	}
	t.spans.Add(n)
	t.spanReqs.Add(1)
	return &coltrace.ExportTraceServiceResponse{}, nil
}

type metricsServer struct{ *Collector }

func (m metricsServer) Export(ctx context.Context, req *colmetrics.ExportMetricsServiceRequest) (*colmetrics.ExportMetricsServiceResponse, error) {
	m.wait(ctx)
	m.mu.Lock()
	for _, rm := range req.GetResourceMetrics() {
		for _, sm := range rm.GetScopeMetrics() {
			for _, met := range sm.GetMetrics() {
				sum := met.GetSum()
				if sum == nil {
					continue
				}
				var total float64
				for _, dp := range sum.GetDataPoints() {
					total += dp.GetAsDouble() + float64(dp.GetAsInt())
				}
				m.sums[met.GetName()] = total
			}
		}
	}
	m.mu.Unlock()
	m.metricReqs.Add(1)
	return &colmetrics.ExportMetricsServiceResponse{}, nil
}
