// Package gengo implements protoc-gen-telemetry's factory.Target: it renders
// the plugin/model IR into one companion Go file per input proto file (the
// same convention protoc-gen-go-grpc and similar codegen use), each
// containing every in-scope message's <Name>Metrics type — an inferred
// create-op counter and a write-latency histogram, with metric labels
// projected from fields per model.Field.
package gengo

import (
	"fmt"
	"strconv"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"

	"github.com/the-protobuf-project/protokit/factory"
	"github.com/the-protobuf-project/protokit/header"
	"github.com/the-protobuf-project/protokit/naming"

	"github.com/the-protobuf-project/telemetry/telemetry-go/plugin/model"
)

// telemetryModule is the import path of the first-party, backend-agnostic
// Meter contract every generated file goes through. The plugin itself never
// imports it — only generated consumers do.
const telemetryModule = "github.com/the-protobuf-project/runtime-go/telemetry"

// Standard-library and runtime import identifiers used across every
// generated file. Passing one of these to GeneratedFile.P (or resolving it
// through QualifiedGoIdent) both prints the correctly-qualified reference
// and registers the import, the same mechanism protoc-gen-go itself uses
// for its own generated code (see google.golang.org/protobuf/cmd/
// protoc-gen-go/internal_gengo, e.g. mathPackage/sortPackage).
var (
	contextPackage   = protogen.GoImportPath("context")
	strconvPackage   = protogen.GoImportPath("strconv")
	fmtPackage       = protogen.GoImportPath("fmt")
	telemetryPackage = protogen.GoImportPath(telemetryModule)
)

// Target implements factory.Target[*model.Model].
type Target struct {
	// PluginVersion is stamped into every generated file's banner.
	PluginVersion string
}

// New returns a gengo Target that stamps pluginVersion into generated-file
// banners.
func New(pluginVersion string) *Target { return &Target{PluginVersion: pluginVersion} }

// Name identifies this target in a factory.Registry.
func (t *Target) Name() string { return "go" }

// Languages lists the languages this target can emit.
func (t *Target) Languages() []string { return []string{"go"} }

// Generate renders m into one companion Go file per proto file that
// contributed at least one Resource.
func (t *Target) Generate(ctx factory.Ctx, m *model.Model, lang string) error {
	if ctx.Plugin == nil {
		return fmt.Errorf("gengo target requires a protoc plugin context (only available in a buf/protoc run)")
	}
	if lang != "go" {
		return fmt.Errorf("gengo target only supports language %q, got %q", "go", lang)
	}

	// Group resources by their originating file, preserving first-seen file
	// order so output is deterministic across a run.
	var order []*protogen.File
	byFile := map[*protogen.File][]*model.Resource{}
	for _, res := range m.Resources {
		if _, ok := byFile[res.SourceFile]; !ok {
			order = append(order, res.SourceFile)
		}
		byFile[res.SourceFile] = append(byFile[res.SourceFile], res)
	}

	protocVersion := formatProtocVersion(ctx.Plugin)
	for _, file := range order {
		t.generateFile(ctx.Plugin, file, byFile[file], protocVersion)
	}
	return nil
}

