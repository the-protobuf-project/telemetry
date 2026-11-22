//! Trace propagation over HTTP and gRPC.
//!
//! A trace crosses a request when the caller sends the W3C `traceparent`
//! header and the callee continues it; every SDK here does both, so a trace
//! runs end to end across services in any of their languages. Rust cannot
//! patch a library at run time, so servers and clients take one layer each.
//! axum, hyper and tonic all serve and send `http::Request`s through tower,
//! so the same two layers cover HTTP and gRPC:
//!
//! ```no_run
//! # use telemetry::propagation::{ClientLayer, ServerLayer};
//! # async fn demo(router: axum::Router, channel: tonic::transport::Channel) {
//! // Servers: each request runs in a SERVER span that continues the caller's trace.
//! let app = router.layer(ServerLayer::new());                 // axum
//! // tonic::transport::Server::builder().layer(ServerLayer::new())...  // tonic
//!
//! // Clients: each request is a CLIENT span and carries the trace.
//! let channel = tower::ServiceBuilder::new().layer(ClientLayer::new()).service(channel);
//! # }
//! ```
//!
//! A client that is not a tower service, such as reqwest, takes the headers:
//! `client.get(url).headers(telemetry::propagation::headers())`.
//!
//! The layers use the global tracer and propagator Telemetry installs,
//! looked up per request, so a layer built before Telemetry still traces.
//! With tracing off they pass a caller's trace on: the no-op tracer keeps
//! the parent's span context. A span ends when the response headers arrive,
//! so a streamed body is not part of it.

use std::borrow::Cow;
use std::future::Future;
use std::pin::Pin;
use std::sync::Arc;
use std::task::{Context as TaskContext, Poll};

use http::{HeaderMap, Request, Response};
use opentelemetry::global::{self, BoxedTracer};
use opentelemetry::trace::{FutureExt, SpanKind, Status, TraceContextExt, Tracer};
use opentelemetry::{Context, KeyValue};
use opentelemetry_http::{HeaderExtractor, HeaderInjector};
use tower_layer::Layer;
use tower_service::Service;

const SCOPE: &str = "telemetry-rs";

/// The trace context a caller sent in `headers`; empty without one.
pub fn extract(headers: &HeaderMap) -> Context {
    global::get_text_map_propagator(|p| p.extract(&HeaderExtractor(headers)))
}

/// Writes `cx`'s trace context into `headers`.
pub fn inject(cx: &Context, headers: &mut HeaderMap) {
    global::get_text_map_propagator(|p| p.inject_context(cx, &mut HeaderInjector(headers)));
}

/// The current trace context as headers, for a client the layers do not
/// cover: `reqwest_client.get(url).headers(telemetry::propagation::headers())`.
pub fn headers() -> HeaderMap {
    let mut headers = HeaderMap::new();
    inject(&Context::current(), &mut headers);
    headers
}

/// Where spans come from: the global tracer at each request, or a given one.
#[derive(Clone, Default)]
struct Tracing(Option<Arc<BoxedTracer>>);

impl Tracing {
    fn start(&self, req: &Request<impl Sized>, kind: SpanKind, parent: &Context) -> Context {
        let (name, attributes) = describe(req, kind.clone());
        let start = |t: &BoxedTracer| {
            t.span_builder(name)
                .with_kind(kind.clone())
                .with_attributes(attributes)
                .start_with_context(t, parent)
        };
        let span = match &self.0 {
            Some(t) => start(t),
            None => start(&global::tracer(SCOPE)),
        };
        parent.with_span(span)
    }
}

fn boxed<T>(tracer: T) -> Tracing
where
    T: Tracer + Send + Sync + 'static,
    T::Span: Send + Sync + 'static,
{
    Tracing(Some(Arc::new(BoxedTracer::new(Box::new(tracer)))))
}

fn is_grpc(headers: &HeaderMap) -> bool {
    headers
        .get(http::header::CONTENT_TYPE)
        .and_then(|v| v.to_str().ok())
        .is_some_and(|v| v.starts_with("application/grpc"))
}

/// A span's name and attributes, as the semantic conventions give them: gRPC
/// as `package.Service/Method`, HTTP by its method.
fn describe<B>(req: &Request<B>, kind: SpanKind) -> (Cow<'static, str>, Vec<KeyValue>) {
    let path = req.uri().path();
    if is_grpc(req.headers()) {
        let full = path.trim_start_matches('/').to_string();
        let (service, method) = full.split_once('/').unwrap_or((full.as_str(), ""));
        let attributes = vec![
            KeyValue::new("rpc.system", "grpc"),
            KeyValue::new("rpc.service", service.to_string()),
            KeyValue::new("rpc.method", method.to_string()),
        ];
        return (full.into(), attributes);
    }
    let method = req.method().as_str().to_string();
    let mut attributes = vec![KeyValue::new("http.request.method", method.clone())];
    if kind == SpanKind::Client {
        attributes.push(KeyValue::new("url.full", req.uri().to_string()));
    } else {
        attributes.push(KeyValue::new("url.path", path.to_string()));
    }
    (method.into(), attributes)
}

