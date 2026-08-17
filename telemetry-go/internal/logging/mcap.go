package logging

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/the-protobuf-project/telemetry/telemetry-go/internal/foxglove"
	"github.com/the-protobuf-project/telemetry/telemetry-go/options"
)

// LogMcapWriter wraps the unified MCAP writer with Foxglove Log schema support
type LogMcapWriter struct {
	unifiedWriter      *foxglove.UnifiedMcapWriter
	channelID          uint16
	serviceName        string
	serviceVersion     string
	serviceEnvironment string
}

// FoxgloveLog represents a log message following the Foxglove Log schema
// https://github.com/foxglove/schemas/blob/main/schemas/jsonschema/Log.json
// Extended with service_version and service_environment fields
type FoxgloveLog struct {
	Timestamp          FoxgloveTimestamp      `json:"timestamp"`
	Level              int32                  `json:"level"`               // 0=UNKNOWN, 1=DEBUG, 2=INFO, 3=WARNING, 4=ERROR, 5=FATAL
	Message            string                 `json:"message"`             // Log message
	Name               string                 `json:"name"`                // Process or node name (service name with version and env)
	File               string                 `json:"file"`                // Filename
	Line               uint32                 `json:"line"`                // Line number
	Data               map[string]interface{} `json:"data,omitempty"`      // Additional structured data
	ServiceVersion     string                 `json:"service_version"`     // Service version (e.g., "1.0.0")
	ServiceEnvironment string                 `json:"service_environment"` // Service environment (e.g., "development", "production")
}

// FoxgloveTimestamp represents a timestamp in Foxglove format
type FoxgloveTimestamp struct {
	Sec  uint32 `json:"sec"`  // Seconds since epoch
	Nsec uint32 `json:"nsec"` // Nanoseconds (0-999999999)
}

// LogLevel constants matching Foxglove Log schema
const (
	LogLevelUnknown = 0 // Unknown log level
	LogLevelDebug   = 1 // Debug log level
	LogLevelInfo    = 2 // Info log level
	LogLevelWarning = 3 // Warning log level
	LogLevelError   = 4 // Error log level
	LogLevelFatal   = 5 // Fatal log level
)

// NewLogMcapWriter creates a new log writer using the unified MCAP writer
func NewLogMcapWriter(serviceOpts options.ServiceOptions, unifiedWriter *foxglove.UnifiedMcapWriter) (*LogMcapWriter, error) {
	// Format service name with version and environment
	serviceName := fmt.Sprintf("%s (%s | %s)", serviceOpts.Name, serviceOpts.Version, serviceOpts.Environment)

	// Topic for logs
	topic := fmt.Sprintf("/logs/%s", serviceOpts.Name)

	// Channel metadata
	metadata := map[string]string{
		"service":     serviceOpts.Name,
		"version":     serviceOpts.Version,
		"environment": string(serviceOpts.Environment),
		"description": serviceOpts.Description,
	}

	// Create log channel in unified writer
	channelID, err := unifiedWriter.CreateLogChannel(topic, metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to create log channel: %w", err)
	}

	return &LogMcapWriter{
		unifiedWriter:      unifiedWriter,
		channelID:          channelID,
		serviceName:        serviceName,
		serviceVersion:     serviceOpts.Version,
		serviceEnvironment: string(serviceOpts.Environment),
	}, nil
}

// WriteLog writes a log message using the Foxglove Log schema
func (l *LogMcapWriter) WriteLog(level, message, file string, line uint32, data map[string]interface{}) error {
	now := time.Now()

	// Convert string level to Foxglove level integer
	levelInt := stringToFoxgloveLevel(level)

	// Create Foxglove Log message
	logMsg := FoxgloveLog{
		Timestamp: FoxgloveTimestamp{
			Sec:  uint32(now.Unix()),
			Nsec: uint32(now.Nanosecond()),
		},
		Level:              levelInt,
		Message:            message,
		Name:               l.serviceName,
		File:               file,
		Line:               line,
		Data:               data,
		ServiceVersion:     l.serviceVersion,
		ServiceEnvironment: l.serviceEnvironment,
	}

	// Serialize to JSON
	msgData, err := json.Marshal(logMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal log message: %w", err)
	}

	nowNano := uint64(now.UnixNano())
	return l.unifiedWriter.WriteMessage(l.channelID, msgData, nowNano, nowNano)
}

// Close is a no-op since the unified writer is managed at the Telemetry level
func (l *LogMcapWriter) Close() error {
	return nil
}

// IsClosed returns whether the writer is closed
func (l *LogMcapWriter) IsClosed() bool {
	return l.unifiedWriter.IsClosed()
}

// GetFilePath returns the path to the MCAP file
func (l *LogMcapWriter) GetFilePath() string {
	return l.unifiedWriter.GetFilePath()
}

// stringToFoxgloveLevel converts string log level to Foxglove integer level
func stringToFoxgloveLevel(level string) int32 {
	switch level {
	case "DEBUG", "debug", "Debug":
		return LogLevelDebug
	case "INFO", "info", "Info":
		return LogLevelInfo
	case "WARN", "warn", "Warn", "WARNING", "warning", "Warning":
		return LogLevelWarning
	case "ERROR", "error", "Error":
		return LogLevelError
	case "FATAL", "fatal", "Fatal":
		return LogLevelFatal
	default:
		return LogLevelUnknown
	}
}
