//! Main Telemetry configuration options.
//!
//! Aggregates all configuration options for the Telemetry library.

use super::{
    FoxgloveOptions, LoggingOptions, OpenTelemetryOptions, ProfilingOptions, TracingOptions,
};
use serde::{Deserialize, Serialize};

/// Main configuration options for Telemetry.
///
/// # Examples
///
/// ```no_run
/// use telemetry::options::{TelemetryOptions, OpenTelemetryOptions, FoxgloveOptions};
///
/// let opts = TelemetryOptions::new()
///     .with_telemetry(OpenTelemetryOptions::default())
///     .with_foxglove(FoxgloveOptions::new("output.mcap"));
/// ```
#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct TelemetryOptions {
    /// Logging options for the service.
    #[serde(default)]
    pub logging: LoggingOptions,
    /// Foxglove options for the service.
    #[serde(default)]
    pub foxglove: FoxgloveOptions,
    /// Unified telemetry options (OpenTelemetry-based).
    #[serde(default)]
    pub telemetry: OpenTelemetryOptions,
    /// Continuous profiling options (Pyroscope).
    #[serde(default)]
    pub profiling: ProfilingOptions,
    /// Distributed tracing options.
    #[serde(default)]
    pub tracing: TracingOptions,
}

impl TelemetryOptions {
    /// Creates telemetry options with their default configuration.
    ///
    /// # Examples
    ///
    /// ```
    /// use telemetry::options::TelemetryOptions;
    ///
    /// let options = TelemetryOptions::new();
    /// ```
    pub fn new() -> Self {
        Self::default()
    }

    /// Replaces the logging configuration.
    ///
    /// # Examples
    ///
    /// ```
    /// use telemetry::options::{LoggingOptions, TelemetryOptions};
    ///
    /// let options = TelemetryOptions::new()
    ///     .with_logging(LoggingOptions::default());
    /// ```
    pub fn with_logging(mut self, logging: LoggingOptions) -> Self {
        self.logging = logging;
        self
    }

    /// Replaces the telemetry configuration.
    ///
    /// # Examples
    ///
    /// ```
    /// use telemetry::options::{OpenTelemetryOptions, TelemetryOptions};
    ///
    /// let telemetry = OpenTelemetryOptions::default();
    /// let options = TelemetryOptions::new().with_telemetry(telemetry);
    /// ```
    pub fn with_telemetry(mut self, telemetry: OpenTelemetryOptions) -> Self {
        self.telemetry = telemetry;
        self
    }

    /// Sets Foxglove configuration.
    pub fn with_foxglove(mut self, foxglove: FoxgloveOptions) -> Self {
        self.foxglove = foxglove;
        self
    }

    /// Sets profiling configuration.
    pub fn with_profiling(mut self, profiling: ProfilingOptions) -> Self {
        self.profiling = profiling;
        self
    }

    /// Sets tracing configuration.
    pub fn with_tracing(mut self, tracing: TracingOptions) -> Self {
        self.tracing = tracing;
        self
    }
}
