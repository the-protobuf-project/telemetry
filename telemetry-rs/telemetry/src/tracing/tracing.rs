//! Distributed tracing with OpenTelemetry.
//!
//! This module provides integration with OpenTelemetry for distributed tracing,
//! supporting both tokio-rs/tracing instrumentation and manual span management.

use std::borrow::Cow;
use std::collections::HashMap;

use anyhow::Result;
use opentelemetry::baggage::BaggageExt;
use opentelemetry::propagation::TextMapCompositePropagator;
use opentelemetry::trace::{Span as _, SpanKind, Status, TraceContextExt, Tracer as _};
use opentelemetry::{Context, ContextGuard, KeyValue};
use opentelemetry_sdk::propagation::{BaggagePropagator, TraceContextPropagator};

/// Installs the W3C TraceContext and Baggage propagator globally, the same
/// one telemetry-go installs, so `traceparent`, `tracestate` and `baggage`
/// headers carry a trace across services whatever language each is written
/// in. Telemetry's builder calls it; it is safe to call more than once.
pub fn install_propagator() {
    opentelemetry::global::set_text_map_propagator(TextMapCompositePropagator::new(vec![
        Box::new(TraceContextPropagator::new()),
        Box::new(BaggagePropagator::new()),
    ]));
}

/// Configures global `tracing` instrumentation with OpenTelemetry and console output.
///
/// The supplied tracer receives application spans while infrastructure and exporter-related
/// targets are excluded from OpenTelemetry output. Console filtering respects `RUST_LOG`;
/// setting `ENABLE_TRACING_DEFAULT` enables trace-level output by default. If a global subscriber
/// is already configured, a warning is logged and initialization still succeeds.
///
/// # Examples
///
/// ```no_run
/// use telemetry::tracing::init_tokio_tracing;
///
/// let tracer = todo!("configure an OpenTelemetry SDK tracer");
/// init_tokio_tracing(tracer).unwrap();
/// ```
pub fn init_tokio_tracing(tracer: opentelemetry_sdk::trace::Tracer) -> Result<()> {
    use tracing_subscriber::EnvFilter;
    use tracing_subscriber::Layer;
    use tracing_subscriber::layer::SubscriberExt;

    /// Drop infra spans from OTLP (they are not your app trace; often emitted during export).
    fn otel_export_filter() -> EnvFilter {
        const BLOCK: &str = "\
            h2=off,\
            hyper=off,\
            hyper_util=off,\
            tonic=off,\
            tower=off,\
            tower_http=off,\
            reqwest=off,\
            rustls=off,\
            tokio_rustls=off,\
            opentelemetry=off,\
            opentelemetry_sdk=off,\
            opentelemetry_http=off,\
            opentelemetry_otlp=off";
        EnvFilter::try_new(format!("trace,{BLOCK}")).unwrap_or_else(|_| EnvFilter::new("trace"))
    }

    /// Console: respect `RUST_LOG` but keep h2/tonic/hyper quiet on stdout.
    fn fmt_filter() -> EnvFilter {
        const QUIET: &str = "opentelemetry=warn,opentelemetry_sdk=warn,h2=warn,tonic=warn,hyper=warn,tower=warn,reqwest=warn";
        let user = std::env::var("RUST_LOG").unwrap_or_default();
        let base = if std::env::var("ENABLE_TRACING_DEFAULT").is_ok() {
            if user.is_empty() {
                "trace".to_string()
            } else {
                user
            }
        } else if user.is_empty() {
            format!("info,{QUIET}")
        } else {
            format!("{user},{QUIET}")
        };
        EnvFilter::try_new(&base).unwrap_or_else(|_| EnvFilter::new("info"))
    }

    let telemetry = tracing_opentelemetry::layer()
        .with_tracer(tracer)
        .with_filter(otel_export_filter());
    let fmt_layer = tracing_subscriber::fmt::layer()
        .with_target(false)
        .with_thread_ids(true)
        .with_filter(fmt_filter());

    // Do not use `try_init()`: it runs `LogTracer::init()` after `set_global_default`, which
    // **always fails** when Telemetry already called `log4rs::init_*` — returning Err even though
    // the subscriber was installed. That looked like a broken trace pipeline.
    let subscriber = tracing_subscriber::registry()
        .with(telemetry)
        .with(fmt_layer);
    let dispatch: tracing::Dispatch = subscriber.into();
    if tracing::dispatcher::set_global_default(dispatch).is_err() {
        log::warn!(
            "telemetry: global tracing subscriber already set (second Telemetry::build in this process); \
             #[instrument] OTLP export applies only to the first Telemetry instance"
        );
    }

    Ok(())
}

