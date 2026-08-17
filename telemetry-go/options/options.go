package options

// Package options provides configuration options for the Telemetry service.
// It includes options for logging, metrics, tracing, and network settings.
// The options are structured in a way that allows for easy customization
// and extension, making it suitable for various deployment scenarios.
// The options are defined as structs, which can be easily serialized to JSON
// or other formats for configuration files.
type TelemetryOptions struct {
	Logging   LoggingOptions   `json:"logging"`   // Logging options for the service
	Foxglove  FoxgloveOptions  `json:"foxglove"`  // Foxglove options for the service
	Telemetry OpenTelemetryOptions `json:"telemetry"` // Unified telemetry options (OpenTelemetry-based)
	Profiling ProfilingOptions `json:"profiling"` // Continuous profiling options (Pyroscope)
	Tracing   TracingOptions   `json:"tracing"`   // Distributed tracing options
	// Add more options as needed
}

// ServiceOptions defines the identifying info for a running service.
// It includes the service name, description, version, and environment.
// The service name is used to identify the service in logs and metrics.
// The description provides additional context about the service.
// The version indicates the current version of the service.
type ServiceOptions struct {
	Name        string            `json:"name"`        // Service name
	Description string            `json:"description"` // Service description
	Version     string            `json:"version"`     // Service version
	Environment Environment       `json:"environment"` // Environment (e.g., "production", "development")
	Labels      map[string]string `json:"labels"`      // Global labels added to all telemetry (e.g., robot.id, device.id)
}

// Environment is a string type that represents the environment in which the service is running.
type Environment string

const (
	Development Environment = "development" // Development environment
	Staging     Environment = "staging"     // Staging environment
	Production  Environment = "production"  // Production environment
	Jetson      Environment = "jetson"      // Jetson environment
)

// NetworkOptions defines the network settings for the service.
// It includes options for the host and port on which the service listens.
// The host can be an IP address or a hostname, and the port is the TCP port.
type NetworkOptions struct {
	OpenTelemetry OTELOptions `json:"open_telemetry"` // OTEL settings (deprecated)
}

// FoxgloveOptions defines the settings for Foxglove integration.
type FoxgloveOptions struct {
	Enabled  bool   `json:"enabled"`   // Enable MCAP logging
	McapPath string `json:"file_path"` // Path to save MCAP files (e.g., "/var/logs/service.mcap")
}

// OTELOptions defines the settings for OpenTelemetry.
// It includes the host and port for the OpenTelemetry collector.
type OTELOptions struct {
	Host    string `json:"host"`    // e.g., "localhost" (deprecated)
	Port    int    `json:"port"`    // e.g., 4317 for gRPC, 4318 for HTTP (deprecated)
	Enabled bool   `json:"enabled"` // Enable OTEL export (deprecated)
}
