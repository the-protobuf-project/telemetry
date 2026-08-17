//! OTLP traces to `localhost:6009` via `#[instrument]` + batch export.
use telemetry::tracing::instrument;
use telemetry::{Environment, logger};

#[instrument]
fn simple_operation() {
    logger::info!("traced operation");
    std::thread::sleep(std::time::Duration::from_millis(50));
}

#[tokio::main]
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
