//! A trace crossing a real HTTP hop: another service's traceparent comes in
//! over TCP to an axum server behind `ServerLayer`, and the handler runs in
//! that trace and hands it on.

use axum::Router;
use axum::routing::get;
use opentelemetry::Context;
use opentelemetry::trace::{TraceContextExt, TracerProvider as _};
use opentelemetry_sdk::trace::{InMemorySpanExporter, SdkTracerProvider};
use telemetry::propagation::{ServerLayer, headers};

const UPSTREAM_TRACE: &str = "0af7651916cd43dd8448eb211c80319c";
const UPSTREAM_SPAN: &str = "b7ad6b7169203331";

#[tokio::test]
async fn an_axum_server_continues_the_callers_trace() {
    telemetry::tracing::install_propagator();
    let exporter = InMemorySpanExporter::default();
    let provider = SdkTracerProvider::builder()
        .with_simple_exporter(exporter.clone())
        .build();

    // The handler answers with the trace it runs in and the traceparent an
    // outgoing request from it would carry.
    let app = Router::new()
        .route(
            "/orders",
            get(|| async {
                let trace = Context::current()
                    .span()
                    .span_context()
                    .trace_id()
                    .to_string();
                let onward = headers()["traceparent"].to_str().unwrap().to_string();
                format!("{trace} {onward}")
            }),
        )
        .layer(ServerLayer::with_tracer(provider.tracer("test")));
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let addr = listener.local_addr().unwrap();
    tokio::spawn(async move { axum::serve(listener, app).await.unwrap() });

    let body = reqwest::Client::new()
        .get(format!("http://{addr}/orders"))
        .header(
            "traceparent",
            format!("00-{UPSTREAM_TRACE}-{UPSTREAM_SPAN}-01"),
        )
        .send()
        .await
        .unwrap()
        .text()
        .await
        .unwrap();
    let (trace, onward) = body.split_once(' ').unwrap();

    let spans = exporter.get_finished_spans().unwrap();
    let server = spans.first().expect("the server span");
    assert_eq!(trace, UPSTREAM_TRACE);
    assert_eq!(server.span_context.trace_id().to_string(), UPSTREAM_TRACE);
    assert_eq!(server.parent_span_id.to_string(), UPSTREAM_SPAN);
    assert_eq!(
        onward,
        format!("00-{UPSTREAM_TRACE}-{}-01", server.span_context.span_id())
    );
}

/// Compiled, not run: a tonic server takes `ServerLayer` as it is.
#[allow(dead_code)]
async fn a_tonic_server_takes_the_layer(addr: std::net::SocketAddr) {
    tonic::transport::Server::builder()
        .layer(ServerLayer::new())
        .add_routes(tonic::service::Routes::default())
        .serve(addr)
        .await
        .unwrap();
}
