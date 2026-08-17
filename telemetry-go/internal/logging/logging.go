package logging

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/charmbracelet/log"
	"github.com/the-protobuf-project/telemetry/telemetry-go/internal/foxglove"
	"github.com/the-protobuf-project/telemetry/telemetry-go/options"

	otellog "go.opentelemetry.io/otel/log"
)

// Logger is the main logging client for the telemetry framework.
// It wraps Charmbracelet's log.Logger and provides structured logging,
// format-based logging, and hooks for MCAP/OTEL integration.
// It also forwards logs to OpenTelemetry for OTLP/Loki integration
// and optionally writes to MCAP files for Foxglove visualization.
type Logger struct {
	loggerService      *log.Logger
	otelLogger         *OtelLogger
	mcapWriter         *LogMcapWriter
	ctx                context.Context
	serviceName        string
	serviceVersion     string
	serviceEnvironment string
}

// NewLogger initializes a new structured logger instance based on
// the provided service and logging options.
// If otelLogger is provided, logs will be forwarded to OTLP/Loki.
// If unifiedWriter is provided, logs will be written to MCAP files.
func NewLogger(serviceOpts options.ServiceOptions, opts options.LoggingOptions, unifiedWriter *foxglove.UnifiedMcapWriter, otelLogger otellog.Logger) *Logger {
	loggerService := log.NewWithOptions(os.Stderr, log.Options{
		Prefix:          formatPrefix(serviceOpts),
		Level:           resolveLogLevel(serviceOpts.Name, opts, serviceOpts.Environment),
		ReportCaller:    true, // Always show file:line
		ReportTimestamp: true, // Always show timestamp
		TimeFormat:      resolveTimeFormat(opts),
		CallerOffset:    resolveCallerOffset(opts),
	})

	logger := &Logger{
		loggerService:      loggerService,
		ctx:                context.Background(),
		serviceName:        serviceOpts.Name,
		serviceVersion:     serviceOpts.Version,
		serviceEnvironment: string(serviceOpts.Environment),
	}

	// If OTLP logger is provided, set it up for forwarding
	if otelLogger != nil {
		logger.otelLogger = NewOtelLogger(otelLogger)
	}

	// If unified MCAP writer is provided, create log channel
	if unifiedWriter != nil {
		mcapWriter, err := NewLogMcapWriter(serviceOpts, unifiedWriter)
		if err != nil {
			loggerService.Errorf("Failed to initialize MCAP log writer: %v", err)
		} else {
			logger.mcapWriter = mcapWriter
			loggerService.Infof("MCAP logging enabled, writing to: %s", unifiedWriter.GetFilePath())
		}
	}

	return logger
}

// WithContext returns a new Logger with the specified context for OTLP logging
func (l *Logger) WithContext(ctx context.Context) *Logger {
	return &Logger{
		loggerService:      l.loggerService,
		otelLogger:         l.otelLogger,
		mcapWriter:         l.mcapWriter,
		ctx:                ctx,
		serviceName:        l.serviceName,
		serviceVersion:     l.serviceVersion,
		serviceEnvironment: l.serviceEnvironment,
	}
}

// Info logs an info-level message with optional structured data.
func (l *Logger) Info(msg string, data ...any) {
	l.log(log.InfoLevel, msg, data...)
}

// Debug logs a debug-level message with optional structured data.
func (l *Logger) Debug(msg string, data ...any) {
	l.log(log.DebugLevel, msg, data...)
}

// Warn logs a warning-level message with optional structured data.
func (l *Logger) Warn(msg string, data ...any) {
	l.log(log.WarnLevel, msg, data...)
}

// Error logs an error-level message with optional structured data.
func (l *Logger) Error(msg string, data ...any) error {
	l.log(log.ErrorLevel, msg, data...)
	return fmt.Errorf("%s", msg)
}

// Fatal logs a fatal-level message with optional structured data and exits the program.
func (l *Logger) Fatal(msg string, data ...any) {
	l.log(log.FatalLevel, msg, data...)
	os.Exit(1)
}

// Infof logs an info-level message using a format string.
func (l *Logger) Infof(format string, args ...any) {
	l.loggerService.Infof(format, args...)
	if l.otelLogger != nil {
		l.otelLogger.WithContext(l.ctx).Infof(format, args...)
	}
}

// Debugf logs a debug-level message using a format string.
func (l *Logger) Debugf(format string, args ...any) {
	l.loggerService.Debugf(format, args...)
	if l.otelLogger != nil {
		l.otelLogger.WithContext(l.ctx).Debugf(format, args...)
	}
}