/// Telemetry tracing for manual span management.
///
/// Provides manual control over span creation and management,
/// as an alternative to the `#[instrument]` macro.
///
/// # Examples
///
/// ```no_run
/// use telemetry::tracing::TelemetryTracing;
///
/// // Construct from an existing telemetry tracer (or `None` when tracing is disabled).
/// let tracing = TelemetryTracing::new(None);
///
/// let mut span = tracing.start_span("my_operation");
/// span.set_attribute("key", "value");
/// span.end();
/// ```
pub struct TelemetryTracing {
    tracer: Option<opentelemetry_sdk::trace::Tracer>,
}

impl TelemetryTracing {
    /// Creates a tracing instance with an optional OpenTelemetry tracer.
    ///
    /// # Examples
    ///
    /// ```
    /// use telemetry::tracing::TelemetryTracing;
    ///
    /// let tracing = TelemetryTracing::new(
    ///     None::<opentelemetry_sdk::trace::Tracer>,
    /// );
    /// assert!(!tracing.is_enabled());
    /// ```
    pub fn new(tracer: Option<opentelemetry_sdk::trace::Tracer>) -> Self {
        Self { tracer }
    }

    /// Starts a new span.
    ///
    /// # Arguments
    ///
    /// * `name` - Span name
    pub fn start_span(&self, name: &'static str) -> Span {
        Span::new(self.tracer.as_ref(), name)
    }

    /// Starts a span of `kind` as a child of `parent`: a context from
    /// [`TelemetryTracing::extract`] continues a remote caller's trace, one
    /// from [`Span::context`] nests under a local span, and `None` starts a
    /// new trace.
    ///
    /// ```no_run
    /// # use std::collections::HashMap;
    /// # use telemetry::tracing::{TelemetryTracing, SpanKind};
    /// # let tracing = TelemetryTracing::new(None);
    /// # let headers: HashMap<String, String> = HashMap::new();
    /// // Server side of a request from a Go service.
    /// let parent = tracing.extract(&headers);
    /// let span = tracing.start_span_with("orders.handle", SpanKind::Server, Some(&parent));
    /// span.end();
    /// ```
    pub fn start_span_with(
        &self,
        name: impl Into<Cow<'static, str>>,
        kind: SpanKind,
        parent: Option<&Context>,
    ) -> Span {
        let parent = parent.cloned().unwrap_or_default();
        let span = self.tracer.as_ref().map(|t| {
            t.span_builder(name)
                .with_kind(kind)
                .start_with_context(t, &parent)
        });
        Span { span, parent }
    }

    /// Returns `cx` with `key=value` added to its W3C baggage: it travels with
    /// the trace to every service downstream (inject writes it, extract reads
    /// it), in any language. Start spans under the returned context.
    pub fn set_baggage(
        &self,
        cx: &Context,
        key: impl Into<String>,
        value: impl Into<String>,
    ) -> Context {
        // with_baggage replaces the whole set: carry the existing entries over.
        let key = key.into();
        let mut entries: Vec<KeyValue> = cx
            .baggage()
            .iter()
            .filter(|(k, _)| k.as_str() != key)
            .map(|(k, (v, _))| KeyValue::new(k.clone(), v.clone()))
            .collect();
        entries.push(KeyValue::new(key, value.into()));
        cx.with_baggage(entries)
    }

