"""
Metrics example demonstrating automatic metric recording with Pydantic models.

This example uses the new Telemetry.new() builder API and shows:
- LLM processing metrics (tokens, response time, active requests, cache hit rate)
- Transcription metrics (audio duration, confidence, word count)
- OTLP export and MCAP recording

Configuration is auto-discovered from telemetry.toml.

Run with:
    uv run python -m examples.metrics.simple_example
"""

import random
import time

import telemetry
from telemetry import MetricsBaseModel, Telemetry


# LLMMetrics demonstrates automatic metric recording with MetricsBaseModel
# No prefix needed - uses service name from telemetry.toml automatically
class LLMMetrics(MetricsBaseModel):
    """LLM processing metrics"""

    tokens_processed: int = telemetry.Counter(
        description="Total tokens processed by LLM"
    )
    response_time: float = telemetry.Histogram(
        description="LLM response time in milliseconds"
    )
    active_requests: int = telemetry.Gauge(description="Number of active LLM requests")
    cache_hit_rate: float = telemetry.Gauge(description="LLM cache hit rate (0.0-1.0)")


# TranscriptionMetrics for speech-to-text
# No prefix needed - uses service name from telemetry.toml automatically
class TranscriptionMetrics(MetricsBaseModel):
    """Speech-to-text transcription metrics"""

    audio_duration: float = telemetry.Histogram(description="Audio duration in seconds")
    confidence: float = telemetry.Gauge(
        description="Transcription confidence score (0.0-1.0)"
    )
    word_count: int = telemetry.Counter(description="Total words transcribed")


def main():
    # Uses telemetry.toml config for OTLP endpoint and service info
    """
    Run the telemetry metrics example and record simulated LLM and transcription metrics.
    """
    with Telemetry.new().build() as p:
        p.logger.info("Metrics Example Started")
        p.logger.info("Metrics will be written to OTLP and MCAP")

        # Simulate LLM processing with metrics (run for 2 minutes to generate rate data)
        for i in range(120):
            # LLM request metrics
            llm_metrics = LLMMetrics(
                tokens_processed=random.randint(100, 600),
                response_time=random.uniform(500, 2500),  # 500-2500ms
                active_requests=random.randint(1, 11),
                cache_hit_rate=random.random(),
            )

            # Record metrics automatically from field metadata
            p.metrics.record(llm_metrics)

            p.logger.info(
                "LLM request processed",
                {
                    "tokens": llm_metrics.tokens_processed,
                    "response_time": llm_metrics.response_time,
                },
            )

            # Transcription metrics every 3rd iteration
            if i % 3 == 0:
                trans_metrics = TranscriptionMetrics(
                    audio_duration=random.uniform(1, 11),  # 1-11 seconds
                    confidence=random.uniform(0.8, 1.0),
                    word_count=random.randint(20, 120),
                )

                p.metrics.record(trans_metrics)

                p.logger.info(
                    "Audio transcribed",
                    {
                        "duration": trans_metrics.audio_duration,
                        "confidence": trans_metrics.confidence,
                    },
                )

            time.sleep(0.3)  # 300ms

        p.logger.info("Metrics example completed!")
        p.logger.info("Check:")
        p.logger.info("1. MCAP file: metrics-data.mcap")
        p.logger.info("2. Open in Foxglove Studio")
        p.logger.info("3. Use Gauge/Indicator/Plot panels to visualize")


if __name__ == "__main__":
    main()
