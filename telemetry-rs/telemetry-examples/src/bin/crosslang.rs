//! Service B of the cross-language tracing demo: continues, in Rust, the trace
//! a Go service started, from the W3C headers it was sent.
//!
//! ```text
//! go run ./examples/crosslang | cargo run -q -p telemetry-examples --bin crosslang
//! ```
//!
//! The headers arrive on stdin as a JSON object; in a real service they are
//! the incoming HTTP headers, gRPC metadata or message headers. Set
//! `TELEMETRY_DEMO_OTLP=localhost:4317` to export the spans to a collector.
use std::collections::HashMap;
use std::io::Read;

use opentelemetry::trace::TracerProvider as _;
use opentelemetry_sdk::trace::SdkTracerProvider;
use telemetry::tracing::{SpanKind, TelemetryTracing};

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let mut input = String::new();
    std::io::stdin().read_to_string(&mut input)?;
    let headers: HashMap<String, String> = serde_json::from_str(&input)?;

    let mut builder = telemetry::Telemetry::builder("inventory-rs", "1.0.0").with_tracing();
    if let Some((host, port)) = std::env::var("TELEMETRY_DEMO_OTLP")
        .ok()
        .and_then(|ep| ep.rsplit_once(':').map(|(h, p)| (h.to_string(), p.parse().unwrap_or(4317))))
    {
        builder = builder.with_otlp(host, port);
    }
    let telemetry = builder.build()?;

    // Without a collector the SDK has no tracer; use a local one so the
    // spans still get real ids to compare.
    let local;
    let tracing = match telemetry.tracing.as_ref().filter(|t| t.is_enabled()) {
        Some(t) => t,
        None => {
            local = TelemetryTracing::new(Some(SdkTracerProvider::builder().build().tracer("inventory-rs")));
            &local
        }
    };

    // The one call that joins the caller's trace.
    let parent = tracing.extract(&headers);
    let mut span = tracing.start_span_with("inventory.reserve", SpanKind::Server, Some(&parent));
    span.set_attribute("order.id", "o-42");

    // And the next hop continues it in turn.
    let mut downstream = HashMap::new();
    tracing.inject(&span, &mut downstream);

    println!(
        "{}",
        serde_json::json!({
            "received_traceparent": headers.get("traceparent"),
            "trace_id": span.trace_id(),
            "span_id": span.span_id(),
            "downstream_traceparent": downstream.get("traceparent"),
        })
    );
    span.end();
    Ok(())
}
