//! Logger implementation for structured logging with multiple backends.
//!
//! This module provides the `Logger` struct which handles logging to console,
//! MCAP files, and OpenTelemetry backends simultaneously.

use super::LogMcapWriter;
use super::OtelLogger;
use anyhow::Result;
use opentelemetry::KeyValue;
use opentelemetry::logs::Severity;
use serde::Serialize;
use serde_json::Value;
use std::sync::Arc;

/// Logger that writes to multiple backends simultaneously.
///
/// The logger can write to:
/// - Console output (via log4rs)
/// - MCAP files for recording
/// - OpenTelemetry for distributed tracing
///
/// # Examples
///
/// ```no_run
/// use telemetry::Logger;
///
/// let logger = Logger::new(
///     "my-service".to_string(),
///     "1.0.0".to_string(),
///     "production".to_string(),
///     None,
///     None,
/// );
///
/// logger.info("Application started", None::<()>);
/// ```
pub struct Logger {
    mcap_writer: Option<Arc<LogMcapWriter>>,
    otel_logger: Option<Arc<OtelLogger>>,
    service_name: String,
    service_version: String,
    service_environment: String,
}

impl Logger {
    /// Creates a new logger instance.
    ///
    /// # Arguments
    ///
    /// * `service_name` - Name of the service
    /// * `service_version` - Version of the service
    /// * `service_environment` - Deployment environment
    /// * `mcap_writer` - Optional MCAP writer for recording logs
    /// * `otel_logger` - Optional OpenTelemetry logger
    pub fn new(
        service_name: String,
        service_version: String,
        service_environment: String,
        mcap_writer: Option<LogMcapWriter>,
        otel_logger: Option<OtelLogger>,
    ) -> Self {
        Self {
            mcap_writer: mcap_writer.map(Arc::new),
            otel_logger: otel_logger.map(Arc::new),
            service_name,
            service_version,
            service_environment,
        }
    }

    /// Serializes data to JSON value.
    fn serialize_data<T: Serialize>(&self, data: &T) -> Option<Value> {
        serde_json::to_value(data).ok()
    }

    /// Creates OpenTelemetry attributes for a log entry.
    fn otel_attributes(&self, data: Option<&Value>, file: &str, line: u32) -> Vec<KeyValue> {
        let mut attrs = vec![
            KeyValue::new("service.name", self.service_name.clone()),
            KeyValue::new("service.version", self.service_version.clone()),
            KeyValue::new("deployment.environment", self.service_environment.clone()),
            KeyValue::new("code.filepath", file.to_string()),
            KeyValue::new("code.lineno", line as i64),
        ];

        if let Some(v) = data {
            attrs.push(KeyValue::new("data", v.to_string()));
        }

        attrs
    }

    /// Returns a reference to the OpenTelemetry logger if configured.
    pub fn otel_logger(&self) -> Option<&Arc<OtelLogger>> {
        self.otel_logger.as_ref()
    }

    /// Logs a debug-level message with optional structured data.
    ///
    /// # Arguments
    ///
    /// * `message` - The log message
    /// * `data` - Optional structured data to attach
    pub fn debug<T: Serialize>(&self, message: &str, data: Option<T>) {
        log::debug!(target: module_path!(), "{}", message);

        let data_value = data.as_ref().and_then(|d| self.serialize_data(d));

        if let Some(value) = data_value.as_ref() {
            print!("{}", super::formatter::format_data_output(value));
        }

        if let Some(ref mcap) = self.mcap_writer {
            let _ = mcap.write_log("debug", message, "unknown", 0, data_value.clone());
        }

        if let Some(ref otel) = self.otel_logger {
            otel.log(
                Severity::Debug,
                message,
                self.otel_attributes(data_value.as_ref(), "unknown", 0),
            );
        }
    }