/// Sets the span's outcome from the response: its HTTP status, and a gRPC
/// status when it came in the headers (a trailers-only error response).
fn finish<B>(cx: &Context, res: &Response<B>, kind: &SpanKind) {
    let span = cx.span();
    let status = res.status().as_u16();
    if is_grpc(res.headers()) {
        if let Some(code) = res
            .headers()
            .get("grpc-status")
            .and_then(|v| v.to_str().ok())
            .and_then(|v| v.parse::<i64>().ok())
        {
            span.set_attribute(KeyValue::new("rpc.grpc.status_code", code));
            if code != 0 {
                span.set_status(Status::error(format!("gRPC status {code}")));
            }
        }
    } else {
        span.set_attribute(KeyValue::new(
            "http.response.status_code",
            i64::from(status),
        ));
        // A client failed on any error status; a server only on its own (5xx).
        let failed = if *kind == SpanKind::Client {
            status >= 400
        } else {
            status >= 500
        };
        if failed {
            span.set_status(Status::error(format!("HTTP {status}")));
        }
    }
    span.end();
}

fn fail(cx: &Context, error: &dyn std::fmt::Display) {
    let span = cx.span();
    span.set_status(Status::error(error.to_string()));
    span.end();
}

type BoxFuture<T, E> = Pin<Box<dyn Future<Output = Result<T, E>> + Send>>;

/// Wraps a server (axum, hyper or tonic): each request runs in a SERVER span
/// that continues the trace the caller sent, so spans, logs and outgoing
/// requests inside the handler join it.
#[derive(Clone, Default)]
pub struct ServerLayer {
    tracing: Tracing,
}

impl ServerLayer {
    pub fn new() -> Self {
        Self::default()
    }

    /// Spans from `tracer` rather than the global one.
    pub fn with_tracer<T>(tracer: T) -> Self
    where
        T: Tracer + Send + Sync + 'static,
        T::Span: Send + Sync + 'static,
    {
        Self {
            tracing: boxed(tracer),
        }
    }
}

impl<S> Layer<S> for ServerLayer {
    type Service = TracedServer<S>;

    fn layer(&self, inner: S) -> Self::Service {
        TracedServer {
            inner,
            tracing: self.tracing.clone(),
        }
    }
}

/// The service [`ServerLayer`] makes.
#[derive(Clone)]
pub struct TracedServer<S> {
    inner: S,
    tracing: Tracing,
}

impl<S, B, R> Service<Request<B>> for TracedServer<S>
where
    S: Service<Request<B>, Response = Response<R>>,
    S::Future: Send + 'static,
    S::Error: std::fmt::Display,
{
    type Response = S::Response;
    type Error = S::Error;
    type Future = BoxFuture<S::Response, S::Error>;

    fn poll_ready(&mut self, cx: &mut TaskContext<'_>) -> Poll<Result<(), Self::Error>> {
        self.inner.poll_ready(cx)
    }

    fn call(&mut self, req: Request<B>) -> Self::Future {
        let parent = extract(req.headers());
        let cx = self.tracing.start(&req, SpanKind::Server, &parent);
        let future = {
            let _current = cx.clone().attach();
            self.inner.call(req)
        };
        Box::pin(
            async move {
                let result = future.await;
                match &result {
                    Ok(res) => finish(&Context::current(), res, &SpanKind::Server),
                    Err(e) => fail(&Context::current(), e),
                }
                result
            }
            .with_context(cx),
        )
    }
}

/// Wraps a client (a tonic channel, a hyper client): each request is a
/// CLIENT span under the current one and carries the trace.
#[derive(Clone, Default)]
pub struct ClientLayer {
    tracing: Tracing,
}

impl ClientLayer {
    pub fn new() -> Self {
        Self::default()
    }

    /// Spans from `tracer` rather than the global one.
    pub fn with_tracer<T>(tracer: T) -> Self
    where
        T: Tracer + Send + Sync + 'static,
        T::Span: Send + Sync + 'static,
    {
        Self {
            tracing: boxed(tracer),
        }
    }
}

impl<S> Layer<S> for ClientLayer {
    type Service = TracedClient<S>;

    fn layer(&self, inner: S) -> Self::Service {
        TracedClient {
            inner,
            tracing: self.tracing.clone(),
        }
    }
}

/// The service [`ClientLayer`] makes.
#[derive(Clone)]
pub struct TracedClient<S> {
    inner: S,
    tracing: Tracing,
}

