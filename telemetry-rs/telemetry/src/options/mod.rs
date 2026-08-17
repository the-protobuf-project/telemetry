//! Configuration options for Telemetry.
//!
//! This module contains all configuration structures for setting up
//! Telemetry with various backends and features.

pub mod foxglove;
pub mod logging;
pub mod telemetry;
pub mod profiling;
pub mod service;
pub mod opentelemetry;
pub mod tracing;

pub use foxglove::FoxgloveOptions;
pub use logging::{LogLevel, LogOptions, LoggingOptions, ModuleOptions, TimeFormat};
pub use telemetry::TelemetryOptions;
pub use profiling::ProfilingOptions;
pub use service::{Environment, ServiceOptions};
pub use opentelemetry::{
    LoggingTelemetryOptions, MetricsTelemetryOptions, OTLPOptions, OtelOptions, OpenTelemetryOptions,
    TracingTelemetryOptions,
};
pub use tracing::TracingOptions;
