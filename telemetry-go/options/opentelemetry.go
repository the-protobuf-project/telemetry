package options

// OpenTelemetryOptions defines the configuration for the unified telemetry service
// that integrates OpenTelemetry for logging, metrics, and tracing.
type OpenTelemetryOptions struct {
	Enabled bool                    `json:"enabled"` // Enable all telemetry (logging, metrics, tracing)
	Logging LoggingTelemetryOptions `json:"logging"` // Logging telemetry options
	Metrics MetricsTelemetryOptions `json:"metrics"` // Metrics telemetry options
	Tracing TracingTelemetryOptions `json:"tracing"` // Tracing telemetry options
	OTLP    OTLPOptions             `json:"otlp"`    // OTLP exporter options
}

// LoggingTelemetryOptions defines the configuration for OpenTelemetry logging
type LoggingTelemetryOptions struct {
	Enabled bool `json:"enabled"` // Enable logging (inherits from telemetry.enabled if not set)
}

// MetricsTelemetryOptions defines the configuration for OpenTelemetry metrics
type MetricsTelemetryOptions struct {
	Enabled               bool `json:"enabled"`                 // Enable metrics (inherits from telemetry.enabled if not set)
	ExportIntervalSeconds int  `json:"export_interval_seconds"` // Export interval in seconds
}

// TracingTelemetryOptions defines the configuration for OpenTelemetry tracing
type TracingTelemetryOptions struct {
	Enabled bool `json:"enabled"` // Enable tracing (inherits from telemetry.enabled if not set)
}

// OTLPOptions defines the settings for OTLP exporter
// Host is auto-detected: if it's a domain (not localhost/IP), secure is enabled automatically
// Port defaults to 4317 (gRPC) unless use_http is true, then 4318
type OTLPOptions struct {
	Endpoint  string            `json:"endpoint"`   // OTLP endpoint (e.g., "otel.example.com" or "localhost:4317")
	AuthToken string            `json:"auth_token"` // Bearer token for authentication (simpler than headers)
	Enabled   bool              `json:"enabled"`    // Enable OTLP export (if false, uses stdout)
	Secure    bool              `json:"secure"`     // Use TLS (auto-detected from endpoint if not set)
	UseHTTP   bool              `json:"use_http"`   // Use HTTP instead of gRPC (default: false = gRPC)
	Headers   map[string]string `json:"headers"`    // Custom headers (use auth_token for simple auth)
	// Deprecated: Use Endpoint instead
	Host string `json:"host"` // OTLP collector host (deprecated, use endpoint)
	Port int    `json:"port"` // OTLP collector port (deprecated, auto-detected)
}
