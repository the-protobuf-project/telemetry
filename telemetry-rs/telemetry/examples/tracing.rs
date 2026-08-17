// Spans: `#[instrument]` → one trace per root (Gantt). Logs: `telemetry::logger::info!` etc.
// Parent chain needs OTEL layer unfiltered — fixed in `init_tokio_tracing` (fmt-only filter).
use telemetry::tracing::instrument;
use telemetry::{Environment, Telemetry};

/// Each call runs **inside** the caller’s active span → same trace, child spans.
#[instrument]
fn connect_to_db() {
    telemetry::logger::info!("child span: connect_to_db");
    std::thread::sleep(std::time::Duration::from_millis(80));
}

#[instrument]
fn query_database() {
    telemetry::logger::info!("child span: query_database");
    std::thread::sleep(std::time::Duration::from_millis(80));
}

#[instrument]
fn process_data() {
    telemetry::logger::info!("child span: process_data");
    std::thread::sleep(std::time::Duration::from_millis(80));
}

#[instrument]
fn save_data() {
    telemetry::logger::info!("child span: save_data");
    std::thread::sleep(std::time::Duration::from_millis(80));
}

/// **Continue the trace:** one root span; inner `#[instrument]` fns become children.
#[instrument(name = "sync_pipeline")]
fn run_sync_pipeline() {
    connect_to_db();
    query_database();
    process_data();
    save_data();
}

#[instrument]
async fn async_step(name: &'static str) {
    telemetry::logger::info!("async child step={}", name);
    tokio::time::sleep(tokio::time::Duration::from_millis(60)).await;
}

#[instrument]
async fn run_async_pipeline() {
    async_step("first").await;
    async_step("second").await;
}

/// Single root → one trace id in Tempo (full Gantt: sync + async children).
#[instrument]
async fn full_demo() {
    run_sync_pipeline();
    run_async_pipeline().await;
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let mut telemetry = Telemetry::new()
        .with_service("simple-trace-test", "1.0.0")
        .environment(Environment::Development)
        .with_otlp("localhost", 6009)
        .with_tracing()
        .build()?;

    telemetry::logger::info!("=== One trace: full_demo → sync_pipeline + async_pipeline ===");
    full_demo().await;

    telemetry::logger::info!("Waiting for batch export...");
    tokio::time::sleep(tokio::time::Duration::from_secs(2)).await;
    telemetry.flush()?;

    telemetry::logger::info!("Done. Tempo: one trace `full_demo` with nested spans (Gantt)");
    telemetry.close()?;
    Ok(())
}
