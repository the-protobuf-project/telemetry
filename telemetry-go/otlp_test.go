package telemetry

import "testing"

// WithOTLP decides TLS by the same rule as a configured endpoint: a remote host
// gets it on the TLS collector ports only, so a plaintext collector reached by a
// host name works; WithOTLPSecure overrides either way.
func TestWithOTLPSecure(t *testing.T) {
	for _, c := range []struct {
		host   string
		port   int
		secure *bool
		want   bool
	}{
		{"localhost", 4317, nil, false},
		{"10.0.0.7", 4317, nil, false},
		{"otel-collector", 4317, nil, false},
		{"telemetry", 6009, nil, false},
		{"otel.example.com", 443, nil, true},
		{"otel.example.com", 4318, nil, true},
		{"otel-collector", 4317, ptr(true), true},
		{"otel.example.com", 443, ptr(false), false},
	} {
		b := New().WithOTLP(c.host, c.port)
		if c.secure != nil {
			b.WithOTLPSecure(*c.secure)
		}
		if got := b.otlp().Secure; got != c.want {
			t.Errorf("WithOTLP(%q, %d) secure=%v: TLS %v, want %v", c.host, c.port, c.secure, got, c.want)
		}
	}
}

func ptr[T any](v T) *T { return &v }