// Warnf logs a warning-level message using a format string.
func (l *Logger) Warnf(format string, args ...any) {
	l.loggerService.Warnf(format, args...)
	if l.otelLogger != nil {
		l.otelLogger.WithContext(l.ctx).Warnf(format, args...)
	}
}

// Errorf logs an error-level message using a format string.
func (l *Logger) Errorf(format string, args ...interface{}) error {
	l.loggerService.Errorf(format, args...)
	if l.otelLogger != nil {
		l.otelLogger.WithContext(l.ctx).Errorf(format, args...)
	}
	return fmt.Errorf(format, args...)
}

// Fatalf logs a fatal-level message using a format string and exits the program.
func (l *Logger) Fatalf(format string, args ...any) {
	l.loggerService.Fatalf(format, args...)
	if l.otelLogger != nil {
		l.otelLogger.WithContext(l.ctx).Fatalf(format, args...)
	}
	os.Exit(1)
}

// log is the internal handler for all log levels, with optional structured data.
func (l *Logger) log(level log.Level, msg string, data ...any) {
	// Log to stdout via charmbracelet logger
	if len(data) == 0 {
		l.loggerService.Log(level, msg)
	} else {
		sub := l.loggerService.With("data", formattedData(data[0]))
		sub.Log(level, msg)
	}

	// Forward to OTLP logger if available
	if l.otelLogger != nil {
		otelLogger := l.otelLogger.WithContext(l.ctx)

		// Get caller information for file and line
		file, line := getCallerInfo(3) // Skip 3 frames: getCallerInfo, log, and the calling function

		// Build attributes with service metadata and caller info
		attrs := []otellog.KeyValue{
			otellog.String("service.name", l.serviceName),
			otellog.String("service.version", l.serviceVersion),
			otellog.String("service.environment", l.serviceEnvironment),
			otellog.String("code.filepath", file),
			otellog.Int("code.lineno", line),
		}

		// Convert user data to OTLP attributes if present
		// Iterate through all data items
		for _, d := range data {
			attrs = append(attrs, dataToOtelAttributes(d)...)
		}

		// Build the log body: message first, then data as a JSON object on the same line.
		// Format: "User joined room | {"user_id":"user-alice","room_id":"room-ai-chat"}"
		// This keeps the message visible in the Grafana log list while showing structured data inline.
		body := msg
		if len(data) > 0 {
			if b, err := json.Marshal(convertToMap(data[0])); err == nil {
				body = fmt.Sprintf("%s | %s", msg, string(b))
			} else {
				body = fmt.Sprintf("%s | %v", msg, formattedData(data[0]))
			}
		}

		// Map charmbracelet log levels to OTLP
		switch level {
		case log.DebugLevel:
			otelLogger.Debug(body, attrs...)
		case log.InfoLevel:
			otelLogger.Info(body, attrs...)
		case log.WarnLevel:
			otelLogger.Warn(body, attrs...)
		case log.ErrorLevel:
			otelLogger.Error(body, attrs...)
		case log.FatalLevel:
			otelLogger.Fatal(body, attrs...)
		default:
			otelLogger.Info(body, attrs...)
		}
	}

	// Write to MCAP file if available
	if l.mcapWriter != nil && !l.mcapWriter.IsClosed() {
		levelStr := level.String()

		// Get caller information for file and line
		file, line := getCallerInfo(3) // Skip 3 frames: getCallerInfo, log, and the calling function

		// Convert structured data to map for MCAP
		var dataMap map[string]interface{}
		if len(data) > 0 {
			dataMap = convertToMap(data[0])
		}

		// Write to MCAP with structured data in separate field
		if err := l.mcapWriter.WriteLog(levelStr, msg, file, uint32(line), dataMap); err != nil {
			l.loggerService.Warnf("Failed to write to MCAP: %v", err)
		}
	}
}

// Close closes the logger and any associated resources (e.g., MCAP writer)
func (l *Logger) Close() error {
	if l.mcapWriter != nil && !l.mcapWriter.IsClosed() {
		if err := l.mcapWriter.Close(); err != nil {
			return fmt.Errorf("failed to close MCAP writer: %w", err)
		}
		l.loggerService.Info("MCAP writer closed successfully")
	}
	return nil
}

// GetMcapWriter returns the MCAP writer if available (useful for custom logging)
func (l *Logger) GetMcapWriter() *LogMcapWriter {
	return l.mcapWriter
}
