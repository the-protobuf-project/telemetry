//! OTLP traces to `localhost:6009` via `#[instrument]` + batch export.
use telemetry::tracing::instrument;
use telemetry::{Environment, logger};

/// Runs a traced operation and pauses briefly.
///
/// # Examples
///
/// ```
/// simple_operation();
/// ```
#[instrument]
fn simple_operation() {
    logger::info!("traced operation");
    std::thread::sleep(std::time::Duration::from_millis(50));
}

/// Initializes local OTLP telemetry, executes the traced operation, and waits for pending spans to be exported.
///
/// # Errors
///
/// Returns an error if telemetry initialization fails.
///
/// # Examples
///
/// ```no_run
/// // Run the binary to initialize telemetry and execute the operation.
/// ```
async fn main() -> anyhow::Result<()> {
    let _telemetry = telemetry::telemetry_local_otel!()
        .with_service("simple-trace-test", "1.0.0")
        .environment(Environment::Development)
        .with_tracing()
        .build()?;

    logger::info!("Tracing → OTLP localhost:6009");

    simple_operation();

    tokio::time::sleep(tokio::time::Duration::from_secs(2)).await;
    Ok(())
}
