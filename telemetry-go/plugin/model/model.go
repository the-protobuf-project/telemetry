// Package model is protoc-gen-telemetry's intermediate representation: a
// Source (see plugin/source) walks the proto descriptors handed to the
// plugin and builds a Model; a Target (see plugin/target/gengo) renders a
// Model into Go source. Keeping the IR here, independent of both protogen
// descriptor-walking and Go-source rendering, is what lets each side stay
// small and lets infer.go's label-inference rule be unit tested in
// isolation (see infer_test.go).
package model

import "google.golang.org/protobuf/compiler/protogen"

// Model is the root IR: one Resource per proto message that carries
// (telemetry.v1.telemetry) and resolves to metrics being on (see
// plugin/source's telemetryOpts/resolveTelemetry).
type Model struct {
	Resources []*Resource
}

// Resource is one telemetry-annotated message: it renders to a single
// <Name>Metrics Go type carrying an inferred create-op counter and a
// write-latency histogram, with Fields projected in as metric labels.
type Resource struct {
	// Name is the message's Go type name (e.g. "Book"), used to derive the
	// generated <Name>Metrics type name and the metric names themselves.
	Name string

	// GoIdent is the message's own Go identifier. protoc-gen-telemetry never
	// needs to reference the message type itself in generated code (only its
	// fields' types), but callers of the IR (tests, future targets) may want
	// it, so it's carried through.
	GoIdent protogen.GoIdent

	// SourceFile is the proto file the message was declared in. The gengo
	// target groups Resources by SourceFile so it emits exactly one
	// companion Go file per input proto file, in the same Go package as the
	// original message (matching the convention protoc-gen-go-grpc and
	// similar companion generators use).
	SourceFile *protogen.File

	// MetricsEnabled is always true for a Resource that made it into the
	// Model — plugin/source only appends a Resource once it has resolved
	// both TelemetryOptions.Enabled and TelemetryOptions.Metrics to "on". A
	// message that opts out of either is simply never added, so this field
	// is a documentation aid rather than something a Target needs to branch
	// on; it is kept on the IR because a caller may hold a Resource without
	// having gone through Source (e.g. a test building one by hand).
	MetricsEnabled bool

	// Fields are the fields resolved as metric labels, in proto field order.
	Fields []*Field

	// ValueMetrics are the numeric fields opted in (via
	// MetricFieldOptions.value) as their own recorded metric — a dedicated
	// instrument per field, in addition to the resource's fixed create-op
	// counter and write-latency histogram. Also in proto field order.
	ValueMetrics []*ValueMetric
}

// Field is one proto field projected into the generated metrics as a label:
// an extra parameter on IncCreated/RecordLatency, and an entry in the
// telemetry.Labels map passed to the underlying instrument.
type Field struct {
	// Name is the Go parameter name for this field (lowerCamel, e.g.
	// "genre", "authorID").
	Name string

	// Column is the field's proto name, unmodified (e.g. "genre",
	// "author_id").
	Column string

	// LabelKey is the string key this field is recorded under in the
	// telemetry.Labels map: MetricFieldOptions.LabelKey when set, else
	// Column's snake_case form.
	LabelKey string

	// GoType is the field's Go type as literal source text, already fully
	// formed (including a leading "[]" for a repeated field and a leading
	// "*" for a message type) for every field EXCEPT one whose type must be
	// import-qualified relative to the file being generated (an enum or
	// message type) — for those, GoType is left empty and the target
	// resolves the type from GoIdent instead (see Pointer/Repeated below),
	// because only the target, at render time, knows which Go package it is
	// currently writing into and can correctly decide whether the type needs
	// a package qualifier and an import.
	GoType string

	// GoIdent is set only for enum- and message-typed fields (zero value
	// otherwise, i.e. GoImportPath == ""). The target calls
	// protogen.GeneratedFile.QualifiedGoIdent on it so a field whose enum or
	// message type lives in a different Go package than the file being
	// generated gets both a correct package-qualified reference and the
	// matching import — the same mechanism protoc-gen-go itself uses for
	// cross-file type references.
	GoIdent protogen.GoIdent

	// Pointer is true when GoIdent names a message type that must be
	// rendered as a pointer (*Type). Always false for enum fields (proto
	// enums are plain values, never pointers) and for non-GoIdent fields.
	Pointer bool

	// Repeated is true for a repeated (list, non-map) field. The target
	// wraps whichever type GoType/GoIdent resolves to in a "[]".
	Repeated bool

	// NeedsStrconv/NeedsFmtFallback tell the target which stdlib import (if
	// any) ToStringExpr's rendered text depends on, so the generated file's
	// imports exactly match what its body actually uses — matching the
	// requirement that e.g. "strconv" is only imported when a bool label is
	// actually emitted. Both are false for a string or (singular) enum
	// field, whose ToStringExpr needs neither. At most one is ever true.
	NeedsStrconv     bool
	NeedsFmtFallback bool

	// ToStringExpr renders the Go expression that converts a value already
	// bound to paramExpr (the field's parameter name) into the string stored
	// in the telemetry.Labels map: a string field passes paramExpr through
	// unchanged, a bool field wraps it in strconv.FormatBool, a (singular)
	// enum field calls its generated String() method, and everything else
	// (numeric, bytes, message, and any repeated field regardless of
	// element kind) falls back to fmt.Sprintf("%v", paramExpr) so a forced
	// label of any type still compiles instead of being refused.
	ToStringExpr func(paramExpr string) string
}

