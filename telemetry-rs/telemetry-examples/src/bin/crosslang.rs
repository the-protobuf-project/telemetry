//! The inventory service of the cross-language tracing demo (`just crosslang`):
//! an axum server behind `ServerLayer`, which continues the trace the caller
//! sent, calling the next service with reqwest and `propagation::headers()`.
//! Neither side has an inject or extract of its own.
//!
//! It listens on a free port and prints `{"listening": <port>}`; GET /hop
//! answers with this hop's view of the trace, followed by the hops after it
//! (`CROSSLANG_NEXT`, a URL, when there is one). Set
//! `TELEMETRY_DEMO_OTLP=localhost:4317` to export the spans to a collector.

use axum::Router;
use axum::http::HeaderMap;
use axum::routing::get;
use opentelemetry::Context;
use opentelemetry::trace::{TraceContextExt, TracerProvider as _};
use opentelemetry_sdk::trace::SdkTracerProvider;
use serde_json::{Value, json};
use telemetry::propagation::{ServerLayer, headers};

async fn hop(incoming: HeaderMap) -> axum::Json<Value> {
    let cx = Context::current();
    let span = cx.span();
    let mut hops = vec![json!({
        "service": "inventory-rs",
        "received_traceparent": incoming.get("traceparent").and_then(|v| v.to_str().ok()),
        "trace_id": span.span_context().trace_id().to_string(),
        "span_id": span.span_context().span_id().to_string(),
    })];
    if let Ok(next) = std::env::var("CROSSLANG_NEXT") {
        // The current context is this request's server span: the next
        // service continues it.
        let downstream: Vec<Value> = reqwest::Client::new()
            .get(format!("{next}/hop"))
            .headers(headers())
            .send()
            .await
            .and_then(|r| r.error_for_status())
            .expect("the next service answered")
            .json()
            .await
            .expect("the next service's hops");
        hops.extend(downstream);
    }
    axum::Json(Value::Array(hops))
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let mut builder = telemetry::Telemetry::builder("inventory-rs", "1.0.0").with_tracing();
    let otlp = std::env::var("TELEMETRY_DEMO_OTLP").ok().and_then(|ep| {
        ep.rsplit_once(':')
            .map(|(h, p)| (h.to_string(), p.parse().unwrap_or(4317)))
    });
    if let Some((host, port)) = otlp.clone() {
        builder = builder.with_otlp(host, port);
    }
    let _telemetry = builder.build()?;

    // Exporting, the layer uses the global tracer Telemetry installs; without
    // a collector, a local one, so the spans still get ids of their own.
    let layer = if otlp.is_some() {
        ServerLayer::new()
    } else {
        ServerLayer::with_tracer(SdkTracerProvider::builder().build().tracer("inventory-rs"))
    };
    let app = Router::new().route("/hop", get(hop)).layer(layer);

    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await?;
    println!("{}", json!({"listening": listener.local_addr()?.port()}));
    axum::serve(listener, app).await?;
    Ok(())
}
