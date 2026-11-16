package logging

import (
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/charmbracelet/log"
	"github.com/the-protobuf-project/telemetry/telemetry-go/options"
)

// formatPrefix formats the prefix for the logger.
func formatPrefix(serviceOpts options.ServiceOptions) string {
	return fmt.Sprintf("%s (%s | %s)", serviceOpts.Name, serviceOpts.Version, serviceOpts.Environment)
}

// resolveTimeFormat returns the Go time layout selected by the logging options, defaulting to RFC3339 when no supported format is configured.
func resolveTimeFormat(opts options.LoggingOptions) string {
	switch opts.Log.TimeFormatKey {
	case options.TimeFormatRFC3339:
		return time.RFC3339
	case options.TimeFormatRFC3339Nano:
		return time.RFC3339Nano
	case options.TimeFormatKitchen:
		return time.Kitchen
	case options.TimeFormatStamp:
		return "Jan _2 15:04:05"
	case options.TimeFormatCustom:
		if opts.Log.CustomFormat != "" {
			return opts.Log.CustomFormat
		}
		return time.RFC3339
	default:
		return time.RFC3339
	}
}

// logLevelToCharm converts a telemetry LogLevel to a charmbracelet log.Level.
func logLevelToCharm(level options.LogLevel) log.Level {
	switch level {
	case options.ModuleLevel_1:
		return log.ErrorLevel
	case options.ModuleLevel_2:
		return log.InfoLevel
	case options.ModuleLevel_3:
		return log.DebugLevel
	default:
		return log.InfoLevel
	}
}

// resolveLogLevelFromEnv sets default log level based on environment.
func resolveLogLevelFromEnv(env options.Environment) log.Level {
	switch env {
	case options.Production:
		return log.InfoLevel
	case options.Staging:
		return log.WarnLevel
	case options.Jetson, options.Development:
		return log.DebugLevel
	default:
		return log.InfoLevel
	}
}

// resolveLogLevel determines the effective log level using the priority chain:
//
//	per-module override (from config) > global logging.level > environment-based default
//
// The serviceName is used to look up per-module overrides in logging.modules.<name>.
func resolveLogLevel(serviceName string, opts options.LoggingOptions, env options.Environment) log.Level {
	// 1. Start with environment-based default (lowest priority)
	level := resolveLogLevelFromEnv(env)

	// 2. If global logging.level is set, override
	if opts.Level != options.ModuleLevel_Unset {
		level = logLevelToCharm(opts.Level)
	}

	// 3. If per-module override exists for this service, it wins (highest priority)
	if opts.Modules != nil {
		if moduleOpts, ok := opts.Modules[serviceName]; ok && moduleOpts.Level != options.ModuleLevel_Unset {
			level = logLevelToCharm(moduleOpts.Level)
		}
	}

	return level
}

// resolveCallerOffset returns the configured positive caller offset or the default offset of 2.
func resolveCallerOffset(opts options.LoggingOptions) int {
	if opts.Log.CallerOffset > 0 {
		return opts.Log.CallerOffset
	}
	// Default offset to skip internal logging wrapper functions
	return 2
}

// convertKeyToString converts interface{} keys to consistent string representations
// This ensures proper key-value mapping in logs sent to Grafana
func convertKeyToString(key any) string {
	if key == nil {
		return "<nil>"
	}

	rv := reflect.ValueOf(key)

	switch rv.Kind() {
	case reflect.String:
		return rv.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return fmt.Sprintf("%d", rv.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return fmt.Sprintf("%d", rv.Uint())
	case reflect.Float32, reflect.Float64:
		return fmt.Sprintf("%f", rv.Float())
	case reflect.Bool:
		return fmt.Sprintf("%t", rv.Bool())
	default:
		// For other types, try JSON first, then fallback to fmt.Sprintf
		if b, err := json.Marshal(key); err == nil {
			return string(b)
		}
		return fmt.Sprintf("%v", key)
	}
}