    /// Logs an info-level message with optional structured data.
    ///
    /// # Arguments
    ///
    /// * `message` - The log message
    /// * `data` - Optional structured data to attach
    pub fn info<T: Serialize>(&self, message: &str, data: Option<T>) {
        log::info!(target: module_path!(), "{}", message);

        let data_value = data.as_ref().and_then(|d| self.serialize_data(d));

        if let Some(value) = data_value.as_ref() {
            print!("{}", super::formatter::format_data_output(value));
        }

        if let Some(ref mcap) = self.mcap_writer {
            let _ = mcap.write_log("info", message, "unknown", 0, data_value.clone());
        }

        if let Some(ref otel) = self.otel_logger {
            otel.log(
                Severity::Info,
                message,
                self.otel_attributes(data_value.as_ref(), "unknown", 0),
            );
        }
    }

    /// Logs a warning-level message with optional structured data.
    ///
    /// # Arguments
    ///
    /// * `message` - The log message
    /// * `data` - Optional structured data to attach
    pub fn warn<T: Serialize>(&self, message: &str, data: Option<T>) {
        log::warn!(target: module_path!(), "{}", message);

        let data_value = data.as_ref().and_then(|d| self.serialize_data(d));

        if let Some(value) = data_value.as_ref() {
            print!("{}", super::formatter::format_data_output(value));
        }

        if let Some(ref mcap) = self.mcap_writer {
            let _ = mcap.write_log("warn", message, "unknown", 0, data_value.clone());
        }

        if let Some(ref otel) = self.otel_logger {
            otel.log(
                Severity::Warn,
                message,
                self.otel_attributes(data_value.as_ref(), "unknown", 0),
            );
        }
    }

    /// Logs an error-level message with optional structured data.
    ///
    /// # Arguments
    ///
    /// * `message` - The log message
    /// * `data` - Optional structured data to attach
    pub fn error<T: Serialize>(&self, message: &str, data: Option<T>) -> Result<()> {
        log::error!(target: module_path!(), "{}", message);

        let data_value = data.as_ref().and_then(|d| self.serialize_data(d));

        if let Some(value) = data_value.as_ref() {
            print!("{}", super::formatter::format_data_output(value));
        }

        if let Some(ref mcap) = self.mcap_writer {
            let _ = mcap.write_log("error", message, "unknown", 0, data_value.clone());
        }

        if let Some(ref otel) = self.otel_logger {
            otel.log(
                Severity::Error,
                message,
                self.otel_attributes(data_value.as_ref(), "unknown", 0),
            );
        }

        Ok(())
    }

    /// Logs a fatal-level message with optional structured data.
    ///
    /// # Arguments
    ///
    /// * `message` - The log message
    /// * `data` - Optional structured data to attach
    pub fn fatal<T: Serialize>(&self, message: &str, data: Option<T>) {
        log::error!(target: module_path!(), "FATAL: {}", message);

        let data_value = data.as_ref().and_then(|d| self.serialize_data(d));

        if let Some(value) = data_value.as_ref() {
            print!("{}", super::formatter::format_data_output(value));
        }

        if let Some(ref mcap) = self.mcap_writer {
            let _ = mcap.write_log("fatal", message, "unknown", 0, data_value.clone());
        }

        if let Some(ref otel) = self.otel_logger {
            otel.log(
                Severity::Fatal,
                message,
                self.otel_attributes(data_value.as_ref(), "unknown", 0),
            );
        }
    }

    /// Returns the service name.
    pub fn service_name(&self) -> &str {
        &self.service_name
    }

    /// Returns the service version.
    pub fn service_version(&self) -> &str {
        &self.service_version
    }

    /// Returns the service environment.
    pub fn service_environment(&self) -> &str {
        &self.service_environment
    }

    /// Returns a reference to the MCAP writer if configured.
    pub fn mcap_writer(&self) -> Option<&Arc<LogMcapWriter>> {
        self.mcap_writer.as_ref()
    }

    /// Returns a cloned Arc to the MCAP writer if configured.
    pub fn mcap_writer_arc(&self) -> Option<Arc<LogMcapWriter>> {
        self.mcap_writer.clone()
    }

    /// Returns a cloned Arc to the OpenTelemetry logger if configured.
    pub fn otel_logger_arc(&self) -> Option<Arc<OtelLogger>> {
        self.otel_logger.clone()
    }
}