    /// The W3C baggage `cx` carries.
    pub fn baggage(&self, cx: &Context) -> HashMap<String, String> {
        cx.baggage()
            .iter()
            .map(|(k, (v, _))| (k.to_string(), v.to_string()))
            .collect()
    }

    /// Reads the trace context a caller sent in `headers` (`traceparent`,
    /// `tracestate`, `baggage`). Without them it is an empty context, and a
    /// span started under it begins a new trace.
    pub fn extract(&self, headers: &HashMap<String, String>) -> Context {
        opentelemetry::global::get_text_map_propagator(|p| p.extract(headers))
    }

    /// Writes `span`'s trace context into `headers`, for the service the
    /// request or message goes to — in any language — to continue the trace.
    /// Writes nothing when tracing is off.
    pub fn inject(&self, span: &Span, headers: &mut HashMap<String, String>) {
        if span.span.is_none() {
            return;
        }
        let cx = span.context();
        opentelemetry::global::get_text_map_propagator(|p| p.inject_context(&cx, headers));
    }

    /// Checks if tracing is enabled.
    pub fn is_enabled(&self) -> bool {
        self.tracer.is_some()
    }
}

/// A tracing span.
///
/// Represents a unit of work in distributed tracing.
pub struct Span {
    span: Option<opentelemetry_sdk::trace::Span>,
    // The context the span started under: its baggage goes on with the trace.
    parent: Context,
}

impl Span {
    /// Creates a new span (internal use).
    pub(crate) fn new(
        tracer: Option<&opentelemetry_sdk::trace::Tracer>,
        name: &'static str,
    ) -> Self {
        let span = tracer.map(|t| t.start(name));

        Self {
            span,
            parent: Context::new(),
        }
    }

    /// A context carrying this span, to start child spans under it or to
    /// inject into outgoing headers.
    pub fn context(&self) -> Context {
        match &self.span {
            Some(span) => self
                .parent
                .with_remote_span_context(span.span_context().clone()),
            None => self.parent.clone(),
        }
    }

    /// Makes this span current until the guard is dropped. Log records emitted
    /// meanwhile (`logger::info!` and friends) carry its trace and span id, so
    /// a trace in Tempo finds the logs every service wrote under it —
    /// distributed logging.
    ///
    /// ```no_run
    /// # use telemetry::tracing::{TelemetryTracing, SpanKind};
    /// # let tracing = TelemetryTracing::new(None);
    /// let span = tracing.start_span_with("reserve", SpanKind::Server, None);
    /// let _current = span.attach();
    /// telemetry::logger::info!("reserved"); // correlated with the span
    /// ```
    pub fn attach(&self) -> ContextGuard {
        self.context().attach()
    }

    /// The span's trace id as 32 hex digits, or `None` when tracing is off.
    pub fn trace_id(&self) -> Option<String> {
        self.span
            .as_ref()
            .map(|s| s.span_context().trace_id().to_string())
    }

    /// The span's id as 16 hex digits, or `None` when tracing is off.
    pub fn span_id(&self) -> Option<String> {
        self.span
            .as_ref()
            .map(|s| s.span_context().span_id().to_string())
    }

    /// Sets an attribute on the span.
    ///
    /// # Arguments
    ///
    /// * `key` - Attribute key
    /// * `value` - Attribute value
    pub fn set_attribute(&mut self, key: &str, value: impl Into<opentelemetry::Value>) {
        if let Some(ref mut span) = self.span {
            span.set_attribute(KeyValue::new(key.to_string(), value.into()));
        }
    }

    /// Adds an event to the span.
    ///
    /// # Arguments
    ///
    /// * `name` - Event name
    pub fn add_event(&mut self, name: &str) {
        if let Some(ref mut span) = self.span {
            span.add_event(name.to_string(), vec![]);
        }
    }

    /// Records an error on the span.
    ///
    /// # Arguments
    ///
    /// * `error` - Error to record
    pub fn record_error(&mut self, error: &dyn std::error::Error) {
        if let Some(ref mut span) = self.span {
            span.record_error(error);
            span.set_status(Status::error(error.to_string()));
        }
    }

