use telemetry::derive::Metrics;
use telemetry::{Environment, Telemetry, logger};

#[derive(Debug, Metrics)]
pub struct LlmMetrics {
    #[metric(
        name = "llm.requests.total",
        description = "Total number of LLM requests",
        counter
    )]
    pub request_count: u64,

    #[metric(
        name = "llm.response.latency_ms",
        description = "LLM response latency in milliseconds",
        histogram
    )]
    pub latency_ms: f64,

    #[metric(
        name = "llm.cache.hit_rate",
        description = "LLM cache hit rate percentage",
        gauge
    )]
    pub cache_hit_rate: f64,
}

/// Runs the metrics example, recording sample LLM and API telemetry.
///
/// # Examples
///
/// Run the example binary to start recording metrics:
///
/// ```text
/// cargo run --example metrics
/// ```
///
/// # Errors
///
/// Returns an error if telemetry initialization or metric recording fails.
#[tokio::main]
async fn main() -> anyhow::Result<()> {
    // Auto-discovers telemetry.toml config file
    let mut telemetry = Telemetry::new()
        .with_service("metrics-example", "1.0.0")
        .description("Metrics example service")
        .environment(Environment::Development)
        .with_mcap("telemetry/examples/metrics.mcap")
        .build()?;

    logger::info!("Metrics Example Started");
    logger::info!("Sending metrics to OTEL collector at localhost:4317");

    // Record metrics using the macro-generated struct
    let llm_metrics = LlmMetrics {
        request_count: 42,
        latency_ms: 123.5,
        cache_hit_rate: 0.85,
    };

    telemetry.metrics.record(&llm_metrics)?;
    logger::info!("Recorded LLM metrics from struct");

    // Or record metrics directly
    logger::info!("Recording metrics for 30 seconds...");

    for _iteration in 0..30 {
        for i in 0..10 {
            telemetry.metrics.counter("api.requests", 1.0)?;
            telemetry
                .metrics
                .histogram("api.latency_ms", (i as f64) * 10.0 + 50.0)?;
            telemetry
                .metrics
                .gauge("api.active_connections", (10 - i) as f64)?;

            // Re-record LLM metrics every iteration to keep them visible
            telemetry.metrics.record(&llm_metrics)?;

            logger::debug!("Recorded API metrics");
            tokio::time::sleep(tokio::time::Duration::from_millis(100)).await;
        }
    }

    logger::info!("Metrics recording completed");
    logger::info!("MCAP file will be finalized automatically");

    Ok(())
}
