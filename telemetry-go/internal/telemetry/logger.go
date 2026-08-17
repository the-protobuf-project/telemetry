package telemetry

import (
	"context"
	"fmt"

	"github.com/the-protobuf-project/telemetry/telemetry-go/options"
	"go.opentelemetry.io/otel/log"
)

// Logger provides a simplified interface for OpenTelemetry logging
type Logger struct {
	logger log.Logger
	opts   options.LoggingTelemetryOptions
}

// NewLogger creates a new Logger instance
func NewLogger(logger log.Logger, opts options.LoggingTelemetryOptions) *Logger {
	return &Logger{
		logger: logger,
		opts:   opts,
	}
}

// Info logs an informational message
func (l *Logger) Info(ctx context.Context, msg string, attrs ...log.KeyValue) {
	l.emit(ctx, log.SeverityInfo, msg, attrs...)
}

// Debug logs a debug message
func (l *Logger) Debug(ctx context.Context, msg string, attrs ...log.KeyValue) {
	l.emit(ctx, log.SeverityDebug, msg, attrs...)
}

// Warn logs a warning message
func (l *Logger) Warn(ctx context.Context, msg string, attrs ...log.KeyValue) {
	l.emit(ctx, log.SeverityWarn, msg, attrs...)
}

// Error logs an error message
func (l *Logger) Error(ctx context.Context, msg string, attrs ...log.KeyValue) {
	l.emit(ctx, log.SeverityError, msg, attrs...)
}

// Fatal logs a fatal message
func (l *Logger) Fatal(ctx context.Context, msg string, attrs ...log.KeyValue) {
	l.emit(ctx, log.SeverityFatal, msg, attrs...)
}

// Infof logs an informational message with formatting
func (l *Logger) Infof(ctx context.Context, format string, args ...interface{}) {
	l.Info(ctx, fmt.Sprintf(format, args...))
}

// Debugf logs a debug message with formatting
func (l *Logger) Debugf(ctx context.Context, format string, args ...interface{}) {
	l.Debug(ctx, fmt.Sprintf(format, args...))
}

// Warnf logs a warning message with formatting
func (l *Logger) Warnf(ctx context.Context, format string, args ...interface{}) {
	l.Warn(ctx, fmt.Sprintf(format, args...))
}

// Errorf logs an error message with formatting
func (l *Logger) Errorf(ctx context.Context, format string, args ...interface{}) {
	l.Error(ctx, fmt.Sprintf(format, args...))
}

// Fatalf logs a fatal message with formatting
func (l *Logger) Fatalf(ctx context.Context, format string, args ...interface{}) {
	l.Fatal(ctx, fmt.Sprintf(format, args...))
}

// emit is the internal method that emits log records
func (l *Logger) emit(ctx context.Context, severity log.Severity, msg string, attrs ...log.KeyValue) {
	var record log.Record
	record.SetBody(log.StringValue(msg))
	record.SetSeverity(severity)
	record.AddAttributes(attrs...)

	l.logger.Emit(ctx, record)
}

// WithAttrs returns a new Logger with additional attributes
func (l *Logger) WithAttrs(attrs ...log.KeyValue) *Logger {
	// Create a new logger with attributes
	// Note: This is a simplified implementation
	// In production, you might want to implement attribute chaining
	return &Logger{
		logger: l.logger,
		opts:   l.opts,
	}
}

// Non-context versions of logging methods for compatibility with logging.Logger interface
// These methods use context.Background() internally

// InfoMsg logs an informational message without requiring context
func (l *Logger) InfoMsg(msg string, data ...interface{}) {
	ctx := context.Background()
	if len(data) > 0 {
		l.Info(ctx, msg, convertToKeyValues(data)...)
	} else {
		l.Info(ctx, msg)
	}
}

// DebugMsg logs a debug message without requiring context
func (l *Logger) DebugMsg(msg string, data ...interface{}) {
	ctx := context.Background()
	if len(data) > 0 {
		l.Debug(ctx, msg, convertToKeyValues(data)...)
	} else {
		l.Debug(ctx, msg)
	}
}

// WarnMsg logs a warning message without requiring context
func (l *Logger) WarnMsg(msg string, data ...interface{}) {
	ctx := context.Background()
	if len(data) > 0 {
		l.Warn(ctx, msg, convertToKeyValues(data)...)
	} else {
		l.Warn(ctx, msg)
	}
}

// ErrorMsg logs an error message without requiring context and returns an error
func (l *Logger) ErrorMsg(msg string, data ...interface{}) error {
	ctx := context.Background()
	if len(data) > 0 {
		l.Error(ctx, msg, convertToKeyValues(data)...)
	} else {
		l.Error(ctx, msg)
	}
	return fmt.Errorf(msg, data...)
}

// FatalMsg logs a fatal message without requiring context
func (l *Logger) FatalMsg(msg string, data ...interface{}) {
	ctx := context.Background()
	if len(data) > 0 {
		l.Fatal(ctx, msg, convertToKeyValues(data)...)
	} else {
		l.Fatal(ctx, msg)
	}
}

// Printf logs a message using a format string
func (l *Logger) Printf(format string, args ...interface{}) {
	l.Infof(context.Background(), format, args...)
}

// convertToKeyValues converts variadic interface{} data to log.KeyValue attributes
// This is a helper function to bridge the gap between logging.Logger and telemetry.Logger
func convertToKeyValues(data []interface{}) []log.KeyValue {
	if len(data) == 0 {
		return nil
	}

	// If the first element is already a KeyValue, return as-is
	if _, ok := data[0].(log.KeyValue); ok {
		kvs := make([]log.KeyValue, len(data))
		for i, d := range data {
			if kv, ok := d.(log.KeyValue); ok {
				kvs[i] = kv
			}
		}
		return kvs
	}

	// Otherwise, treat the first element as structured data
	// Convert it to a single "data" attribute
	return []log.KeyValue{
		log.String("data", fmt.Sprintf("%v", data[0])),
	}
}