impl<S, B, R> Service<Request<B>> for TracedClient<S>
where
    S: Service<Request<B>, Response = Response<R>>,
    S::Future: Send + 'static,
    S::Error: std::fmt::Display,
{
    type Response = S::Response;
    type Error = S::Error;
    type Future = BoxFuture<S::Response, S::Error>;

    fn poll_ready(&mut self, cx: &mut TaskContext<'_>) -> Poll<Result<(), Self::Error>> {
        self.inner.poll_ready(cx)
    }

    fn call(&mut self, mut req: Request<B>) -> Self::Future {
        let cx = self
            .tracing
            .start(&req, SpanKind::Client, &Context::current());
        inject(&cx, req.headers_mut());
        let future = {
            let _current = cx.clone().attach();
            self.inner.call(req)
        };
        Box::pin(
            async move {
                let result = future.await;
                match &result {
                    Ok(res) => finish(&Context::current(), res, &SpanKind::Client),
                    Err(e) => fail(&Context::current(), e),
                }
                result
            }
            .with_context(cx),
        )
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use opentelemetry::trace::{Span as _, TracerProvider as _};
    use opentelemetry_sdk::trace::{InMemorySpanExporter, SdkTracerProvider, SpanData};
    use std::convert::Infallible;
    use tower::{ServiceBuilder, ServiceExt, service_fn};

    // What another language's SDK sends: a sampled trace this process never saw.
    const UPSTREAM_TRACE: &str = "0af7651916cd43dd8448eb211c80319c";
    const UPSTREAM_SPAN: &str = "b7ad6b7169203331";

    fn provider() -> (SdkTracerProvider, InMemorySpanExporter) {
        crate::tracing::install_propagator();
        let exporter = InMemorySpanExporter::default();
        let provider = SdkTracerProvider::builder()
            .with_simple_exporter(exporter.clone())
            .build();
        (provider, exporter)
    }

    fn spans(exporter: &InMemorySpanExporter) -> Vec<SpanData> {
        exporter.get_finished_spans().unwrap()
    }

    fn of_kind(spans: &[SpanData], kind: SpanKind) -> &SpanData {
        spans
            .iter()
            .find(|s| s.span_kind == kind)
            .expect("span of kind")
    }

    #[tokio::test]
    async fn a_server_continues_another_services_trace_and_hands_it_on() {
        let (provider, exporter) = provider();
        let tracer = provider.tracer("test");

        // The next service: records the traceparent it gets.
        let seen = Arc::new(std::sync::Mutex::new(String::new()));
        let next = {
            let seen = Arc::clone(&seen);
            service_fn(move |req: Request<()>| {
                let seen = Arc::clone(&seen);
                async move {
                    *seen.lock().unwrap() = req.headers()["traceparent"].to_str().unwrap().into();
                    Ok::<_, Infallible>(Response::new(()))
                }
            })
        };
        let client = ServiceBuilder::new()
            .layer(ClientLayer::with_tracer(provider.tracer("client")))
            .service(next);

        // This service: its handler calls the next one.
        let server = ServiceBuilder::new()
            .layer(ServerLayer::with_tracer(tracer))
            .service(service_fn(move |_req: Request<()>| {
                let client = client.clone();
                async move {
                    client.oneshot(Request::new(())).await?;
                    Ok::<_, Infallible>(Response::new(()))
                }
            }));

        let mut req = Request::builder().uri("/orders").body(()).unwrap();
        req.headers_mut().insert(
            "traceparent",
            format!("00-{UPSTREAM_TRACE}-{UPSTREAM_SPAN}-01")
                .parse()
                .unwrap(),
        );
        server.oneshot(req).await.unwrap();

        let spans = spans(&exporter);
        let server = of_kind(&spans, SpanKind::Server);
        let client = of_kind(&spans, SpanKind::Client);
        assert_eq!(server.span_context.trace_id().to_string(), UPSTREAM_TRACE);
        assert_eq!(server.parent_span_id.to_string(), UPSTREAM_SPAN);
        assert_eq!(client.parent_span_id, server.span_context.span_id());
        let onward = seen.lock().unwrap().clone();
        assert_eq!(
            onward,
            format!("00-{UPSTREAM_TRACE}-{}-01", client.span_context.span_id())
        );
    }

    #[tokio::test]
    async fn grpc_requests_are_named_by_method_and_carry_their_status() {
        let (provider, exporter) = provider();
        let server = ServiceBuilder::new()
            .layer(ServerLayer::with_tracer(provider.tracer("test")))
            .service(service_fn(|_req: Request<()>| async {
                let res = Response::builder()
                    .header("content-type", "application/grpc")
                    .header("grpc-status", "5")
                    .body(())
                    .unwrap();
                Ok::<_, Infallible>(res)
            }));
        let req = Request::builder()
            .uri("/grpc.health.v1.Health/Check")
            .header("content-type", "application/grpc")
            .body(())
            .unwrap();
        server.oneshot(req).await.unwrap();

        let spans = spans(&exporter);
        let span = of_kind(&spans, SpanKind::Server);
        assert_eq!(span.name, "grpc.health.v1.Health/Check");
        assert!(matches!(span.status, Status::Error { .. }));
        assert!(
            span.attributes
                .contains(&KeyValue::new("rpc.service", "grpc.health.v1.Health"))
        );
    }

    #[test]
    fn headers_carry_the_current_trace() {
        let (provider, _exporter) = provider();
        let span = provider.tracer("test").start("checkout");
        let trace_id = span.span_context().trace_id().to_string();
        let _current = Context::current_with_span(span).attach();
        let traceparent = headers()["traceparent"].to_str().unwrap().to_string();
        assert!(traceparent.contains(&trace_id), "{traceparent}");
    }
}
