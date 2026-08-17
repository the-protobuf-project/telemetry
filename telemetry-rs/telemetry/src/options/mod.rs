//! Configuration options for Telemetry.
//!
//! This module contains all configuration structures for setting up
//! Telemetry with various backends and features.

pub mod foxglove;
pub mod logging;
pub mod opentelemetry;
pub mod profiling;
pub mod service;
pub mod telemetry;
pub mod tracing;

pub use foxglove::FoxgloveOptions;
pub use logging::{LogLevel, LogOptions, LoggingOptions, ModuleOptions, TimeFormat};
pub use opentelemetry::{
    LoggingTelemetryOptions, MetricsTelemetryOptions, OTLPOptions, OpenTelemetryOptions,
    OtelOptions, TracingTelemetryOptions,
};
pub use profiling::ProfilingOptions;
pub use service::{Environment, ServiceOptions};
pub use telemetry::TelemetryOptions;
pub use tracing::TracingOptions;
