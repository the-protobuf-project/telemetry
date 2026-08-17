// Package source implements protoc-gen-telemetry's factory.Source: it walks
// the proto descriptors the plugin was invoked with and builds the
// plugin/model IR for every message that carries (telemetry.v1.telemetry)
// and resolves to metrics being on.
package source

import (
	"fmt"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"

	"google.golang.org/genproto/googleapis/api/annotations"

	"github.com/the-protobuf-project/protokit/factory"
	"github.com/the-protobuf-project/protokit/naming"
	"github.com/the-protobuf-project/protokit/schema"
	"github.com/the-protobuf-project/protokit/types"

	telemetrypbv1 "github.com/the-protobuf-project/telemetry/telemetry-go/protobuf/telemetry/v1/telemetrypbv1"

	"github.com/the-protobuf-project/telemetry/telemetry-go/plugin/model"
)

// Source implements factory.Source[*model.Model]. It is stateful only in
// that it accumulates generate-time Warnings across a Build call — main.go
// prints them to stderr after a successful run, matching the "warn, don't
// fail" contract for a metric_field.label=true override on a poor-fit field
// type.
type Source struct {
	// Warnings collects human-readable, generate-time-only warnings (never
	// fatal) produced while building the Model. Populated by Build; empty
	// until Build is called.
	Warnings []string
}

// New returns an empty Source ready to Build.
func New() *Source { return &Source{} }

// Name identifies this source in a factory.Registry.
func (s *Source) Name() string { return "proto" }

// Build walks every generated proto file's messages (including nested ones)
// looking for (telemetry.v1.telemetry), resolving each into a *model.Resource
// when it (and its Metrics option) is enabled.
func (s *Source) Build(ctx factory.Ctx) (*model.Model, error) {
	if ctx.Plugin == nil {
		return nil, fmt.Errorf("telemetry source requires a protoc plugin context (only available in a buf/protoc run)")
	}

	m := &model.Model{}
	for _, file := range ctx.Plugin.Files {
		if !file.Generate {
			continue
		}
		s.walkMessages(m, file, file.Messages)
	}
	return m, nil
}

// walkMessages recursively visits msgs (and their nested messages),
// appending a Resource to m for each one in scope.
func (s *Source) walkMessages(m *model.Model, file *protogen.File, msgs []*protogen.Message) {
	for _, msg := range msgs {
		if res := s.buildResource(file, msg); res != nil {
			m.Resources = append(m.Resources, res)
		}
		s.walkMessages(m, file, msg.Messages)
	}
}

// buildResource resolves one message into a Resource, or nil when the
// message doesn't carry (telemetry.v1.telemetry) or has resolved its
// Enabled/Metrics options to off.
func (s *Source) buildResource(file *protogen.File, msg *protogen.Message) *model.Resource {
	if !proto.HasExtension(msg.Desc.Options(), telemetrypbv1.E_Telemetry) {
		return nil
	}
	opts, _ := proto.GetExtension(msg.Desc.Options(), telemetrypbv1.E_Telemetry).(*telemetrypbv1.TelemetryOptions)
	if opts == nil {
		opts = &telemetrypbv1.TelemetryOptions{}
	}

	enabled := true
	if opts.Enabled != nil {
		enabled = opts.GetEnabled()
	}
	metricsOn := true
	if opts.Metrics != nil {
		metricsOn = opts.GetMetrics()
	}
	if !enabled || !metricsOn {
		return nil
	}

	res := &model.Resource{
		Name:           msg.GoIdent.GoName,
		GoIdent:        msg.GoIdent,
		SourceFile:     file,
		MetricsEnabled: true,
	}
	for _, f := range msg.Fields {
		if field := s.buildField(msg, f); field != nil {
			res.Fields = append(res.Fields, field)
		}
		if vm := s.buildValueMetric(res.Name, msg, f); vm != nil {
			res.ValueMetrics = append(res.ValueMetrics, vm)
		}
	}
	return res
}

