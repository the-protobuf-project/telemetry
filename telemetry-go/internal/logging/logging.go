package logging

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"time"

	"github.com/charmbracelet/log"
	"github.com/the-protobuf-project/telemetry/telemetry-go/internal/foxglove"
	"github.com/the-protobuf-project/telemetry/telemetry-go/options"

	otellog "go.opentelemetry.io/otel/log"
)

// LevelFatal is the level Fatal and Fatalf log at, above slog.LevelError.
const LevelFatal = slog.Level(12)

// Logger is the main logging client for the telemetry framework.
//
// Every record goes through one slog.Handler that fans out to the console
// (charmbracelet/log by default, or the program's own handler — see
// Builder.WithLogHandler), to OpenTelemetry when log records are exported,
// and to MCAP when recording. The level is checked once, before a record is
// built, so a filtered-out Debug call costs next to nothing.
type Logger struct {
	handler slog.Handler
	console slog.Handler
	mcap    *LogMcapWriter
	ctx     context.Context
	skip    int // extra caller frames to skip (options.LogOptions.CallerOffset)
}

// NewLogger builds the logger for a service. console is where records are
// printed; nil uses the default charmbracelet console on stderr. otelLogger,
// when not nil, receives every record for OTLP export, and unifiedWriter,
// when not nil, records them to MCAP.
func NewLogger(serviceOpts options.ServiceOptions, opts options.LoggingOptions, unifiedWriter *foxglove.UnifiedMcapWriter, otelLogger otellog.Logger, console slog.Handler) *Logger {
	level := slog.Level(resolveLogLevel(serviceOpts.Name, opts, serviceOpts.Environment))
	if console == nil {
		console = log.NewWithOptions(os.Stderr, log.Options{
			Prefix:          formatPrefix(serviceOpts),
			Level:           log.Level(level),
			ReportCaller:    true, // Always show file:line
			ReportTimestamp: true, // Always show timestamp
			TimeFormat:      resolveTimeFormat(opts),
		})
	}

	sinks := []slog.Handler{console}
	if otelLogger != nil {
		sinks = append(sinks, newOtelHandler(otelLogger))
	}
	l := &Logger{
		console: console,
		ctx:     context.Background(),
		skip:    max(resolveCallerOffset(opts)-3, 0),
	}
	if unifiedWriter != nil {
		mcapWriter, err := NewLogMcapWriter(serviceOpts, unifiedWriter)
		if err != nil {
			printTo(console, slog.LevelError, "Failed to initialize MCAP log writer", "error", err)
		} else {
			l.mcap = mcapWriter
			sinks = append(sinks, &mcapHandler{w: mcapWriter})
		}
	}
	l.handler = &levelHandler{level: level, next: slog.NewMultiHandler(sinks...)}
	if l.mcap != nil {
		printTo(console, slog.LevelInfo, "MCAP logging enabled", "path", unifiedWriter.GetFilePath())
	}
	return l
}

// Slog returns a *slog.Logger writing through this logger: to the console,
// OpenTelemetry and MCAP alike. Records logged with a context carrying a span
// (InfoContext and friends) are correlated with that trace.
func (l *Logger) Slog() *slog.Logger { return slog.New(l.handler) }

// Handler is the slog.Handler behind Slog, for composing with other handlers.
func (l *Logger) Handler() slog.Handler { return l.handler }

// Console is the handler records are printed with: the program's own (see
// Builder.WithLogHandler) or the default console.
func (l *Logger) Console() slog.Handler { return l.console }

// WithContext returns a Logger whose records carry ctx, so they are
// correlated with the span ctx holds.
func (l *Logger) WithContext(ctx context.Context) *Logger {
	c := *l
	c.ctx = ctx
	return &c
}

// Info logs an info-level message with optional structured data: a map
// (each entry becomes an attribute), a struct (its `telemetry:"attribute:k"`
// fields become attributes and the whole value a JSON "data" attribute),
// slog.Attr values, or any other value (recorded as "data").
func (l *Logger) Info(msg string, data ...any) { l.emit(l.ctx, slog.LevelInfo, msg, nil, data) }

// Debug logs a debug-level message with optional structured data.
func (l *Logger) Debug(msg string, data ...any) { l.emit(l.ctx, slog.LevelDebug, msg, nil, data) }

// Warn logs a warning-level message with optional structured data.
func (l *Logger) Warn(msg string, data ...any) { l.emit(l.ctx, slog.LevelWarn, msg, nil, data) }

// Error logs an error-level message with optional structured data, and
// returns msg as an error.
func (l *Logger) Error(msg string, data ...any) error {
	l.emit(l.ctx, slog.LevelError, msg, nil, data)
	return fmt.Errorf("%s", msg)
}

