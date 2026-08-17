package logging

import (
	"encoding/json"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	"github.com/the-protobuf-project/telemetry/telemetry-go/options"
	otellog "go.opentelemetry.io/otel/log"
)

// formatPrefix formats a logger prefix from the service name, version, and environment.
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
// resolveLogLevel determines the effective log level for a service.
// It applies the environment default, then the global level, and finally the
// matching per-service module level, with later settings taking precedence.
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

// extractStructTagAttributes extracts OpenTelemetry attributes from exported fields
// tagged with telemetry:"attribute:<name>", including fields in nested structs. It
// extractStructTagAttributes extracts telemetry attributes from exported fields tagged with
// "attribute:" and recursively processes nested structs. It also adds the extraction time
// and the input struct's type name. Non-struct values produce no attributes.
func extractStructTagAttributes(rv reflect.Value) []otellog.KeyValue {
	if rv.Kind() != reflect.Struct {
		return nil
	}

	attrs := []otellog.KeyValue{}
	rt := rv.Type()

	for i := 0; i < rv.NumField(); i++ {
		field := rt.Field(i)
		fieldValue := rv.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Check for telemetry struct tag
		tag := field.Tag.Get("telemetry")
		if tag != "" {
			// Parse tag format: "attribute:key_name" or "attribute:session.id trace:trace.id"
			// Split by space and extract only items with "attribute:" prefix
			// Other prefixes (e.g., "trace:") are ignored by this function
			for _, tagPart := range strings.Fields(tag) {
				if attrName, found := strings.CutPrefix(tagPart, "attribute:"); found {
					// Convert field value to appropriate OTEL attribute
					attrs = append(attrs, convertToOtelKeyValue(attrName, fieldValue.Interface()))
				}
			}
		}

		// Recursively process nested structs
		if fieldValue.Kind() == reflect.Struct {
			attrs = append(attrs, extractStructTagAttributes(fieldValue)...)
		} else if fieldValue.Kind() == reflect.Pointer && !fieldValue.IsNil() && fieldValue.Elem().Kind() == reflect.Struct {
			attrs = append(attrs, extractStructTagAttributes(fieldValue.Elem())...)
		}
	}

	// Add dynamic/computed attributes
	// Example: Add a timestamp if not present
	attrs = append(attrs, otellog.Int64("extracted_at", time.Now().Unix()))

	// Example: Add struct type name
	attrs = append(attrs, otellog.String("struct_type", rt.Name()))

	return attrs
}

// dataToOtelAttributes converts various data types to OpenTelemetry KeyValue attributes
// dataToOtelAttributes converts a value into OpenTelemetry attributes, expanding map entries and preserving complex values under the "data" attribute. Struct values also contribute attributes from `telemetry:"attribute:<name>"` tags. 
// dataToOtelAttributes converts a value into OpenTelemetry attributes.
// Struct attributes tagged with telemetry are included; maps are expanded into individual attributes and retained as serialized data. Nil values produce no attributes.
func dataToOtelAttributes(v any) []otellog.KeyValue {
	if v == nil {
		return nil
	}

	rv := reflect.ValueOf(v)

	// Handle pointers
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
		v = rv.Interface()
	}

	attrs := []otellog.KeyValue{}

	// Extract struct tag attributes if it's a struct
	if rv.Kind() == reflect.Struct {
		attrs = append(attrs, extractStructTagAttributes(rv)...)
	}

	// For all types, convert to JSON string and send as "data" attribute
	switch rv.Kind() {
	case reflect.Map, reflect.Struct, reflect.Slice, reflect.Array:
		// For maps, convert to map[string]interface{} first to handle interface{} keys properly
		if rv.Kind() == reflect.Map {
			convertedMap := convertToMap(v)
			// Expand each map entry as its own OTLP attribute so Loki can index/query them individually
			for k, val := range convertedMap {
				attrs = append(attrs, convertToOtelKeyValue(k, val))
			}
			// Also keep full map as a "data" JSON blob for reference
			if b, err := json.Marshal(convertedMap); err == nil {
				attrs = append(attrs, otellog.String("data", string(b)))
			} else {
				// Fallback to string representation if marshal fails
				attrs = append(attrs, otellog.String("data", fmt.Sprintf("%+v", v)))
			}
		} else {
			// For structs, slices, arrays, marshal directly
			if b, err := json.Marshal(v); err == nil {
				attrs = append(attrs, otellog.String("data", string(b)))
			} else {
				// Fallback to string representation if marshal fails
				attrs = append(attrs, otellog.String("data", fmt.Sprintf("%+v", v)))
			}
		}

	default:
		// For primitive types, use convertToOtelKeyValue
		attrs = append(attrs, convertToOtelKeyValue("data", v))
	}

	return attrs
}

