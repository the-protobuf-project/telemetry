//! The inventory service of the cross-language tracing demo (`just crosslang`):
//! it looks up each item of an order, reserves them, and hands the order to
//! billing. An axum server behind `ServerLayer` continues the trace the caller
//! sent, and the call to billing, made with reqwest (not a tower service, so
//! no `ClientLayer`), is a CLIENT span whose headers `propagation::inject`
//! writes.
//!
//! Each step is a span of its own under the request's, logs under it (so the
//! logs carry its trace and span id) and counts what it did, so the journey
//! shows in Grafana as one trace with its logs and metrics.
//!
//! It listens on a free port and prints `{"listening": <port>}`; GET /hop
//! answers with this hop's view of the trace, followed by the hops after it
//! (`CROSSLANG_NEXT`, a URL, when there is one). Set
//! `TELEMETRY_DEMO_OTLP=localhost:4317` to export to a collector.

use std::collections::HashMap;
use std::sync::Arc;
use std::time::Duration;

use axum::extract::{Query, State};
use axum::http::HeaderMap;
use axum::routing::get;
use axum::{Json, Router};
use opentelemetry::Context;
use opentelemetry::trace::{TraceContextExt, TracerProvider as _};
use opentelemetry_sdk::trace::SdkTracerProvider;
use serde_json::{Value, json};
use telemetry::logger;
use telemetry::metrics::CounterHandle;
use telemetry::propagation::{ServerLayer, inject};
use telemetry::tracing::{SpanKind, TelemetryTracing};

#[derive(Clone)]
struct App {
    tracing: Arc<TelemetryTracing>,
    reserved: CounterHandle,
}

/// One step of the request: a span under the current one, current itself
/// while `work` runs, so the logs written there carry it.
fn step<T>(app: &App, name: &'static str, attrs: &[(&str, String)], work: impl FnOnce() -> T) -> T {
    let mut span = app
        .tracing
        .start_span_with(name, SpanKind::Internal, Some(&Context::current()));
    for (k, v) in attrs {
        span.set_attribute(k, v.clone());
    }
    let out = {
        let _current = span.attach();
        work()
    };
    span.end();
    out
}

async fn hop(
    State(app): State<App>,
    Query(order): Query<HashMap<String, String>>,
    incoming: HeaderMap,
) -> Json<Value> {
    let cx = Context::current();
    let span = cx.span();
    let order_id = order.get("order").cloned().unwrap_or_default();
    let items: Vec<String> = order
        .get("items")
        .map(|i| i.split(',').map(str::to_string).collect())
        .unwrap_or_default();

    for item in &items {
        let attrs = [("order.id", order_id.clone()), ("item.sku", item.clone())];
        step(&app, "inventory.lookup", &attrs, || {
            std::thread::sleep(Duration::from_millis(6));
            logger::info!("{item} in stock for {order_id}");
        });
    }
    step(
        &app,
        "inventory.reserve",
        &[("order.id", order_id.clone())],
        || {
            std::thread::sleep(Duration::from_millis(10));
            let _ = app.reserved.add(items.len() as f64);
            logger::info!("reserved {} items for {order_id}", items.len());
        },
    );

    let mut hops = vec![json!({
        "service": "inventory-rs",
        "received_traceparent": incoming.get("traceparent").and_then(|v| v.to_str().ok()),
        "trace_id": span.span_context().trace_id().to_string(),
        "span_id": span.span_context().span_id().to_string(),
    })];
    if let Ok(next) = std::env::var("CROSSLANG_NEXT") {
        // A CLIENT span under this request's server span; billing continues it.
        let call = app
            .tracing
            .start_span_with("GET", SpanKind::Client, Some(&Context::current()));
        let mut headers = axum::http::HeaderMap::new();
        inject(&call.context(), &mut headers);
        let query: Vec<(&String, &String)> = order.iter().collect();
        let downstream: Vec<Value> = reqwest::Client::new()
            .get(format!("{next}/hop"))
            .query(&query)
            .headers(headers)
            .send()
            .await
            .and_then(|r| r.error_for_status())
            .expect("the next service answered")
            .json()
            .await
            .expect("the next service's hops");
        call.end();
        hops.extend(downstream);
    }
    Json(Value::Array(hops))
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
    let mut telemetry = builder.build()?;

    // Exporting, spans come from the tracer Telemetry installs; without a
    // collector, from a local one, so they still get ids of their own.
    let (layer, tracing) = match telemetry.tracing.take().filter(|t| t.is_enabled()) {
        Some(tracing) if otlp.is_some() => (ServerLayer::new(), tracing),
        _ => {
            let local = SdkTracerProvider::builder().build();
            (
                ServerLayer::with_tracer(local.tracer("inventory-rs")),
                TelemetryTracing::new(Some(local.tracer("inventory-rs"))),
            )
        }
    };
    let app = App {
        tracing: Arc::new(tracing),
        reserved: telemetry
            .metrics
            .counter_handle("inventory_items_reserved_total"),
    };
    let router = Router::new()
        .route("/hop", get(hop))
        .with_state(app)
        .layer(layer);

    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await?;
    println!("{}", json!({"listening": listener.local_addr()?.port()}));
    // Stopped with SIGTERM or Ctrl-C: finish the requests under way, then
    // close Telemetry, which exports the spans still queued.
    axum::serve(listener, router)
        .with_graceful_shutdown(stopped())
        .await?;
    telemetry.close()?;
    Ok(())
}

async fn stopped() {
    let mut term = tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate())
        .expect("a SIGTERM handler");
    tokio::select! {
        _ = term.recv() => {}
        _ = tokio::signal::ctrl_c() => {}
    }
}