// buildField resolves one field to a *model.Field metric label, or nil when
// the field is not a label (either by inference or by an explicit
// override=false).
func (s *Source) buildField(msg *protogen.Message, f *protogen.Field) *model.Field {
	fieldOpts := metricFieldOpts(f)
	override := fieldOpts.Label // *bool: nil means "no override" (Label unset)

	isResourceRef := false
	if ref, ok := proto.GetExtension(f.Desc.Options(), annotations.E_ResourceReference).(*annotations.ResourceReference); ok && ref != nil && ref.GetType() != "" {
		isResourceRef = true
	}

	fieldType := types.ClassifyField(f)
	decision := model.InferLabel(override, fieldType, isResourceRef)

	if decision.IsLabel && f.Desc.IsMap() {
		// Map fields have no sensible single-value label rendering; rather
		// than emit broken code for an edge case the spec doesn't call out,
		// warn and exclude — every other type (scalar, enum, message,
		// repeated) is still supported below, forced-in or not.
		s.Warnings = append(s.Warnings, fmt.Sprintf(
			"metric_field.label=true on %s.%s is unsupported (map fields cannot be used as metric labels); excluding it",
			msg.GoIdent.GoName, f.Desc.Name()))
		return nil
	}
	if !decision.IsLabel {
		return nil
	}
	if decision.Warn {
		s.Warnings = append(s.Warnings, fmt.Sprintf(
			"metric_field.label=true on %s.%s (type %s) may be a poor label choice",
			msg.GoIdent.GoName, f.Desc.Name(), f.Desc.Kind()))
	}

	labelKey := fieldOpts.GetLabelKey()
	if labelKey == "" {
		labelKey = naming.SnakeCase(string(f.Desc.Name()))
	}

	field := &model.Field{
		Name:     naming.Camel(string(f.Desc.Name())),
		Column:   string(f.Desc.Name()),
		LabelKey: labelKey,
	}
	populateType(field, f, fieldType)
	return field
}

// buildValueMetric resolves one field to a *model.ValueMetric when
// MetricFieldOptions.value is true, or nil otherwise — including when the
// field's type isn't a single numeric scalar, in which case a warning is
// recorded rather than treating it as fatal, so a plugin run always finishes
// and every problem surfaces as text on stderr instead of stopping the first
// one hit.
func (s *Source) buildValueMetric(resourceName string, msg *protogen.Message, f *protogen.Field) *model.ValueMetric {
	fieldOpts := metricFieldOpts(f)
	if !fieldOpts.GetValue() {
		return nil
	}

	fieldType := types.ClassifyField(f)
	goType, toFloat64Expr, ok := numericConversion(fieldType)
	if !ok || f.Desc.IsList() || f.Desc.IsMap() {
		s.Warnings = append(s.Warnings, fmt.Sprintf(
			"metric_field.value=true on %s.%s (type %s) is unsupported (a value metric needs a single numeric field); ignoring it",
			msg.GoIdent.GoName, f.Desc.Name(), f.Desc.Kind()))
		return nil
	}

	column := string(f.Desc.Name())
	kind := model.InferKind(protoKindToModelKind(fieldOpts.GetKind()), column)

	name := fieldOpts.GetMetric()
	if name == "" {
		name = naming.SnakeCase(resourceName) + "_" + naming.SnakeCase(column)
	}

	unit := fieldOpts.GetUnit()
	if unit == "" {
		unit = "1"
		if kind == model.KindHistogram && model.IsDurationLike(column) {
			unit = "ms"
		}
	}

	return &model.ValueMetric{
		FieldName:     naming.Camel(column),
		Column:        column,
		MetricName:    name,
		Kind:          kind,
		GoType:        goType,
		ToFloat64Expr: toFloat64Expr,
		Unit:          unit,
		Buckets:       fieldOpts.GetBuckets(),
	}
}

// numericConversion returns the Go scalar type and float64 conversion
// expression runtime-go/telemetry's instrument methods need for a
// value-metric-eligible FieldType, or ok=false when fieldType isn't a numeric
// scalar (string, bool, enum, bytes, message are all ineligible — list/map
// are rejected separately by the caller, since ClassifyField doesn't encode
// cardinality).
func numericConversion(fieldType schema.FieldType) (goType string, toFloat64Expr func(string) string, ok bool) {
	identity := func(expr string) string { return expr }
	cast := func(expr string) string { return "float64(" + expr + ")" }
	switch fieldType {
	case schema.TypeInt32:
		return "int32", cast, true
	case schema.TypeUint32:
		return "uint32", cast, true
	case schema.TypeInt64:
		return "int64", cast, true
	case schema.TypeUint64:
		return "uint64", cast, true
	case schema.TypeFloat:
		return "float32", cast, true
	case schema.TypeDouble:
		return "float64", identity, true
	default:
		return "", nil, false
	}
}

// protoKindToModelKind translates telemetry.v1's generated MetricKind enum
// into the plugin's own model.MetricKind, so model/infer.go and gengo don't
// need to import the generated proto package themselves — only source.go,
// which already imports telemetrypbv1 to read the annotation in the first
// place, does the translation.
func protoKindToModelKind(k telemetrypbv1.MetricKind) model.MetricKind {
	switch k {
	case telemetrypbv1.MetricKind_METRIC_KIND_COUNTER:
		return model.KindCounter
	case telemetrypbv1.MetricKind_METRIC_KIND_UP_DOWN_COUNTER:
		return model.KindUpDownCounter
	case telemetrypbv1.MetricKind_METRIC_KIND_GAUGE:
		return model.KindGauge
	case telemetrypbv1.MetricKind_METRIC_KIND_HISTOGRAM:
		return model.KindHistogram
	default:
		return model.KindUnspecified
	}
}

