package options

// Removed self-import

type TimeFormat string

const (
	TimeFormatRFC3339     TimeFormat = "RFC3339"
	TimeFormatRFC3339Nano TimeFormat = "RFC3339Nano"
	TimeFormatKitchen     TimeFormat = "Kitchen"
	TimeFormatStamp       TimeFormat = "Stamp"
	TimeFormatCustom      TimeFormat = "Custom"
)

// LogLevel represents the verbosity level for a service or module.
// Higher levels produce more verbose output.
//
//   - Level1 (Error)  — stable, production-ready module; minimal noise
//   - Level2 (Info)   — normal operation; standard telemetry
//   - Level3 (Debug)  — active development; full observability
type LogLevel int

const (
	ModuleLevel_Unset LogLevel = 0 // No explicit level set; fall back to environment-based default
	ModuleLevel_1     LogLevel = 1 // Error only — stable module
	ModuleLevel_2     LogLevel = 2 // Info — normal operation
	ModuleLevel_3     LogLevel = 3 // Debug — full observability
)

// ModuleOptions defines per-module logging overrides.
// When set in config, these take highest priority (after env vars).
type ModuleOptions struct {
	Level LogLevel `json:"level"` // Log level override for this module
}

type LogOptions struct {
	Prefix          string     `json:"prefix"`           // Prefix string in log output
	ReportCaller    bool       `json:"report_caller"`    // Include caller info in logs
	ReportTimestamp bool       `json:"report_timestamp"` // Include timestamp in logs
	TimeFormatKey   TimeFormat `json:"time_format_key"`  // Named time format enum
	CustomFormat    string     `json:"custom_format"`    // Custom format if TimeFormatKey == TimeFormatCustom
	CallerOffset    int        `json:"caller_offset"`    // Adjust call depth for correct file/line display
}

type LoggingOptions struct {
	Log     LogOptions               `json:"log"`     // Log options
	Level   LogLevel                 `json:"level"`   // Global log level (overrides environment-based default)
	Modules map[string]ModuleOptions `json:"modules"` // Per-module log level overrides
}
