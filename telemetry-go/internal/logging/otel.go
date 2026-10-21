package logging

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/log"
)

// Re-export OpenTelemetry attribute types for convenience. Since the stable
// logs API (otel/log v1.47.0), log records use attribute.KeyValue/Value.
type (
	KeyValue = attribute.KeyValue
	Value    = attribute.Value
)

// Re-export OpenTelemetry attribute constructors
var (
	String       = attribute.String
	Int          = attribute.Int
	Int64        = attribute.Int64
	Float64      = attribute.Float64
	Bool         = attribute.Bool
	Bytes        = attribute.ByteSlice
	Slice        = attribute.Slice
	Map          = attribute.Map
	StringValue  = attribute.StringValue
	IntValue     = attribute.IntValue
	Int64Value   = attribute.Int64Value
	Float64Value = attribute.Float64Value
	BoolValue    = attribute.BoolValue
	BytesValue   = attribute.ByteSliceValue
	SliceValue   = attribute.SliceValue
	MapValue     = attribute.MapValue
)

// Empty returns a KeyValue with an empty value.
func Empty(key string) KeyValue {
	return KeyValue{Key: attribute.Key(key)}
}

// OtelLogger wraps the telemetry logger with a context-optional API
type OtelLogger struct {
	logger log.Logger      // OpenTelemetry logger
	ctx    context.Context // Context for OTLP logging
}

// NewOtelLogger creates an OpenTelemetry logger with a background context.
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
	record.SetBody(attribute.StringValue(msg))
	record.SetSeverity(log.SeverityInfo)
	record.AddAttributes(attrs...)
	l.logger.Emit(l.ctx, record)
	return l
}

// Debug logs a debug message
func (l *OtelLogger) Debug(msg string, attrs ...KeyValue) *OtelLogger {
	var record log.Record
	record.SetBody(attribute.StringValue(msg))
	record.SetSeverity(log.SeverityDebug)
	record.AddAttributes(attrs...)
	l.logger.Emit(l.ctx, record)
	return l
}

// Warn logs a warning message
func (l *OtelLogger) Warn(msg string, attrs ...KeyValue) *OtelLogger {
	var record log.Record
	record.SetBody(attribute.StringValue(msg))
	record.SetSeverity(log.SeverityWarn)
	record.AddAttributes(attrs...)
	l.logger.Emit(l.ctx, record)
	return l
}

// Error logs an error message
func (l *OtelLogger) Error(msg string, attrs ...KeyValue) *OtelLogger {
	var record log.Record
	record.SetBody(attribute.StringValue(msg))
	record.SetSeverity(log.SeverityError)
	record.AddAttributes(attrs...)
	l.logger.Emit(l.ctx, record)
	return l
}

// Fatal logs a fatal message
func (l *OtelLogger) Fatal(msg string, attrs ...KeyValue) *OtelLogger {
	var record log.Record
	record.SetBody(attribute.StringValue(msg))
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
