package telemetry

// Trace propagation over net/http and gRPC: every request a service makes
// carries the W3C trace (traceparent, tracestate, baggage) and every request
// it answers continues the caller's, so a trace runs end to end across
// services and languages.
//
// Go cannot patch a package at run time, so what the SDK can reach on its
// own it does: Build wraps http.DefaultClient, which http.Get, http.Post and
// any library using the default client go through. Servers, other clients
// and gRPC take one call each: HTTPHandler, HTTPTransport, GRPCServerOptions
// and GRPCDialOptions. All of them use the global tracer, meter and
// propagator Build installs, so with tracing off they still pass a caller's
// trace on.

import (
	"net/http"
	"sync"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"google.golang.org/grpc"
)

// defaultClient remembers which Telemetry wrapped http.DefaultClient, so
// only that one unwraps it, and only if nothing replaced it since.
var defaultClient struct {
	sync.Mutex
	owner   *Telemetry
	wrapped http.RoundTripper
	before  http.RoundTripper
}

// instrumentDefaultClient makes http.DefaultClient carry the trace. The
// first Telemetry built does it; http.DefaultTransport is left as it is,
// since code that asserts it is an *http.Transport would break.
func (p *Telemetry) instrumentDefaultClient() {
	defaultClient.Lock()
	defer defaultClient.Unlock()
	if defaultClient.owner != nil {
		return
	}
	before := http.DefaultClient.Transport
	base := before
	if base == nil {
		base = http.DefaultTransport
	}
	defaultClient.owner = p
	defaultClient.before = before
	defaultClient.wrapped = otelhttp.NewTransport(base)
	http.DefaultClient.Transport = defaultClient.wrapped
}

func (p *Telemetry) uninstrumentDefaultClient() {
	defaultClient.Lock()
	defer defaultClient.Unlock()
	if defaultClient.owner != p {
		return
	}
	if http.DefaultClient.Transport == defaultClient.wrapped {
		http.DefaultClient.Transport = defaultClient.before
	}
	defaultClient.owner, defaultClient.wrapped, defaultClient.before = nil, nil, nil
}

// HTTPHandler wraps a server handler so each request runs in a SERVER span
// that continues the caller's trace, named after the operation, and is
// measured (http.server.request.duration). Wrap the whole mux to cover every
// route:
//
//	http.ListenAndServe(":8080", tel.HTTPHandler("orders", mux))
func (p *Telemetry) HTTPHandler(operation string, h http.Handler) http.Handler {
	return otelhttp.NewHandler(h, operation)
}

// HTTPTransport wraps a client transport so each request is a CLIENT span
// and carries the trace, for a client other than http.DefaultClient. A nil
// base means http.DefaultTransport.
//
//	client := &http.Client{Transport: tel.HTTPTransport(nil), Timeout: 5 * time.Second}
func (p *Telemetry) HTTPTransport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return otelhttp.NewTransport(base)
}

// GRPCServerOptions traces a gRPC server: each call runs in a SERVER span
// that continues the caller's trace from its metadata.
//
//	srv := grpc.NewServer(tel.GRPCServerOptions()...)
func (p *Telemetry) GRPCServerOptions() []grpc.ServerOption {
	return []grpc.ServerOption{grpc.StatsHandler(otelgrpc.NewServerHandler())}
}

// GRPCDialOptions traces a gRPC client: each call is a CLIENT span and
// carries the trace in its metadata.
//
//	conn, err := grpc.NewClient(addr, append(tel.GRPCDialOptions(), creds)...)
func (p *Telemetry) GRPCDialOptions() []grpc.DialOption {
	return []grpc.DialOption{grpc.WithStatsHandler(otelgrpc.NewClientHandler())}
}
