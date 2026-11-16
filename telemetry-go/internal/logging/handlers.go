package logging

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	otellog "go.opentelemetry.io/otel/log"
)

// otelHandler is the slog.Handler that emits records to OpenTelemetry. The
// body is the message; data travels as typed attributes, which Loki keeps as
// structured metadata and queries by key (| user_id = "...").
type otelHandler struct {
	logger otellog.Logger
	attrs  []attribute.KeyValue // from WithAttrs
	group  string               // key prefix from WithGroup
}

func newOtelHandler(l otellog.Logger) *otelHandler { return &otelHandler{logger: l} }

func (h *otelHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *otelHandler) Handle(ctx context.Context, r slog.Record) error {
	var rec otellog.Record
	rec.SetTimestamp(r.Time)
	rec.SetBody(attribute.StringValue(r.Message))
	rec.SetSeverity(otellog.Severity(r.Level + 9)) // slog DEBUG..FATAL (-4..12) onto OTel 5..21
	rec.SetSeverityText(levelName(r.Level))

	attrs := make([]attribute.KeyValue, 0, len(h.attrs)+r.NumAttrs()+2)
	attrs = append(attrs, h.attrs...)
	if r.PC != 0 {
		file, line := callSite(r.PC)
		attrs = append(attrs, attribute.String("code.filepath", file), attribute.Int("code.lineno", line))
	}
	r.Attrs(func(a slog.Attr) bool {
		attrs = appendOtel(attrs, h.group, a)
		return true
	})
	rec.AddAttributes(attrs...)
	h.logger.Emit(ctx, rec)
	return nil
}

// callSites caches each log call's file and line by program counter: a
// program logs from a fixed set of places, and resolving a PC every time was
// a tenth of a log call.
var callSites sync.Map // uintptr -> site

type site struct {
	file string
	line int
}

func callSite(pc uintptr) (string, int) {
	if s, ok := callSites.Load(pc); ok {
		return s.(site).file, s.(site).line
	}
	f, _ := runtime.CallersFrames([]uintptr{pc}).Next()
	s := site{filepath.Base(f.File), f.Line}
	callSites.Store(pc, s)
	return s.file, s.line
}

func (h *otelHandler) WithAttrs(as []slog.Attr) slog.Handler {
	c := *h
	c.attrs = append([]attribute.KeyValue(nil), h.attrs...)
	for _, a := range as {
		c.attrs = appendOtel(c.attrs, h.group, a)
	}
	return &c
}

func (h *otelHandler) WithGroup(name string) slog.Handler {
	c := *h
	c.group = join(h.group, name)
	return &c
}

// appendOtel appends a as OpenTelemetry attributes, flattening groups into
// dotted keys.
func appendOtel(dst []attribute.KeyValue, prefix string, a slog.Attr) []attribute.KeyValue {
	v := a.Value.Resolve()
	if a.Key == "" && v.Kind() != slog.KindGroup {
		return dst
	}
	key := join(prefix, a.Key)
	switch v.Kind() {
	case slog.KindString:
		return append(dst, attribute.String(key, v.String()))
	case slog.KindInt64:
		return append(dst, attribute.Int64(key, v.Int64()))
	case slog.KindUint64:
		return append(dst, attribute.Int64(key, int64(v.Uint64())))
	case slog.KindFloat64:
		return append(dst, attribute.Float64(key, v.Float64()))
	case slog.KindBool:
		return append(dst, attribute.Bool(key, v.Bool()))
	case slog.KindDuration:
		return append(dst, attribute.String(key, v.Duration().String()))
	case slog.KindTime:
		return append(dst, attribute.String(key, v.Time().Format(time.RFC3339Nano)))
	case slog.KindGroup:
		for _, ga := range v.Group() {
			dst = appendOtel(dst, key, ga)
		}
		return dst
	default:
		return append(dst, attribute.String(key, anyString(v.Any())))
	}
}

func join(prefix, key string) string {
	switch {
	case prefix == "":
		return key
	case key == "":
		return prefix
	default:
		return prefix + "." + key
	}
}

// anyString renders a value slog has no kind for: an error by its message,
// []byte as text, composites as JSON, the rest as fmt prints them.
func anyString(v any) string {
	switch x := v.(type) {
	case nil:
		return "<nil>"
	case error:
		return x.Error()
	case []byte:
		return string(x)
	case fmt.Stringer:
		return x.String()
	}
	switch reflect.Indirect(reflect.ValueOf(v)).Kind() {
	case reflect.Map, reflect.Struct, reflect.Slice, reflect.Array:
		if b, err := json.Marshal(v); err == nil {
			return string(b)
		}
	}
	return fmt.Sprintf("%+v", v)
}

