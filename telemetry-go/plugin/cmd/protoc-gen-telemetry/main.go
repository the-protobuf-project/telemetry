// Command protoc-gen-telemetry is a protoc plugin that reads proto
// descriptors annotated with telemetry.v1.* options and generates inferred
// lifecycle metrics (a create-op counter + a write-latency histogram, with
// metric labels) for every message that carries (telemetry.v1.telemetry).
// It intentionally does not generate tracing/spans — those only make sense
// woven into a specific store implementation, which is what the sibling orm
// repo's protoc-gen-orm gorm target already does with
// telemetry.v1.TelemetryFieldOptions.
//
// # Install
//
//	go install github.com/the-protobuf-project/telemetry/telemetry-go/plugin/cmd/protoc-gen-telemetry@latest
//
// # Usage via buf.gen.yaml
//
//	plugins:
//	  - local: protoc-gen-telemetry
//	    out: gen/go
package main

import (
	"fmt"
	"os"
	"runtime/debug"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/types/pluginpb"

	"github.com/the-protobuf-project/protokit/factory"
	"github.com/the-protobuf-project/protokit/header"

	"github.com/the-protobuf-project/telemetry/telemetry-go/plugin/model"
	"github.com/the-protobuf-project/telemetry/telemetry-go/plugin/source"
	"github.com/the-protobuf-project/telemetry/telemetry-go/plugin/target/gengo"
)

// version is the build version, injected at release time via
// -ldflags "-X main.version=...".
var version = "dev"

// modulePath identifies this plugin's own module, so resolveVersion can pull
// a version out of Go's build info when it wasn't set via ldflags (e.g. `go
// install ...@v0.1.2`, or when consumed as a dependency).
const modulePath = "github.com/the-protobuf-project/telemetry/telemetry-go"

// resolveVersion returns the build version to stamp into generated files. A
// release sets `version` via ldflags and wins outright. Otherwise it's
// recovered from the build info the Go toolchain embeds. Only genuine local
// builds (`go build`/`go run`, which report "(devel)") fall back to "dev".
func resolveVersion() string {
	if version != "dev" {
		return version
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return version
	}
	if v := bi.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	for _, dep := range bi.Deps {
		if dep.Path == modulePath && dep.Version != "" {
			return dep.Version
		}
	}
	return version
}

func main() {
	v := resolveVersion()

	// When invoked directly with -version (not by protoc), print and exit
	// before protogen tries to read a CodeGeneratorRequest from stdin.
	if len(os.Args) == 2 && (os.Args[1] == "-version" || os.Args[1] == "--version") {
		fmt.Printf("protoc-gen-telemetry %s\n", v)
		return
	}

	// Every generated file's banner names the tool that produced it.
	header.SetTool("protoc-gen-telemetry")

	reg := factory.NewRegistry[*model.Model]()
	reg.AddSource(source.New())
	reg.AddTarget(gengo.New(v))

	protogen.Options{}.Run(func(p *protogen.Plugin) error {
		// Proto3 `optional` is used by MetricFieldOptions.Label; declare
		// support so buf/protoc don't warn.
		p.SupportedFeatures = uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL)

		ctx := factory.Ctx{Plugin: p}

		src := reg.Sources["proto"]
		m, err := src.Build(ctx)
		if err != nil {
			return err
		}

		tgt, ok := reg.Targets["go"]
		if !ok {
			return fmt.Errorf("no %q target registered — valid targets: %s", "go", reg.TargetNames())
		}
		if err := tgt.Generate(ctx, m, "go"); err != nil {
			return err
		}

		// Generate-time warnings (e.g. a metric_field.label=true override on
		// a field whose type makes a poor label) are never fatal — surface
		// them to stderr after a successful run.
		if s, ok := src.(*source.Source); ok {
			for _, w := range s.Warnings {
				fmt.Fprintln(os.Stderr, "protoc-gen-telemetry: warning:", w)
			}
		}
		return nil
	})
}
