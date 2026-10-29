package telemetry

import (
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// newPrometheusRegistry returns a registry of the pipeline's own — never
// prometheus.DefaultRegisterer, so two pipelines in one process (tests, above
// all) do not collide — carrying the Go runtime and process collectors a
// Prometheus user expects beside the service's metrics, and the handler that
// serves it.
func newPrometheusRegistry() (*prometheus.Registry, http.Handler, error) {
	registry := prometheus.NewRegistry()
	if err := registry.Register(collectors.NewGoCollector()); err != nil {
		return nil, nil, fmt.Errorf("register Go collector: %w", err)
	}
	if err := registry.Register(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{})); err != nil {
		return nil, nil, fmt.Errorf("register process collector: %w", err)
	}
	return registry, promhttp.HandlerFor(registry, promhttp.HandlerOpts{Registry: registry}), nil
}
