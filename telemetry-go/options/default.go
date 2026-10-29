package options

import (
	"os"
	"strconv"
	"strings"
)

// Default constructs telemetry options with profiling, logging, Foxglove, and OpenTelemetry defaults.
// Environment variables override the profiling and Foxglove settings where supported.
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

// DefaultTelemetry returns the default OpenTelemetry options: logging,
// metrics and tracing on, and nothing exported anywhere.
//
// OTLP export is off unless something asks for it: an endpoint (a config
// file's telemetry.otlp.endpoint, OTEL_EXPORTER_OTLP_HOST, or WithOTLP), or
// OTEL_EXPORTER_OTLP_ENABLED=true, which on its own exports to the OTLP
// default, localhost:4317. OTEL_EXPORTER_OTLP_ENABLED=false and
// OTEL_SDK_DISABLED=true turn it off whatever else is configured; see
// [OTLPDisabledByEnv].
//
// The Prometheus exporter is off unless OTEL_METRICS_EXPORTER names
// "prometheus" (or telemetry.metrics.prometheus, or WithPrometheus, turns it on).
func DefaultTelemetry() OpenTelemetryOptions {
	return OpenTelemetryOptions{
		Logging: LoggingTelemetryOptions{
			Enabled: true,
		},
		Metrics: MetricsTelemetryOptions{
			Enabled:               true,
			ExportIntervalSeconds: 10,
			Prometheus:            envListHas("OTEL_METRICS_EXPORTER", "prometheus"),
		},
		Tracing: TracingTelemetryOptions{
			Enabled: true,
		},
		OTLP: OTLPOptions{
			Host:    getFromEnvOrDefault("OTEL_EXPORTER_OTLP_HOST", ""),
			Port:    getIntFromEnvOrDefault("OTEL_EXPORTER_OTLP_PORT", 4317),
			Enabled: getBoolFromEnvOrDefault("OTEL_EXPORTER_OTLP_ENABLED", false),
		},
	}
}

// OTLPDisabledByEnv reports whether the environment forbids OTLP export:
// OTEL_EXPORTER_OTLP_ENABLED set to false, or OTEL_SDK_DISABLED set to true.
// Either wins over a config file, a TELEMETRY_ variable and the builder, so
// an operator (or a test run) can always stop a process exporting.
func OTLPDisabledByEnv() bool {
	return SDKDisabledByEnv() || !getBoolFromEnvOrDefault("OTEL_EXPORTER_OTLP_ENABLED", true)
}

// SDKDisabledByEnv reports whether OTEL_SDK_DISABLED is true: no exporter of
// any kind — OTLP or Prometheus — is set up, and the OpenTelemetry providers
// are no-ops. Console logging is unaffected.
func SDKDisabledByEnv() bool {
	return getBoolFromEnvOrDefault("OTEL_SDK_DISABLED", false)
}

// envListHas reports whether the comma-separated environment variable key
// lists want, ignoring case and spaces.
func envListHas(key, want string) bool {
	for _, v := range strings.Split(os.Getenv(key), ",") {
		if strings.EqualFold(strings.TrimSpace(v), want) {
			return true
		}
	}
	return false
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