// generateFile renders one companion file (<file>_telemetry.pb.go, in the
// same Go package as file) containing every one of resources' Resources.
func (t *Target) generateFile(p *protogen.Plugin, file *protogen.File, resources []*model.Resource, protocVersion string) {
	filename := file.GeneratedFilenamePrefix + "_telemetry.pb.go"
	g := p.NewGeneratedFile(filename, file.GoImportPath)

	g.P(header.Render("//", header.Info{
		PluginVersion: t.PluginVersion,
		ProtocVersion: protocVersion,
		Source:        file.Desc.Path(),
		SchemaLabel:   "package",
		Schema:        string(file.GoPackageName),
		Notes: []string{
			"Inferred lifecycle metrics (create-op counter + write-latency",
			"histogram) from telemetry.v1 annotations. Spans are out of scope",
			"for this generator — see telemetry.v1's TelemetryFieldOptions doc.",
		},
	}))
	g.P()
	g.P("package ", file.GoPackageName)
	g.P()

	// "context" and the telemetry runtime package get registered as imports
	// naturally below, since every resource references context.Context and
	// telemetry.{Meter,Counter,Histogram,WithUnit,WithBuckets} via GoIdent
	// (see writeResource). "strconv"/"fmt", by contrast, only ever appear as
	// plain text embedded inside a field's ToStringExpr output, so they need
	// registering explicitly here — and only when actually used, matching
	// the spec's "only import strconv if you actually emit a bool label".
	needsStrconv, needsFmt := false, false
	for _, res := range resources {
		for _, f := range res.Fields {
			needsStrconv = needsStrconv || f.NeedsStrconv
			needsFmt = needsFmt || f.NeedsFmtFallback
		}
	}
	if needsStrconv {
		g.QualifiedGoIdent(strconvPackage.Ident("FormatBool"))
	}
	if needsFmt {
		g.QualifiedGoIdent(fmtPackage.Ident("Sprintf"))
	}

	for _, res := range resources {
		writeResource(g, res)
	}
}

// writeResource renders one Resource's <Name>Metrics type: the struct, its
// constructor, IncCreated/RecordLatency (each taking one extra parameter per
// label field, generically for 0..N fields of any inferred type), and one
// Add/Set/Record<Field> recorder method per ValueMetric.
func writeResource(g *protogen.GeneratedFile, res *model.Resource) {
	typeName := res.Name + "Metrics"
	counterName := naming.SnakePlural(res.Name) + "_created_total"
	histogramName := naming.SnakeCase(res.Name) + "_write_latency_ms"

	g.P("type ", typeName, " struct {")
	g.P("created ", telemetryPackage.Ident("Counter"))
	g.P("latency ", telemetryPackage.Ident("Histogram"))
	for _, vm := range res.ValueMetrics {
		g.P(vm.FieldName, " ", telemetryPackage.Ident(vm.Kind.String()))
	}
	g.P("}")
	g.P()

	g.P("func New", typeName, "(m ", telemetryPackage.Ident("Meter"), ") *", typeName, " {")
	g.P("return &", typeName, "{")
	g.P("created: m.Counter(", strconv.Quote(counterName), ", ", telemetryPackage.Ident("WithUnit"), "(", strconv.Quote("1"), ")),")
	g.P("latency: m.Histogram(", strconv.Quote(histogramName), ", ", telemetryPackage.Ident("WithUnit"), "(", strconv.Quote("ms"), "),")
	g.P(telemetryPackage.Ident("WithBuckets"), "(5, 10, 25, 50, 100, 250)),")
	for _, vm := range res.ValueMetrics {
		args := []any{
			vm.FieldName, ": m.", vm.Kind.String(), "(", strconv.Quote(vm.MetricName), ", ",
			telemetryPackage.Ident("WithUnit"), "(", strconv.Quote(vm.Unit), ")",
		}
		if vm.Kind == model.KindHistogram && len(vm.Buckets) > 0 {
			args = append(args, ", ", telemetryPackage.Ident("WithBuckets"), "(", bucketsLiteral(vm.Buckets), ")")
		}
		args = append(args, "),")
		g.P(args...)
	}
	g.P("}")
	g.P("}")
	g.P()

	params := paramsExpr(g, res.Fields)
	labels := labelsExpr(res.Fields)

	g.P("func (m *", typeName, ") IncCreated(ctx ", contextPackage.Ident("Context"), params, ") {")
	g.P("m.created.Add(ctx, 1, ", labels, ")")
	g.P("}")
	g.P()

	g.P("func (m *", typeName, ") RecordLatency(ctx ", contextPackage.Ident("Context"), ", ms float64", params, ") {")
	g.P("m.latency.Record(ctx, ms, ", labels, ")")
	g.P("}")
	g.P()

	for _, vm := range res.ValueMetrics {
		writeValueMetric(g, typeName, vm, params, labels)
	}
}

