package options

// TracingOptions defines the options for distributed tracing.
type TracingOptions struct {
	Enabled bool `json:"enabled"` // Enable distributed tracing
	// AutoInstrument makes http.DefaultClient carry the trace on every
	// request (see Telemetry.HTTPHandler and friends for servers, other
	// clients and gRPC). On by default.
	AutoInstrument bool `json:"auto_instrument"`
}
