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
    /// Creates a new OpenTelemetry logger wrapper.
    ///
    /// # Arguments
    ///
    /// * `logger` - The SDK logger used to emit log records.
    ///
    /// # Examples
    ///
    /// ```no_run
    /// let logger = SdkLogger::default();
    /// let otel_logger = OtelLogger::new(logger);
    /// ```
    pub fn new(logger: SdkLogger) -> Self {
        Self {
            logger: Arc::new(logger),
        }
    }

    /// Emits a message at the specified severity with its associated attributes.
    ///
    /// # Arguments
    ///
    /// * `severity` - Severity level assigned to the log record.
    /// * `message` - Message stored in the log record.
    /// * `attributes` - Key-value attributes attached to the log record.
    ///
    /// # Examples
    ///
    /// ```
    /// # use opentelemetry::logs::{Logger, LoggerProvider, Severity};
    /// # use opentelemetry_sdk::logs::SdkLoggerProvider;
    /// # let provider = SdkLoggerProvider::builder().build();
    /// # let logger = OtelLogger::new(provider.logger("example"));
    /// logger.log(Severity::Info, "Service started", vec![]);
    /// ```
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

    /// Logs a debug-level message with the provided attributes.
    ///
    /// # Arguments
    ///
    /// * `message` - The message to log.
    /// * `attributes` - Attributes to attach to the log record.
    ///
    /// # Examples
    ///
    /// ```no_run
    /// let logger: OtelLogger = unimplemented!();
    /// logger.debug("Connection established", Vec::new());
    /// ```
    pub fn debug(&self, message: &str, attributes: Vec<KeyValue>) {
        self.log(Severity::Debug, message, attributes);
    }

    /// Logs an info-level message with the provided attributes.
    ///
    /// # Examples
    ///
    /// ```no_run
    /// # let logger: OtelLogger = todo!();
    /// logger.info("Application started", vec![]);
    /// ```
    pub fn info(&self, message: &str, attributes: Vec<KeyValue>) {
        self.log(Severity::Info, message, attributes);
    }

    /// Logs a warning-level message with the supplied attributes.
    ///
    /// # Examples
    ///
    /// ```no_run
    /// # let logger: OtelLogger = todo!();
    /// logger.warn("Cache miss", vec![]);
    /// ```
    ///
    /// `attributes` provides additional key-value context for the log record.
    pub fn warn(&self, message: &str, attributes: Vec<KeyValue>) {
        self.log(Severity::Warn, message, attributes);
    }

    /// Logs a message with error severity and its associated attributes.
    ///
    /// # Examples
    ///
    /// ```no_run
    /// # let logger: OtelLogger = todo!();
    /// logger.error("Request failed", vec![]);
    /// ```
    pub fn error(&self, message: &str, attributes: Vec<KeyValue>) {
        self.log(Severity::Error, message, attributes);
    }

    /// Records a fatal-level message with optional attributes.
    ///
    /// # Examples
    ///
    /// ```
    /// # fn example(logger: &OtelLogger) {
    /// logger.fatal("Service unavailable", vec![]);
    /// # }
    /// ```
    pub fn fatal(&self, message: &str, attributes: Vec<KeyValue>) {
        self.log(Severity::Fatal, message, attributes);
    }
}
