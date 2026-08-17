package model

import (
	"testing"

	"github.com/the-protobuf-project/protokit/schema"
)

func boolPtr(b bool) *bool { return &b }

func TestInferLabel(t *testing.T) {
	tests := []struct {
		name          string
		override      *bool
		fieldType     schema.FieldType
		isResourceRef bool
		wantIsLabel   bool
		wantWarn      bool
	}{
		// No override: inference rule.
		{"bool infers in", nil, schema.TypeBool, false, true, false},
		{"enum infers in", nil, schema.TypeEnum, false, true, false},
		{"string infers out", nil, schema.TypeString, false, false, false},
		{"numeric infers out", nil, schema.TypeInt32, false, false, false},
		{"bytes infers out", nil, schema.TypeBytes, false, false, false},
		{"message infers out", nil, schema.TypeJSON, false, false, false},
		{"bool resource_reference infers out", nil, schema.TypeBool, true, false, false},
		{"enum resource_reference infers out", nil, schema.TypeEnum, true, false, false},
		{"string resource_reference stays out", nil, schema.TypeString, true, false, false},

		// Explicit override wins outright, both directions.
		{"override true on bool: in, no warn", boolPtr(true), schema.TypeBool, false, true, false},
		{"override true on enum: in, no warn", boolPtr(true), schema.TypeEnum, false, true, false},
		{"override true on string: in, warn", boolPtr(true), schema.TypeString, false, true, true},
		{"override true on numeric: in, warn", boolPtr(true), schema.TypeInt64, false, true, true},
		{"override true on bytes: in, warn", boolPtr(true), schema.TypeBytes, false, true, true},
		{"override true on message: in, warn", boolPtr(true), schema.TypeJSON, false, true, true},
		{"override true on bool resource_reference: in, warn", boolPtr(true), schema.TypeBool, true, true, true},
		{"override true on enum resource_reference: in, warn", boolPtr(true), schema.TypeEnum, true, true, true},
		{"override false on bool: out", boolPtr(false), schema.TypeBool, false, false, false},
		{"override false on enum: out", boolPtr(false), schema.TypeEnum, false, false, false},
		{"override false on string: out", boolPtr(false), schema.TypeString, false, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InferLabel(tt.override, tt.fieldType, tt.isResourceRef)
			if got.IsLabel != tt.wantIsLabel || got.Warn != tt.wantWarn {
				t.Fatalf("InferLabel(%v, %v, %v) = %+v, want {IsLabel:%v Warn:%v}",
					tt.override, tt.fieldType, tt.isResourceRef, got, tt.wantIsLabel, tt.wantWarn)
			}
		})
	}
}

func TestIsDurationLike(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"duration_ms", true},
		{"request_duration_ms", true},
		{"p99_latency", true},
		{"payload_size_bytes", true},
		{"elapsed", true},
		{"timeout_seconds", true},
		{"retry_sec", true},
		{"page_count", false},
		{"priority", false},
		{"retry_count", false},
		{"is_active", false},
	}
	for _, tt := range tests {
		if got := IsDurationLike(tt.name); got != tt.want {
			t.Errorf("IsDurationLike(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestInferKind(t *testing.T) {
	tests := []struct {
		name      string
		override  MetricKind
		fieldName string
		want      MetricKind
	}{
		{"unspecified + plain name infers gauge", KindUnspecified, "page_count", KindGauge},
		{"unspecified + duration-like name infers histogram", KindUnspecified, "request_duration_ms", KindHistogram},
		{"unspecified + latency name infers histogram", KindUnspecified, "p99_latency", KindHistogram},
		{"unspecified + bytes name infers histogram", KindUnspecified, "payload_size_bytes", KindHistogram},
		{"explicit gauge wins over duration-like name", KindGauge, "request_duration_ms", KindGauge},
		{"explicit counter always wins (never auto-detected)", KindCounter, "page_count", KindCounter},
		{"explicit up-down-counter always wins (never auto-detected)", KindUpDownCounter, "active_count", KindUpDownCounter},
		{"explicit histogram wins on a plain name", KindHistogram, "page_count", KindHistogram},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := InferKind(tt.override, tt.fieldName); got != tt.want {
				t.Errorf("InferKind(%v, %q) = %v, want %v", tt.override, tt.fieldName, got, tt.want)
			}
		})
	}
}

func TestMetricKindStringAndVerb(t *testing.T) {
	tests := []struct {
		kind     MetricKind
		wantStr  string
		wantVerb string
	}{
		{KindCounter, "Counter", "Add"},
		{KindUpDownCounter, "UpDownCounter", "Add"},
		{KindGauge, "Gauge", "Set"},
		{KindHistogram, "Histogram", "Record"},
	}
	for _, tt := range tests {
		if got := tt.kind.String(); got != tt.wantStr {
			t.Errorf("%v.String() = %q, want %q", tt.kind, got, tt.wantStr)
		}
		if got := tt.kind.Verb(); got != tt.wantVerb {
			t.Errorf("%v.Verb() = %q, want %q", tt.kind, got, tt.wantVerb)
		}
	}
}
