// Spans: `#[instrument]` → one trace per root (Gantt). Logs: `telemetry::logger::info!` etc.
// Parent chain needs OTEL layer unfiltered — fixed in `init_tokio_tracing` (fmt-only filter).
use telemetry::tracing::instrument;
use telemetry::{Environment, Telemetry};

/// Simulates connecting to a database.
///
/// # Examples
///
/// ```
/// connect_to_db();
/// ```
fn connect_to_db() {
    telemetry::logger::info!("child span: connect_to_db");
    std::thread::sleep(std::time::Duration::from_millis(80));
}

/// Simulates a database query.
///
/// # Examples
///
/// ```
/// query_database();
/// ```
#[instrument]
fn query_database() {
    telemetry::logger::info!("child span: query_database");
    std::thread::sleep(std::time::Duration::from_millis(80));
}

/// Processes a data-processing step in the pipeline.
///
/// # Examples
///
/// ```
/// process_data();
/// ```
fn process_data() {
    telemetry::logger::info!("child span: process_data");
    std::thread::sleep(std::time::Duration::from_millis(80));
}

/// Saves data and records a tracing span.
///
/// # Examples
///
/// ```
/// save_data();
/// ```
fn save_data() {
    telemetry::logger::info!("child span: save_data");
    std::thread::sleep(std::time::Duration::from_millis(80));
}

/// Runs the synchronous data-processing pipeline in sequence.
///
/// # Examples
///
/// ```
/// run_sync_pipeline();
/// ```
fn run_sync_pipeline() {
    connect_to_db();
    query_database();
    process_data();
    save_data();
}

/// Executes a named asynchronous pipeline step.
///
/// # Examples
///
/// ```
/// #[tokio::test]
/// async fn runs_step() {
///     async_step("fetch").await;
/// }
/// ```
async fn async_step(name: &'static str) {
    telemetry::logger::info!("async child step={}", name);
    tokio::time::sleep(tokio::time::Duration::from_millis(60)).await;
}

/// Runs the asynchronous pipeline steps in sequence.
///
/// # Examples
///
/// ```
/// # tokio::runtime::Runtime::new().unwrap().block_on(async {
/// run_async_pipeline().await;
/// # });
/// ```
#[instrument]
async fn run_async_pipeline() {
    async_step("first").await;
    async_step("second").await;
}

/// Runs the synchronous and asynchronous pipelines within a single root operation.
///
/// # Examples
///
/// ```
/// tokio::runtime::Runtime::new()
///     .unwrap()
///     .block_on(full_demo());
/// ```
async fn full_demo() {
    run_sync_pipeline();
    run_async_pipeline().await;
}

/// Initializes telemetry, runs the complete tracing demo, exports the collected data, and shuts down telemetry.
///
/// # Examples
///
/// ```no_run
/// // Run the binary to execute the demo and export its trace.
/// ```
///
/// # Errors
///
/// Returns an error if telemetry initialization, flushing, or shutdown fails.
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