// convertToOtelKeyValue converts a key-value pair to an OpenTelemetry KeyValue
func convertToOtelKeyValue(key string, value any) otellog.KeyValue {
	if value == nil {
		return otellog.String(key, "<nil>")
	}

	rv := reflect.ValueOf(value)

	// Handle pointers
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return otellog.String(key, "<nil>")
		}
		rv = rv.Elem()
		value = rv.Interface()
	}

	switch rv.Kind() {
	case reflect.String:
		return otellog.String(key, rv.String())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return otellog.Int64(key, rv.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return otellog.Int64(key, int64(rv.Uint()))
	case reflect.Float32, reflect.Float64:
		return otellog.Float64(key, rv.Float())
	case reflect.Bool:
		return otellog.Bool(key, rv.Bool())
	case reflect.Slice, reflect.Array:
		// Check if it's a byte slice
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			return otellog.Bytes(key, value.([]byte))
		}
		// For other slices, convert to JSON string
		if b, err := json.Marshal(value); err == nil {
			return otellog.String(key, string(b))
		}
		return otellog.String(key, fmt.Sprintf("%+v", value))
	case reflect.Map, reflect.Struct:
		// Convert complex types to JSON string
		if b, err := json.Marshal(value); err == nil {
			return otellog.String(key, string(b))
		}
		return otellog.String(key, fmt.Sprintf("%+v", value))
	default:
		return otellog.String(key, fmt.Sprintf("%+v", value))
	}
}

// formattedData attempts to marshal structs, maps, or slices into
// pretty-printed JSON for console output. Fallbacks to fmt-compatible output for others.
func formattedData(v any) any {
	if v == nil {
		return "<nil>"
	}

	rv := reflect.ValueOf(v)

	// Handle pointers
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return "<nil>"
		}
		rv = rv.Elem()
		v = rv.Interface()
	}

	// Check if the type is a struct or map, which need to be marshaled.
	switch rv.Kind() {
	case reflect.Struct, reflect.Map:
		// For maps, convert to map[string]interface{} first to handle interface{} keys properly
		if rv.Kind() == reflect.Map {
			convertedMap := convertToMap(v)
			if b, err := json.MarshalIndent(convertedMap, "", "  "); err == nil {
				return string(b)
			}
			// Fallback to default formatting if marshal fails
			return fmt.Sprintf("%+v", v)
		} else {
			// For structs, marshal directly
			if b, err := json.MarshalIndent(v, "", "  "); err == nil {
				return string(b)
			}
			// Fallback to default formatting if marshal fails
			return fmt.Sprintf("%+v", v)
		}

	case reflect.Slice, reflect.Array:
		// Check if it's a byte slice
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			return string(v.([]byte))
		}
		// Marshal other slices/arrays into JSON
		if b, err := json.MarshalIndent(v, "", "  "); err == nil {
			return string(b)
		}
		return fmt.Sprintf("%+v", v)

	case reflect.String:
		return v.(string)

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int()

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return rv.Uint()

	case reflect.Float32, reflect.Float64:
		return rv.Float()

	case reflect.Bool:
		return rv.Bool()

	default:
		return fmt.Sprintf("%+v", v)
	}
}

// convertToMap converts any value to a map[string]interface{} for MCAP logging
func convertToMap(v any) map[string]interface{} {
	if v == nil {
		return nil
	}

	rv := reflect.ValueOf(v)

	// Handle pointers
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
		v = rv.Interface()
	}

	// If already a map, try to convert it
	if rv.Kind() == reflect.Map {
		result := make(map[string]interface{})
		for _, key := range rv.MapKeys() {
			keyStr := convertKeyToString(key.Interface())
			result[keyStr] = rv.MapIndex(key).Interface()
		}
		return result
	}

	// For structs, marshal to JSON and unmarshal to map
	if rv.Kind() == reflect.Struct {
		data, err := json.Marshal(v)
		if err == nil {
			var result map[string]interface{}
			if err := json.Unmarshal(data, &result); err == nil {
				return result
			}
		}
	}

	// For other types, create a simple map with the value
	return map[string]interface{}{
		"value": v,
	}
}

// convertKeyToString converts interface{} keys to consistent string representations
// This ensures proper key-value mapping in logs sent to Grafana
func convertKeyToString(key interface{}) string {
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

// getCallerInfo returns the file and line number of the caller
func getCallerInfo(skip int) (string, int) {
	_, file, line, ok := runtime.Caller(skip)
	if !ok {
		return "unknown", 0
	}

	// Extract just the filename from the full path
	parts := strings.Split(file, "/")
	if len(parts) > 0 {
		file = parts[len(parts)-1]
	}

	return file, line
}