    /// Ends the span with success status.
    pub fn end(mut self) {
        if let Some(mut span) = self.span.take() {
            span.set_status(Status::Ok);
            span.end();
        }
    }
}

impl Drop for Span {
    fn drop(&mut self) {
        if let Some(mut span) = self.span.take() {
            span.end();
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use opentelemetry::trace::TracerProvider as _;
    use opentelemetry_sdk::trace::SdkTracerProvider;

    fn tracing() -> TelemetryTracing {
        install_propagator();
        let provider = SdkTracerProvider::builder().build();
        TelemetryTracing::new(Some(provider.tracer("test")))
    }

    // The header a telemetry-go service sends (W3C spec example ids).
    const TRACEPARENT: &str = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01";

    #[test]
    fn a_span_under_extracted_headers_continues_the_callers_trace() {
        let t = tracing();
        let headers = HashMap::from([("traceparent".to_string(), TRACEPARENT.to_string())]);
        let parent = t.extract(&headers);
        let span = t.start_span_with("handle", SpanKind::Server, Some(&parent));
        assert_eq!(
            span.trace_id().as_deref(),
            Some("4bf92f3577b34da6a3ce929d0e0e4736")
        );
        assert_ne!(span.span_id().as_deref(), Some("00f067aa0ba902b7"));

        let mut out = HashMap::new();
        t.inject(&span, &mut out);
        let sent = out.get("traceparent").expect("traceparent injected");
        assert!(
            sent.starts_with("00-4bf92f3577b34da6a3ce929d0e0e4736-"),
            "{sent}"
        );
        assert!(sent.ends_with("-01"), "{sent}");
        assert!(
            sent.contains(span.span_id().unwrap().as_str()),
            "the downstream parent is this span: {sent}"
        );
    }

    #[test]
    fn without_headers_a_new_trace_starts() {
        let t = tracing();
        let span = t.start_span_with(
            "root",
            SpanKind::Internal,
            Some(&t.extract(&HashMap::new())),
        );
        assert_ne!(
            span.trace_id().as_deref(),
            Some("4bf92f3577b34da6a3ce929d0e0e4736")
        );
        let child = t.start_span_with("child", SpanKind::Client, Some(&span.context()));
        assert_eq!(child.trace_id(), span.trace_id());
    }

    #[test]
    fn baggage_from_the_caller_goes_on_downstream() {
        let t = tracing();
        let headers = HashMap::from([
            ("traceparent".to_string(), TRACEPARENT.to_string()),
            ("baggage".to_string(), "tenant=acme".to_string()),
        ]);
        let parent = t.extract(&headers);
        assert_eq!(
            t.baggage(&parent).get("tenant").map(String::as_str),
            Some("acme")
        );
        let parent = t.set_baggage(&parent, "region", "eu");
        let span = t.start_span_with("handle", SpanKind::Server, Some(&parent));
        let mut out = HashMap::new();
        t.inject(&span, &mut out);
        let sent = out.get("baggage").expect("baggage injected");
        assert!(
            sent.contains("tenant=acme") && sent.contains("region=eu"),
            "{sent}"
        );
    }

    #[test]
    fn an_attached_span_is_current_for_the_logs_written_under_it() {
        let t = tracing();
        let span = t.start_span_with("work", SpanKind::Internal, None);
        {
            let _current = span.attach();
            let cx = Context::current();
            assert_eq!(
                Some(cx.span().span_context().trace_id().to_string()),
                span.trace_id(),
                "the log SDK reads the trace id from the current context"
            );
        }
        assert!(!Context::current().span().span_context().is_valid());
    }

    #[test]
    fn disabled_tracing_injects_nothing() {
        let t = TelemetryTracing::new(None);
        let span = t.start_span_with("x", SpanKind::Client, None);
        let mut out = HashMap::new();
        t.inject(&span, &mut out);
        assert!(out.is_empty());
        assert_eq!(span.trace_id(), None);
    }
}
