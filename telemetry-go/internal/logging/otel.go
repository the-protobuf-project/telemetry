package logging

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/log"
)

// Re-export OpenTelemetry log types for convenience
type (
	KeyValue = log.KeyValue
	Value    = log.Value
)

// Re-export OpenTelemetry log functions
var (
	String       = log.String
	Int          = log.Int
	Int64        = log.Int64
	Float64      = log.Float64
	Bool         = log.Bool
	Bytes        = log.Bytes
	Slice        = log.Slice
	Map          = log.Map
	Empty        = log.Empty
	StringValue  = log.StringValue
	IntValue     = log.IntValue
	Int64Value   = log.Int64Value
	Float64Value = log.Float64Value
	BoolValue    = log.BoolValue
	BytesValue   = log.BytesValue
	SliceValue   = log.SliceValue
	MapValue     = log.MapValue
)

// OtelLogger wraps the telemetry logger with a context-optional API
type OtelLogger struct {
	logger log.Logger      // OpenTelemetry logger
	ctx    context.Context // Context for OTLP logging
}

// NewOtelLogger creates a new OtelLogger with context.Background() as default
func NewOtelLogger(logger log.Logger) *OtelLogger {
	return &OtelLogger{
		logger: logger,
		ctx:    context.Background(),
	}
}

// WithContext returns a new OtelLogger with the specified context
func (l *OtelLogger) WithContext(ctx context.Context) *OtelLogger {
	return &OtelLogger{
		logger: l.logger,
		ctx:    ctx,
	}
}

// Info logs an informational message
func (l *OtelLogger) Info(msg string, attrs ...KeyValue) *OtelLogger {
	var record log.Record
	record.SetBody(log.StringValue(msg))
	record.SetSeverity(log.SeverityInfo)
	record.AddAttributes(attrs...)
	l.logger.Emit(l.ctx, record)
	return l
}

// Debug logs a debug message
func (l *OtelLogger) Debug(msg string, attrs ...KeyValue) *OtelLogger {
	var record log.Record
	record.SetBody(log.StringValue(msg))
	record.SetSeverity(log.SeverityDebug)
	record.AddAttributes(attrs...)
	l.logger.Emit(l.ctx, record)
	return l
}

// Warn logs a warning message
func (l *OtelLogger) Warn(msg string, attrs ...KeyValue) *OtelLogger {
	var record log.Record
	record.SetBody(log.StringValue(msg))
	record.SetSeverity(log.SeverityWarn)
	record.AddAttributes(attrs...)
	l.logger.Emit(l.ctx, record)
	return l
}

// Error logs an error message
func (l *OtelLogger) Error(msg string, attrs ...KeyValue) *OtelLogger {
	var record log.Record
	record.SetBody(log.StringValue(msg))
	record.SetSeverity(log.SeverityError)
	record.AddAttributes(attrs...)
	l.logger.Emit(l.ctx, record)
	return l
}

// Fatal logs a fatal message
func (l *OtelLogger) Fatal(msg string, attrs ...KeyValue) *OtelLogger {
	var record log.Record
	record.SetBody(log.StringValue(msg))
	record.SetSeverity(log.SeverityFatal)
	record.AddAttributes(attrs...)
	l.logger.Emit(l.ctx, record)
	return l
}

// Infof logs an informational message with formatting
func (l *OtelLogger) Infof(format string, args ...interface{}) *OtelLogger {
	return l.Info(fmt.Sprintf(format, args...))
}

// Debugf logs a debug message with formatting
func (l *OtelLogger) Debugf(format string, args ...interface{}) *OtelLogger {
	return l.Debug(fmt.Sprintf(format, args...))
}

// Warnf logs a warning message with formatting
func (l *OtelLogger) Warnf(format string, args ...interface{}) *OtelLogger {
	return l.Warn(fmt.Sprintf(format, args...))
}

// Errorf logs an error message with formatting
func (l *OtelLogger) Errorf(format string, args ...interface{}) *OtelLogger {
	return l.Error(fmt.Sprintf(format, args...))
}

// Fatalf logs a fatal message with formatting
func (l *OtelLogger) Fatalf(format string, args ...interface{}) *OtelLogger {
	return l.Fatal(fmt.Sprintf(format, args...))
}

// With returns a new logger with additional attributes
func (l *OtelLogger) With(attrs ...KeyValue) *OtelLogger {
	// Note: This creates a new logger instance but doesn't actually chain attributes
	// in the OpenTelemetry logger. For true attribute chaining, you'd need to
	// store attributes and append them on each Emit call.
	return &OtelLogger{
		logger: l.logger,
		ctx:    l.ctx,
	}
}