// populateType fills in field's GoType/GoIdent/Pointer/Repeated/
// NeedsStrconv/NeedsFmtFallback/ToStringExpr from f's proto kind. A repeated
// (list, non-map — maps are excluded above) field always falls back to
// fmt.Sprintf("%%v", ...) for stringification regardless of its element
// kind, since a slice never satisfies the string/bool/enum special cases.
func populateType(field *model.Field, f *protogen.Field, fieldType schema.FieldType) {
	switch fieldType {
	case schema.TypeBool:
		field.GoType = "bool"
		if f.Desc.IsList() {
			field.NeedsFmtFallback = true
			field.ToStringExpr = fmtFallback
		} else {
			field.NeedsStrconv = true
			field.ToStringExpr = func(expr string) string { return "strconv.FormatBool(" + expr + ")" }
		}
	case schema.TypeEnum:
		field.GoIdent = f.Enum.GoIdent
		if f.Desc.IsList() {
			field.NeedsFmtFallback = true
			field.ToStringExpr = fmtFallback
		} else {
			field.ToStringExpr = func(expr string) string { return expr + ".String()" }
		}
	case schema.TypeString:
		field.GoType = "string"
		if f.Desc.IsList() {
			field.NeedsFmtFallback = true
			field.ToStringExpr = fmtFallback
		} else {
			field.ToStringExpr = func(expr string) string { return expr }
		}
	case schema.TypeInt32:
		field.GoType = "int32"
		field.NeedsFmtFallback, field.ToStringExpr = true, fmtFallback
	case schema.TypeUint32:
		field.GoType = "uint32"
		field.NeedsFmtFallback, field.ToStringExpr = true, fmtFallback
	case schema.TypeInt64:
		field.GoType = "int64"
		field.NeedsFmtFallback, field.ToStringExpr = true, fmtFallback
	case schema.TypeUint64:
		field.GoType = "uint64"
		field.NeedsFmtFallback, field.ToStringExpr = true, fmtFallback
	case schema.TypeFloat:
		field.GoType = "float32"
		field.NeedsFmtFallback, field.ToStringExpr = true, fmtFallback
	case schema.TypeDouble:
		field.GoType = "float64"
		field.NeedsFmtFallback, field.ToStringExpr = true, fmtFallback
	case schema.TypeBytes:
		field.GoType = "[]byte"
		field.NeedsFmtFallback, field.ToStringExpr = true, fmtFallback
	default:
		// Anything else classifies as a message type (a well-known type like
		// google.protobuf.Timestamp, or a plain nested/imported message) —
		// f.Message is set for every message-kind field. Render it as a
		// pointer and fall back to fmt.Sprintf, which invokes the message's
		// own generated String() via fmt's Stringer check.
		if f.Message != nil {
			field.GoIdent = f.Message.GoIdent
			field.Pointer = true
		} else {
			// Defensive fallback for a proto kind ClassifyField doesn't
			// recognize (e.g. a proto2 group) — practically unreachable for
			// proto3, but "any" always compiles.
			field.GoType = "any"
		}
		field.NeedsFmtFallback, field.ToStringExpr = true, fmtFallback
	}
	field.Repeated = f.Desc.IsList()
}

// fmtFallback is the generic stringification used for every field whose Go
// type isn't string/(singular) bool/(singular) enum: numeric, bytes,
// message, and any repeated field. fmt.Sprintf("%v", ...) compiles for any
// Go type, so a forced-in label is always generated rather than refused.
func fmtFallback(expr string) string { return "fmt.Sprintf(\"%v\", " + expr + ")" }

// metricFieldOpts returns the field's MetricFieldOptions, or a zero-value
// (never nil) one when the extension isn't present — so Label stays nil
// ("no override") rather than requiring callers to nil-check the options
// themselves, matching the accessor pattern the orm gorm target's
// telemetry.go uses for its own extension reads.
func metricFieldOpts(f *protogen.Field) *telemetrypbv1.MetricFieldOptions {
	if f.Desc.Options() == nil || !proto.HasExtension(f.Desc.Options(), telemetrypbv1.E_MetricField) {
		return &telemetrypbv1.MetricFieldOptions{}
	}
	opts, _ := proto.GetExtension(f.Desc.Options(), telemetrypbv1.E_MetricField).(*telemetrypbv1.MetricFieldOptions)
	if opts == nil {
		return &telemetrypbv1.MetricFieldOptions{}
	}
	return opts
}
