//! Distributed tracing with OpenTelemetry.
//!
//! This module provides integration with OpenTelemetry for distributed tracing,
//! supporting both tokio-rs/tracing instrumentation and manual span management.

use anyhow::Result;
use opentelemetry::KeyValue;
use opentelemetry::trace::Status;

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
}

impl Span {
    /// Creates a new span (internal use).
    pub(crate) fn new(
        tracer: Option<&opentelemetry_sdk::trace::Tracer>,
        name: &'static str,
    ) -> Self {
        let span = tracer.map(|t| {
            use opentelemetry::trace::Tracer;
            t.start(name)
        });

        Self { span }
    }

    /// Sets an attribute on the span.
    ///
    /// # Arguments
    ///
    /// * `key` - Attribute key
    /// * `value` - Attribute value
    pub fn set_attribute(&mut self, key: &str, value: impl Into<opentelemetry::Value>) {
        if let Some(ref mut span) = self.span {
            use opentelemetry::trace::Span;
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
            use opentelemetry::trace::Span;
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
            use opentelemetry::trace::Span;
            span.record_error(error);
            span.set_status(Status::error(error.to_string()));
        }
    }

    /// Ends the span with success status.
    pub fn end(mut self) {
        if let Some(mut span) = self.span.take() {
            use opentelemetry::trace::Span;
            span.set_status(Status::Ok);
            span.end();
        }
    }
}

impl Drop for Span {
    fn drop(&mut self) {
        if let Some(mut span) = self.span.take() {
            use opentelemetry::trace::Span;
            span.end();
        }
    }
}