// Fatal logs a fatal-level message with optional structured data and exits the program.
func (l *Logger) Fatal(msg string, data ...any) {
	l.emit(l.ctx, LevelFatal, msg, nil, data)
	os.Exit(1)
}

// Infof logs an info-level message using a format string.
func (l *Logger) Infof(format string, args ...any) { l.emitf(slog.LevelInfo, format, args) }

// Debugf logs a debug-level message using a format string.
func (l *Logger) Debugf(format string, args ...any) { l.emitf(slog.LevelDebug, format, args) }

// Warnf logs a warning-level message using a format string.
func (l *Logger) Warnf(format string, args ...any) { l.emitf(slog.LevelWarn, format, args) }

// Errorf logs an error-level message using a format string, and returns it
// as an error.
func (l *Logger) Errorf(format string, args ...any) error {
	l.emitf(slog.LevelError, format, args)
	return fmt.Errorf(format, args...)
}

// Fatalf logs a fatal-level message using a format string and exits the program.
func (l *Logger) Fatalf(format string, args ...any) {
	l.emitf(LevelFatal, format, args)
	os.Exit(1)
}

// DebugContext logs at debug level with slog-style key-value args, correlated
// with the span in ctx.
func (l *Logger) DebugContext(ctx context.Context, msg string, args ...any) {
	l.emit(ctx, slog.LevelDebug, msg, args, nil)
}

// InfoContext logs at info level with slog-style key-value args, correlated
// with the span in ctx.
func (l *Logger) InfoContext(ctx context.Context, msg string, args ...any) {
	l.emit(ctx, slog.LevelInfo, msg, args, nil)
}

// WarnContext logs at warn level with slog-style key-value args, correlated
// with the span in ctx.
func (l *Logger) WarnContext(ctx context.Context, msg string, args ...any) {
	l.emit(ctx, slog.LevelWarn, msg, args, nil)
}

// ErrorContext logs at error level with slog-style key-value args, correlated
// with the span in ctx.
func (l *Logger) ErrorContext(ctx context.Context, msg string, args ...any) {
	l.emit(ctx, slog.LevelError, msg, args, nil)
}

// emit builds one record and hands it to the handler. Callers are the
// exported methods only, so the caller's frame is a fixed distance away.
// args are slog-style key-value pairs; data is the legacy structured data.
func (l *Logger) emit(ctx context.Context, level slog.Level, msg string, args, data []any) {
	if !l.handler.Enabled(ctx, level) {
		return
	}
	var pcs [1]uintptr
	runtime.Callers(3+l.skip, pcs[:]) // runtime.Callers, emit, the exported method
	r := slog.NewRecord(time.Now(), level, msg, pcs[0])
	if len(args) > 0 {
		r.Add(args...)
	}
	for _, d := range data {
		r.AddAttrs(dataAttrs(d)...)
	}
	_ = l.handler.Handle(ctx, r)
}

func (l *Logger) emitf(level slog.Level, format string, args []any) {
	if !l.handler.Enabled(l.ctx, level) {
		return
	}
	var pcs [1]uintptr
	runtime.Callers(3+l.skip, pcs[:]) // runtime.Callers, emitf, the exported method
	r := slog.NewRecord(time.Now(), level, fmt.Sprintf(format, args...), pcs[0])
	_ = l.handler.Handle(l.ctx, r)
}

// Close is a no-op: the MCAP file is closed by Telemetry.Close.
func (l *Logger) Close() error { return nil }

// GetMcapWriter returns the MCAP writer if available (useful for custom logging)
func (l *Logger) GetMcapWriter() *LogMcapWriter { return l.mcap }

// levelHandler drops records below level before any sink sees them.
type levelHandler struct {
	level slog.Level
	next  slog.Handler
}

func (h *levelHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.level && h.next.Enabled(ctx, level)
}

func (h *levelHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level < h.level {
		return nil
	}
	return h.next.Handle(ctx, r)
}

func (h *levelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &levelHandler{level: h.level, next: h.next.WithAttrs(attrs)}
}

func (h *levelHandler) WithGroup(name string) slog.Handler {
	return &levelHandler{level: h.level, next: h.next.WithGroup(name)}
}

// printTo logs one record straight to h, for the logger's own messages.
func printTo(h slog.Handler, level slog.Level, msg string, args ...any) {
	if !h.Enabled(context.Background(), level) {
		return
	}
	r := slog.NewRecord(time.Now(), level, msg, 0)
	r.Add(args...)
	_ = h.Handle(context.Background(), r)
}
