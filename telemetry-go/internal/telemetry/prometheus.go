package telemetry

import (
	"fmt"
	"net/http"

	"go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// newPrometheusReader returns a metric reader that Prometheus scrapes, on a
// registry of its own (see newPrometheusRegistry), and the http.Handler
// serving it.
func newPrometheusReader() (sdkmetric.Reader, http.Handler, error) {
	registry, handler, err := newPrometheusRegistry()
	if err != nil {
		return nil, nil, err
	}
	exporter, err := prometheus.New(prometheus.WithRegisterer(registry))
	if err != nil {
		return nil, nil, fmt.Errorf("create Prometheus exporter: %w", err)
	}
	return exporter, handler, nil
}

// MetricsHandler serves the pipeline's metrics in the Prometheus text format,
// for the application to mount (conventionally at /metrics). It is nil unless
// the Prometheus exporter is on (options.MetricsTelemetryOptions.Prometheus).
func (t *Telemetry) MetricsHandler() http.Handler {
	return t.promHandler
}