// MetricKind mirrors telemetrypbv1.MetricKind without the plugin's Go-source
// packages (model, infer.go, gengo) needing to import the generated proto
// package — only source.go, which already imports telemetrypbv1 to read the
// annotation, translates between the two.
type MetricKind int

const (
	// KindUnspecified means "auto-detect" — InferKind resolves it to
	// KindGauge or KindHistogram, never Kind(UpDown)Counter. Never itself the
	// final resolved kind a Target renders.
	KindUnspecified MetricKind = iota
	KindCounter
	KindUpDownCounter
	KindGauge
	KindHistogram
)

// String renders both the runtime-go/telemetry Meter constructor method
// (Meter.Counter / .UpDownCounter / .Gauge / .Histogram) and the instrument
// Go type name this kind maps to — the interface is designed so those two
// names always match — for gengo to splice directly into generated source.
func (k MetricKind) String() string {
	switch k {
	case KindCounter:
		return "Counter"
	case KindUpDownCounter:
		return "UpDownCounter"
	case KindGauge:
		return "Gauge"
	case KindHistogram:
		return "Histogram"
	default:
		return "Gauge" // KindUnspecified never reaches a Target; Gauge is InferKind's own default.
	}
}

// Verb renders the runtime-go/telemetry instrument method this kind's
// recorder method delegates to (Counter/UpDownCounter.Add, Gauge.Set,
// Histogram.Record) — also used as the generated recorder method's name
// prefix ("Add<Field>"/"Set<Field>"/"Record<Field>"), so the generated API
// reads as a direct mirror of the underlying instrument.
func (k MetricKind) Verb() string {
	switch k {
	case KindCounter, KindUpDownCounter:
		return "Add"
	case KindGauge:
		return "Set"
	case KindHistogram:
		return "Record"
	default:
		return "Set"
	}
}

// ValueMetric is one numeric field recorded as its own metric (a value, not
// a label) — a dedicated instrument on the <Name>Metrics struct, alongside
// the resource's fixed create-op counter and write-latency histogram.
type ValueMetric struct {
	// FieldName is the Go parameter/receiver-field name (lowerCamel, e.g.
	// "pageCount").
	FieldName string

	// Column is the field's proto name, unmodified (e.g. "page_count").
	Column string

	// MetricName is the generated instrument's name (e.g.
	// "book_page_count"): MetricFieldOptions.metric when set, else
	// "<resource_snake>_<field_snake>".
	MetricName string

	// Kind is the resolved instrument kind (never KindUnspecified — Source
	// resolves it via InferKind before building the ValueMetric).
	Kind MetricKind

	// GoType is the field's Go type as literal source text ("int32",
	// "uint64", "float32", "float64", ...) — always a plain numeric scalar;
	// Source rejects (with a warning) any field type MetricKind can't apply
	// to, so a Target never has to handle a non-numeric ValueMetric.
	GoType string

	// ToFloat64Expr renders the Go expression that converts a value already
	// bound to paramExpr into the float64 every runtime-go/telemetry
	// instrument method (Add/Set/Record) takes.
	ToFloat64Expr func(paramExpr string) string

	// Unit is the instrument's unit (UCUM-style: "1", "ms", "By", ...):
	// MetricFieldOptions.unit when set, else "1" (or "ms" when Kind was
	// auto-detected as KindHistogram from a duration-like field name).
	Unit string

	// Buckets are explicit histogram bucket boundaries (only meaningful when
	// Kind == KindHistogram); nil uses runtime-go/telemetry's own default.
	Buckets []float64
}
