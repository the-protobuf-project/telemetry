package metrics

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/the-protobuf-project/telemetry/telemetry-go/internal/foxglove"
	"github.com/the-protobuf-project/telemetry/telemetry-go/options"
)

// MetricMcapWriter writes metrics to MCAP for Foxglove visualization
// with dynamic channel creation per metric name
type MetricMcapWriter struct {
	unifiedWriter *foxglove.UnifiedMcapWriter // Shared MCAP writer
	channels      map[string]uint16           // Map metric name to channel ID
	mu            sync.Mutex                  // Mutex for channel map
	serviceName   string
	metadata      map[string]string
}

// FoxgloveMetric represents a metric value for Foxglove panels
type FoxgloveMetric struct {
	Timestamp FoxgloveTimestamp `json:"timestamp"`
	Name      string            `json:"name"`
	Value     float64           `json:"value"`
}

// FoxgloveTimestamp represents a timestamp in Foxglove format
type FoxgloveTimestamp struct {
	Sec  uint32 `json:"sec"`
	Nsec uint32 `json:"nsec"`
}

// NewMetricMcapWriter creates a metric writer using the unified MCAP writer
func NewMetricMcapWriter(serviceOpts options.ServiceOptions, unifiedWriter *foxglove.UnifiedMcapWriter) (*MetricMcapWriter, error) {
	metadata := map[string]string{
		"service_name": serviceOpts.Name,
		"version":      serviceOpts.Version,
		"environment":  string(serviceOpts.Environment),
	}

	return &MetricMcapWriter{
		unifiedWriter: unifiedWriter,
		channels:      make(map[string]uint16),
		serviceName:   serviceOpts.Name,
		metadata:      metadata,
	}, nil
}

// WriteCounter writes a counter metric
func (m *MetricMcapWriter) WriteCounter(name string, value float64) error {
	return m.writeMetric(name, value)
}

// WriteHistogram writes a histogram metric
func (m *MetricMcapWriter) WriteHistogram(name string, value float64) error {
	return m.writeMetric(name, value)
}

// WriteGauge writes a gauge metric
func (m *MetricMcapWriter) WriteGauge(name string, value float64) error {
	return m.writeMetric(name, value)
}

// writeMetric writes a metric to MCAP with dynamic channel creation
func (m *MetricMcapWriter) writeMetric(name string, value float64) error {
	// Get or create channel for this metric
	channelID, err := m.getOrCreateChannel(name)
	if err != nil {
		return err
	}

	now := time.Now()
	metric := FoxgloveMetric{
		Timestamp: FoxgloveTimestamp{
			Sec:  uint32(now.Unix()),
			Nsec: uint32(now.Nanosecond()),
		},
		Name:  name,
		Value: value,
	}

	data, err := json.Marshal(metric)
	if err != nil {
		return fmt.Errorf("failed to marshal metric: %w", err)
	}

	nowNano := uint64(now.UnixNano())
	return m.unifiedWriter.WriteMessage(channelID, data, nowNano, nowNano)
}

// getOrCreateChannel gets existing channel ID or creates new channel for metric
func (m *MetricMcapWriter) getOrCreateChannel(metricName string) (uint16, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if channelID, exists := m.channels[metricName]; exists {
		return channelID, nil
	}

	// Convert metric name to topic: llm.cache.hit_rate -> /metrics/{service}/llm/cache/hit_rate
	topic := fmt.Sprintf("/metrics/%s/%s", m.serviceName, strings.ReplaceAll(metricName, ".", "/"))

	// Create channel metadata
	channelMetadata := make(map[string]string)
	for k, v := range m.metadata {
		channelMetadata[k] = v
	}
	channelMetadata["metric_name"] = metricName

	// Create channel in unified writer
	channelID, err := m.unifiedWriter.CreateMetricChannel(topic, channelMetadata)
	if err != nil {
		return 0, fmt.Errorf("failed to create channel for %s: %w", metricName, err)
	}

	m.channels[metricName] = channelID
	return channelID, nil
}

// Close is a no-op since the unified writer is managed at the Telemetry level
func (m *MetricMcapWriter) Close() error {
	return nil
}

// IsClosed returns whether the writer is closed
func (m *MetricMcapWriter) IsClosed() bool {
	return m.unifiedWriter.IsClosed()
}

// GetFilePath returns the path to the MCAP file
func (m *MetricMcapWriter) GetFilePath() string {
	return m.unifiedWriter.GetFilePath()
}
