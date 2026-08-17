//! OpenTelemetry logger for distributed tracing and logging.
//!
//! This module provides a wrapper around the OpenTelemetry SDK logger
//! for sending logs to OpenTelemetry collectors.

use opentelemetry::KeyValue;
use opentelemetry::logs::Logger as _;
use opentelemetry::logs::Severity;
use opentelemetry::logs::{AnyValue, LogRecord as _};
use opentelemetry_sdk::logs::SdkLogger;
use std::sync::Arc;
use std::time::SystemTime;

/// OpenTelemetry logger wrapper.
///
/// Provides a simplified interface for logging to OpenTelemetry backends.
pub struct OtelLogger {
    logger: Arc<SdkLogger>,
}

impl OtelLogger {
    /// Creates a new OpenTelemetry logger.
    ///
    /// # Arguments
    ///
    /// * `logger` - SDK logger instance from OpenTelemetry
    pub fn new(logger: SdkLogger) -> Self {
        Self {
            logger: Arc::new(logger),
        }
    }

    /// Logs a message with the specified severity and attributes.
    ///
    /// # Arguments
    ///
    /// * `severity` - Log severity level
    /// * `message` - Log message
    /// * `attributes` - Key-value attributes to attach
    pub fn log(&self, severity: Severity, message: &str, attributes: Vec<KeyValue>) {
        let mut record = self.logger.create_log_record();

        record.set_timestamp(SystemTime::now());
        record.set_observed_timestamp(SystemTime::now());
        record.set_severity_number(severity);
        record.set_body(AnyValue::from(message.to_string()));

        for kv in attributes {
            record.add_attribute(kv.key.clone(), kv.value.to_string());
        }

        self.logger.emit(record);
    }

    /// Logs a debug-level message.
    pub fn debug(&self, message: &str, attributes: Vec<KeyValue>) {
        self.log(Severity::Debug, message, attributes);
    }

    /// Logs an info-level message.
    pub fn info(&self, message: &str, attributes: Vec<KeyValue>) {
        self.log(Severity::Info, message, attributes);
    }

    /// Logs a warning-level message.
    pub fn warn(&self, message: &str, attributes: Vec<KeyValue>) {
        self.log(Severity::Warn, message, attributes);
    }

    /// Logs an error-level message.
    pub fn error(&self, message: &str, attributes: Vec<KeyValue>) {
        self.log(Severity::Error, message, attributes);
    }

    /// Logs a fatal-level message.
    pub fn fatal(&self, message: &str, attributes: Vec<KeyValue>) {
        self.log(Severity::Fatal, message, attributes);
    }
}
