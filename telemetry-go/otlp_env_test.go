package telemetry

import (
	"context"
	"io"
	"net"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// clearOTelEnv unsets every variable that decides OTLP, so a developer's
// shell cannot change the outcome.
func clearOTelEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"OTEL_EXPORTER_OTLP_ENABLED", "OTEL_EXPORTER_OTLP_HOST", "OTEL_EXPORTER_OTLP_PORT",
		"OTEL_SDK_DISABLED", "OTEL_METRICS_EXPORTER",
	} {
		t.Setenv(k, "")
	}
}

func TestOTLPEnablement(t *testing.T) {
	for _, c := range []struct {
		name     string
		env      map[string]string
		build    func(*Builder)
		enabled  bool
		endpoint string // host:port when enabled
	}{
		{name: "nothing configured is off"},
		{name: "enabled alone exports to localhost:4317",
			env: map[string]string{"OTEL_EXPORTER_OTLP_ENABLED": "true"}, enabled: true, endpoint: "localhost:4317"},
		{name: "a host turns it on",
			env: map[string]string{"OTEL_EXPORTER_OTLP_HOST": "collector"}, enabled: true, endpoint: "collector:4317"},
		{name: "WithOTLP turns it on",
			build: func(b *Builder) { b.WithOTLP("collector", 4317) }, enabled: true, endpoint: "collector:4317"},
		{name: "an endpoint turns it on",
			build: func(b *Builder) { b.telemetryOpts.Telemetry.OTLP.Endpoint = "collector:4317" }, enabled: true, endpoint: "collector:4317"},
		{name: "ENABLED=false wins over WithOTLP",
			env:   map[string]string{"OTEL_EXPORTER_OTLP_ENABLED": "false"},
			build: func(b *Builder) { b.WithOTLP("collector", 4317) }},
		{name: "ENABLED=false wins over an endpoint",
			env:   map[string]string{"OTEL_EXPORTER_OTLP_ENABLED": "false"},
			build: func(b *Builder) { b.telemetryOpts.Telemetry.OTLP.Endpoint = "collector:4317" }},
		{name: "SDK_DISABLED wins over WithOTLP",
			env:   map[string]string{"OTEL_SDK_DISABLED": "true"},
			build: func(b *Builder) { b.WithOTLP("collector", 4317) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			clearOTelEnv(t)
			for k, v := range c.env {
				t.Setenv(k, v)
			}
			b := New()
			if c.build != nil {
				c.build(b)
			}
			otlp := b.otlp()
			if otlp.Enabled != c.enabled {
				t.Fatalf("enabled = %v, want %v", otlp.Enabled, c.enabled)
			}
			if c.enabled {
				//nolint:staticcheck // Host is deprecated but still resolved
				got := otlp.Endpoint
				if got == "" {
					got = net.JoinHostPort(otlp.Host, strconv.Itoa(otlp.Port)) //nolint:staticcheck // Deprecated
				}
				if got != c.endpoint {
					t.Errorf("endpoint = %q, want %q", got, c.endpoint)
				}
			}
		})
	}
}

// Close is bounded: a collector that is not listening cannot hold it up.
func TestCloseIsBounded(t *testing.T) {
	clearOTelEnv(t)
	// A port nothing listens on.
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := lis.Addr().(*net.TCPAddr).Port
	_ = lis.Close()

	tel, err := New().WithService("close-test", "0.0.0").WithOTLP("127.0.0.1", port).
		WithShutdownTimeout(time.Second).Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	_, span := tel.Tracing.Start(context.Background(), "unexported")
	span.End()

	start := time.Now()
	_ = tel.Close()
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("Close took %s with a 1s shutdown timeout", took)
	}
}

func TestPrometheusHandler(t *testing.T) {
	clearOTelEnv(t)
	off, err := New().WithService("prom-off", "0.0.0").Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if off.MetricsHandler() != nil {
		t.Error("MetricsHandler is set without the Prometheus exporter")
	}
	_ = off.Close()

	for name, build := range map[string]func() (*Telemetry, error){
		"WithPrometheus": func() (*Telemetry, error) {
			return New().WithService("prom-on", "0.0.0").WithPrometheus().Build()
		},
		"OTEL_METRICS_EXPORTER": func() (*Telemetry, error) {
			t.Setenv("OTEL_METRICS_EXPORTER", "otlp,prometheus")
			return New().WithService("prom-env", "0.0.0").Build()
		},
	} {
		t.Run(name, func(t *testing.T) {
			tel, err := build()
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			t.Cleanup(func() { _ = tel.Close() })
			h := tel.MetricsHandler()
			if h == nil {
				t.Fatal("MetricsHandler is nil with the Prometheus exporter on")
			}
			counter, err := tel.telemetry.GetMetrics().Counter("orders_placed")
			if err != nil {
				t.Fatal(err)
			}
			counter.Add(context.Background(), 3)

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
			body, _ := io.ReadAll(rec.Body)
			for _, want := range []string{"orders_placed", "go_goroutines"} {
				if !strings.Contains(string(body), want) {
					t.Errorf("/metrics lacks %q:\n%.400s", want, body)
				}
			}
		})
	}

	t.Run("OTEL_SDK_DISABLED", func(t *testing.T) {
		t.Setenv("OTEL_SDK_DISABLED", "true")
		tel, err := New().WithService("prom-disabled", "0.0.0").WithPrometheus().Build()
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		t.Cleanup(func() { _ = tel.Close() })
		if tel.MetricsHandler() != nil {
			t.Error("OTEL_SDK_DISABLED left the Prometheus exporter on")
		}
	})
}
