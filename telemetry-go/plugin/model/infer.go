package model

import (
	"strings"

	"github.com/the-protobuf-project/protokit/schema"
)

// LabelDecision is the resolved outcome of InferLabel for one field.
type LabelDecision struct {
	// IsLabel reports whether the field should be projected in as a metric
	// label.
	IsLabel bool

	// Warn reports whether an explicit override forced a field in whose type
	// makes it a questionable label choice (non-bool/enum, or a
	// google.api.resource_reference field, regardless of type). Only ever
	// true when IsLabel is also true — a field that ended up excluded has
	// nothing to warn about.
	Warn bool
}

// InferLabel implements telemetry.v1's metric-label inference rule for a
// single field:
//
//  1. An explicit MetricFieldOptions.Label override always wins: override
//     tells the caller which way (true forces the field in, false forces it
//     out), and override is nil when the extension is absent or its Label is
//     unset, meaning "no override, fall through to inference".
//  2. Absent an override, a field is a label by default only when it is a
//     bool or enum kind AND does not carry google.api.resource_reference —
//     resource_reference marks a field as conceptually an identifier
//     regardless of its wire type, and identifiers are never inferred as
//     labels (label cardinality must stay bounded). Every other kind
//     (string, numeric, bytes, message) defaults to not-a-label.
//
// isResourceRef must reflect whether the field carries a non-empty
// google.api.resource_reference, independent of fieldType — a resource
// reference is excluded from default inference even on the rare occasion a
// resource-typed field happens to classify as TypeBool or TypeEnum.
//
// InferLabel is pure (no protogen/protoreflect dependency) so the rule can be
// exercised directly in unit tests without constructing proto descriptors.
func InferLabel(override *bool, fieldType schema.FieldType, isResourceRef bool) LabelDecision {
	isBoolOrEnum := fieldType == schema.TypeBool || fieldType == schema.TypeEnum

	if override != nil {
		if !*override {
			return LabelDecision{IsLabel: false}
		}
		return LabelDecision{IsLabel: true, Warn: !isBoolOrEnum || isResourceRef}
	}

	if isResourceRef {
		return LabelDecision{IsLabel: false}
	}
	return LabelDecision{IsLabel: isBoolOrEnum}
}

// durationLikeSubstrings are the lowercase field-name fragments InferKind
// treats as a signal that a numeric field is a measured distribution
// (duration, latency, a size in bytes) rather than a point-in-time reading —
// i.e. histogram-worthy rather than gauge-worthy. Matched with strings.Contains
// against the field's snake_case proto name, so "request_duration_ms",
// "p99_latency", and "payload_size_bytes" all match.
var durationLikeSubstrings = []string{
	"duration", "latency", "elapsed", "_ms", "_seconds", "_sec", "bytes", "size",
}

// IsDurationLike reports whether fieldName (a proto field's snake_case name)
// matches InferKind's histogram-worthy heuristic. Exported so a Target or test
// can explain *why* a field auto-detected the way it did, without duplicating
// the substring list.
func IsDurationLike(fieldName string) bool {
	lower := strings.ToLower(fieldName)
	for _, sub := range durationLikeSubstrings {
		if strings.Contains(lower, sub) {
			return true
		}
	}
	return false
}

// InferKind resolves the instrument kind for a value-metric field (one with
// MetricFieldOptions.value = true): an explicit override (anything but
// KindUnspecified) always wins outright. Absent one, the field's name is
// checked against IsDurationLike — a match resolves to KindHistogram,
// otherwise KindGauge. KindCounter and KindUpDownCounter are never
// auto-detected: a single scalar field can't safely be assumed monotonic from
// its type or name alone, so those two kinds are only reachable via an
// explicit override.
func InferKind(override MetricKind, fieldName string) MetricKind {
	if override != KindUnspecified {
		return override
	}
	if IsDurationLike(fieldName) {
		return KindHistogram
	}
	return KindGauge
}
