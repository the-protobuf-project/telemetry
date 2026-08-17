package main

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/the-protobuf-project/telemetry/telemetry-go"
)

// LLMMetrics demonstrates automatic metric recording with struct tags
type LLMMetrics struct {
	TokensProcessed int64   `telemetry:"metric:counter:llm.tokens.processed"`
	ResponseTime    float64 `telemetry:"metric:histogram:llm.response.time"`
	ActiveRequests  int64   `telemetry:"metric:gauge:llm.requests.active"`
	CacheHitRate    float64 `telemetry:"metric:gauge:llm.cache.hit_rate"`
}

// TranscriptionMetrics for speech-to-text
type TranscriptionMetrics struct {
	AudioDuration float64 `telemetry:"metric:histogram:transcription.audio.duration"`
	Confidence    float64 `telemetry:"metric:gauge:transcription.confidence"`
	WordCount     int64   `telemetry:"metric:counter:transcription.words.count"`
}

// main initializes telemetry, records simulated LLM and transcription metrics, and reports the output locations for visualization.
func main() {
	// Uses telemetry.toml config for service info and OTLP endpoint
	p, err := telemetry.New().Build()
	if err != nil {
		panic(err)
	}
	defer func() {
		if err := p.Close(); err != nil {
			fmt.Printf("Error closing telemetry: %v\n", err)
		}
	}()

	p.Logger.Info("Metrics Example Started")
	p.Logger.Info("Metrics will be written to OTLP and MCAP")

	// Simulate LLM processing with metrics (run for 2 minutes to generate rate data)
	for i := 0; i < 120; i++ {
		// LLM request metrics
		llmMetrics := LLMMetrics{
			TokensProcessed: int64(rand.Intn(500) + 100),
			ResponseTime:    rand.Float64()*2000 + 500, // 500-2500ms
			ActiveRequests:  int64(rand.Intn(10) + 1),
			CacheHitRate:    rand.Float64(),
		}

		// Record metrics automatically from struct tags with per-metric attributes
		if err := p.Metrics.Record(llmMetrics,
			telemetry.WithAttributes(
				telemetry.StringAttribute("model", "gpt-4"),
				telemetry.StringAttribute("user_type", "premium"),
			),
		); err != nil {
			if err := p.Logger.Error("Failed to record LLM metrics", map[string]interface{}{"error": err.Error()}); err != nil {
				return
			}
		}

		p.Logger.Info("LLM request processed", map[string]interface{}{
			"tokens":        llmMetrics.TokensProcessed,
			"response_time": llmMetrics.ResponseTime,
		})

		// Transcription metrics every 3rd iteration
		if i%3 == 0 {
			transMetrics := TranscriptionMetrics{
				AudioDuration: rand.Float64()*10 + 1, // 1-11 seconds
				Confidence:    0.8 + rand.Float64()*0.2,
				WordCount:     int64(rand.Intn(100) + 20),
			}

			if err := p.Metrics.Record(transMetrics,
				telemetry.WithAttributes(
					telemetry.StringAttribute("language", "en"),
					telemetry.StringAttribute("audio_format", "wav"),
				),
			); err != nil {
				if err := p.Logger.Error("Failed to record transcription metrics", map[string]interface{}{"error": err.Error()}); err != nil {
					return
				}
			}

			p.Logger.Info("Audio transcribed", map[string]interface{}{
				"duration":   transMetrics.AudioDuration,
				"confidence": transMetrics.Confidence,
			})
		}

		time.Sleep(300 * time.Millisecond)
	}

	p.Logger.Info("Metrics example completed!")
	p.Logger.Info("Check:")
	p.Logger.Info("1. MCAP file: examples/metrics/metrics-example.mcap")
	p.Logger.Info("2. Open in Foxglove Studio")
	p.Logger.Info("3. Use Gauge/Indicator/Plot panels to visualize")
}
