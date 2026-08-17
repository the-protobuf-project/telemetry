package options

import (
	"os"
	"strconv"
)

// Default constructs telemetry options with profiling, logging, Foxglove, and OpenTelemetry defaults.
// Default constructs telemetry options with profiling, logging, Foxglove MCAP, and OpenTelemetry defaults.
// Profiling and Foxglove MCAP settings can be overridden through their respective environment variables.
func Default() TelemetryOptions {
	return TelemetryOptions{
		Profiling: ProfilingOptions{
			Enabled:              getBoolFromEnvOrDefault("TELEMETRY_PROFILING_ENABLED", false),
			ServerAddress:        getFromEnvOrDefault("TELEMETRY_PROFILING_SERVER", "http://localhost:4040"),
			BasicAuthUser:        getFromEnvOrDefault("TELEMETRY_PROFILING_USER", ""),
			BasicAuthPassword:    getFromEnvOrDefault("TELEMETRY_PROFILING_PASSWORD", ""),
			TenantID:             getFromEnvOrDefault("TELEMETRY_PROFILING_TENANT_ID", ""),
			ProfileCPU:           true,
			ProfileAllocObjects:  true,
			ProfileAllocSpace:    true,
			ProfileInuseObjects:  true,
			ProfileInuseSpace:    true,
			ProfileGoroutines:    false,
			ProfileMutexCount:    false,
			ProfileMutexDuration: false,
			ProfileBlockCount:    false,
			ProfileBlockDuration: false,
			MutexProfileRate:     5,
			BlockProfileRate:     5,
			Tags:                 map[string]string{},
		},
		Logging: LoggingOptions{
			Log: LogOptions{
				ReportCaller:    true,
				ReportTimestamp: true,
				CallerOffset:    3,
			},
		},
		Foxglove: FoxgloveOptions{
			Enabled:  getBoolFromEnvOrDefault("FOXGLOVE_MCAP_ENABLED", false),
			McapPath: getFromEnvOrDefault("FOXGLOVE_MCAP_PATH", ""),
		},
		Telemetry: DefaultTelemetry(),
	}
}

// DefaultTelemetry returns default telemetry options with all features enabled
// DefaultTelemetry provides enabled logging, metrics, and tracing with local OTLP export defaults.
// DefaultTelemetry configures enabled logging, metrics, and tracing with metrics exported every 10 seconds.
// It configures the OTLP host, port, and export enablement from environment variables, defaulting to localhost:4317 with export disabled.
func DefaultTelemetry() OpenTelemetryOptions {
	return OpenTelemetryOptions{
		Logging: LoggingTelemetryOptions{
			Enabled: true,
		},
		Metrics: MetricsTelemetryOptions{
			Enabled:               true,
			ExportIntervalSeconds: 10,
		},
		Tracing: TracingTelemetryOptions{
			Enabled: true,
		},
		OTLP: OTLPOptions{
			Host:    getFromEnvOrDefault("OTEL_EXPORTER_OTLP_HOST", "localhost"),
			Port:    getIntFromEnvOrDefault("OTEL_EXPORTER_OTLP_PORT", 4317),
			Enabled: getBoolFromEnvOrDefault("OTEL_EXPORTER_OTLP_ENABLED", false),
		},
	}
}

// getFromEnvOrDefault returns the value of the environment variable with the given key,
// or the default value if the environment variable is not set
func getFromEnvOrDefault(key string, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

// getIntFromEnvOrDefault returns the value of the environment variable with the given key,
// or the default value if the environment variable is not set
func getIntFromEnvOrDefault(key string, defaultValue int) int {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	intValue, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue
	}
	return intValue
}

// getBoolFromEnvOrDefault returns the value of the environment variable with the given key,
// or the default value if the environment variable is not set
func getBoolFromEnvOrDefault(key string, defaultValue bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	boolValue, err := strconv.ParseBool(value)
	if err != nil {
		return defaultValue
	}
	return boolValue
}
