//! Declarative macros for quick OTLP setup (aligned with `telemetry-go` builder ergonomics).
//!
//! Default collector address for the `telemetry-examples` workspace crate is **gRPC OTLP on port 6009**.
//! Configure your OpenTelemetry Collector to receive OTLP gRPC on that port.

/// Default OTLP **gRPC** port for a local collector used by `telemetry-examples`.
///
/// Standard ports are 4317 (gRPC) and 4318 (HTTP). This project uses **6009** so examples
/// can run alongside a default collector without port clashes.
pub const DEFAULT_OTEL_COLLECTOR_OTLP_PORT: u16 = 12_005;

/// Starts a [`crate::TelemetryBuilder`] pointed at `localhost` and [`DEFAULT_OTEL_COLLECTOR_OTLP_PORT`].
///
/// ```ignore
/// let _telemetry = telemetry::telemetry_local_otel!()
///     .with_service("my-svc", "1.0.0")
///     .with_tracing()
///     .build()?;
/// ```
#[macro_export]
macro_rules! telemetry_local_otel {
    () => {
        $crate::Telemetry::new().with_otlp("localhost", $crate::DEFAULT_OTEL_COLLECTOR_OTLP_PORT)
    };
}

/// Same as [`telemetry_local_otel!`] but from an explicit service name/version builder base.
#[macro_export]
macro_rules! telemetry_local_otel_service {
    ($name:expr, $version:expr) => {
        $crate::telemetry_local_otel!().with_service($name, $version)
    };
}