// mcapHandler is the slog.Handler that records to MCAP in the foxglove.Log schema.
type mcapHandler struct {
	w     *LogMcapWriter
	attrs []slog.Attr
	group string
}

func (h *mcapHandler) Enabled(context.Context, slog.Level) bool { return !h.w.IsClosed() }

func (h *mcapHandler) Handle(_ context.Context, r slog.Record) error {
	if h.w.IsClosed() {
		return nil
	}
	var data map[string]any
	add := func(a slog.Attr) bool {
		if data == nil {
			data = make(map[string]any, r.NumAttrs()+len(h.attrs))
		}
		data[join(h.group, a.Key)] = a.Value.Resolve().Any()
		return true
	}
	for _, a := range h.attrs {
		add(a)
	}
	r.Attrs(add)
	file, line := "unknown", 0
	if r.PC != 0 {
		f, _ := runtime.CallersFrames([]uintptr{r.PC}).Next()
		file, line = filepath.Base(f.File), f.Line
	}
	return h.w.WriteLogAt(r.Time, levelName(r.Level), r.Message, file, uint32(line), data)
}

func (h *mcapHandler) WithAttrs(as []slog.Attr) slog.Handler {
	c := *h
	c.attrs = append(append([]slog.Attr(nil), h.attrs...), as...)
	return &c
}

func (h *mcapHandler) WithGroup(name string) slog.Handler {
	c := *h
	c.group = join(h.group, name)
	return &c
}

func levelName(l slog.Level) string {
	switch {
	case l >= LevelFatal:
		return "FATAL"
	case l >= slog.LevelError:
		return "ERROR"
	case l >= slog.LevelWarn:
		return "WARN"
	case l >= slog.LevelInfo:
		return "INFO"
	default:
		return "DEBUG"
	}
}

// dataAttrs turns one item of the legacy structured data into attributes:
//
//   - slog.Attr and attribute.KeyValue are used as they are;
//   - a map gives one attribute per entry, so Loki can filter by each key;
//   - a struct gives its `telemetry:"attribute:<key>"` fields, plus the
//     whole value as JSON under "data" and its type under "struct_type";
//   - anything else is recorded under "data".
func dataAttrs(d any) []slog.Attr {
	switch x := d.(type) {
	case nil:
		return nil
	case slog.Attr:
		return []slog.Attr{x}
	case attribute.KeyValue:
		return []slog.Attr{slog.Any(string(x.Key), x.Value.AsInterface())}
	case map[string]any:
		attrs := make([]slog.Attr, 0, len(x))
		for k, v := range x {
			attrs = append(attrs, slog.Any(k, v))
		}
		return attrs
	case map[string]string:
		attrs := make([]slog.Attr, 0, len(x))
		for k, v := range x {
			attrs = append(attrs, slog.String(k, v))
		}
		return attrs
	}

	rv := reflect.ValueOf(d)
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Map:
		attrs := make([]slog.Attr, 0, rv.Len())
		for it := rv.MapRange(); it.Next(); {
			attrs = append(attrs, slog.Any(convertKeyToString(it.Key().Interface()), it.Value().Interface()))
		}
		return attrs
	case reflect.Struct:
		attrs := taggedAttrs(rv, nil)
		attrs = append(attrs, slog.String("struct_type", rv.Type().Name()))
		if b, err := json.Marshal(rv.Interface()); err == nil {
			attrs = append(attrs, slog.Any("data", jsonText(b)))
		} else {
			attrs = append(attrs, slog.String("data", fmt.Sprintf("%+v", rv.Interface())))
		}
		return attrs
	default:
		return []slog.Attr{slog.Any("data", rv.Interface())}
	}
}

// taggedAttrs collects exported fields tagged `telemetry:"attribute:<key>"`,
// including those of nested structs.
func taggedAttrs(rv reflect.Value, dst []slog.Attr) []slog.Attr {
	rt := rv.Type()
	for i := range rv.NumField() {
		field, fv := rt.Field(i), rv.Field(i)
		if !field.IsExported() {
			continue
		}
		for part := range strings.FieldsSeq(field.Tag.Get("telemetry")) {
			if key, ok := strings.CutPrefix(part, "attribute:"); ok {
				dst = append(dst, slog.Any(key, fv.Interface()))
			}
		}
		switch {
		case fv.Kind() == reflect.Struct:
			dst = taggedAttrs(fv, dst)
		case fv.Kind() == reflect.Pointer && !fv.IsNil() && fv.Elem().Kind() == reflect.Struct:
			dst = taggedAttrs(fv.Elem(), dst)
		}
	}
	return dst
}

// jsonText is JSON already encoded: printed as text on the console and in
// OpenTelemetry, embedded as the object it is in MCAP.
type jsonText string

func (j jsonText) String() string               { return string(j) }
func (j jsonText) MarshalJSON() ([]byte, error) { return []byte(j), nil }
