package options

import "testing"

func TestEnvKey(t *testing.T) {
	for name, want := range map[string]string{
		"TELEMETRY_TELEMETRY_OTLP_HOST":         "telemetry.otlp.host",
		"TELEMETRY_TELEMETRY__OTLP__USE_HTTP":   "telemetry.otlp.use_http",
		"TELEMETRY_TELEMETRY__OTLP__AUTH_TOKEN": "telemetry.otlp.auth_token",
	} {
		if got := envKey(name); got != want {
			t.Errorf("envKey(%q) = %q, want %q", name, got, want)
		}
	}
}