// writeValueMetric renders one ValueMetric's recorder method: Add<Field> for
// a Counter/UpDownCounter, Set<Field> for a Gauge, Record<Field> for a
// Histogram — mirroring the underlying instrument's own method name — taking
// the field's own value plus the resource's usual label parameters, so a
// value metric can be sliced by the same dimensions as the lifecycle ones.
func writeValueMetric(g *protogen.GeneratedFile, typeName string, vm *model.ValueMetric, params, labels string) {
	methodName := vm.Kind.Verb() + naming.PascalGo(vm.FieldName)
	instrumentMethod := vm.Kind.Verb()

	g.P("func (m *", typeName, ") ", methodName, "(ctx ", contextPackage.Ident("Context"), ", ", vm.FieldName, " ", vm.GoType, params, ") {")
	g.P("m.", vm.FieldName, ".", instrumentMethod, "(ctx, ", vm.ToFloat64Expr(vm.FieldName), ", ", labels, ")")
	g.P("}")
	g.P()
}

// bucketsLiteral renders buckets as a bare comma-separated Go float literal
// list ("5, 10, 25, 50") — just the arguments, not the surrounding
// WithBuckets(...) call, so the caller can wrap it with a properly
// import-registered telemetryPackage.Ident("WithBuckets") instead of a
// hardcoded package-qualifier string.
func bucketsLiteral(buckets []float64) string {
	var b strings.Builder
	for i, bound := range buckets {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(strconv.FormatFloat(bound, 'g', -1, 64))
	}
	return b.String()
}

// paramsExpr renders the ", name Type" suffix appended to IncCreated's and
// RecordLatency's signatures for each label field — empty when fields is
// empty, so a resource with zero labels gets a plain (ctx context.Context).
func paramsExpr(g *protogen.GeneratedFile, fields []*model.Field) string {
	var b strings.Builder
	for _, f := range fields {
		b.WriteString(", ")
		b.WriteString(f.Name)
		b.WriteString(" ")
		b.WriteString(paramType(g, f))
	}
	return b.String()
}

// paramType resolves a field's Go type as it should appear in a function
// signature in the file g is rendering: GoType verbatim for a plain scalar,
// or GoIdent run through QualifiedGoIdent (which both correctly
// package-qualifies a cross-package enum/message type and registers the
// import it needs) wrapped in "*"/"[]" per Pointer/Repeated.
func paramType(g *protogen.GeneratedFile, f *model.Field) string {
	if f.GoIdent.GoImportPath == "" {
		return f.GoType
	}
	base := g.QualifiedGoIdent(f.GoIdent)
	if f.Pointer {
		base = "*" + base
	}
	if f.Repeated {
		base = "[]" + base
	}
	return base
}

// labelsExpr renders the telemetry.Labels argument passed to Add/Record:
// "nil" for a resource with no label fields (per spec, so callers don't pay
// for an empty map literal), else a telemetry.Labels{...} literal with one
// entry per field.
func labelsExpr(fields []*model.Field) string {
	if len(fields) == 0 {
		return "nil"
	}
	var b strings.Builder
	b.WriteString("telemetry.Labels{")
	for i, f := range fields {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(strconv.Quote(f.LabelKey))
		b.WriteString(": ")
		b.WriteString(f.ToStringExpr(f.Name))
	}
	b.WriteString("}")
	return b.String()
}

// formatProtocVersion formats the compiler version off the plugin's
// CodeGeneratorRequest the way protoc-gen-go does ("v<major>.<minor>.<patch>
// [-suffix]"), or "" when none was supplied (e.g. an in-process test
// harness) — header.Render substitutes "(unknown)" for an empty string.
func formatProtocVersion(p *protogen.Plugin) string {
	v := p.Request.GetCompilerVersion()
	if v == nil {
		return ""
	}
	suffix := ""
	if s := v.GetSuffix(); s != "" {
		suffix = "-" + s
	}
	return fmt.Sprintf("v%d.%d.%d%s", v.GetMajor(), v.GetMinor(), v.GetPatch(), suffix)
}
