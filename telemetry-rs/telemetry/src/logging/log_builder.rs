//! Log builder for fluent API logging with automatic location tracking.
//!
//! This module provides a builder pattern for creating log entries with optional data.
//! The builder automatically captures file location and line numbers, and logs are
//! emitted when the builder is dropped.
//!
//! # Examples
//!
//! ```no_run
//! use telemetry::info;
//! use serde::Serialize;
//!
//! #[derive(Serialize)]
//! struct UserData {
//!     user_id: u32,
//!     action: String,
//! }
//!
//! // Simple log without data
//! info!("User logged in");
//!
//! // Log with structured data
//! let data = UserData {
//!     user_id: 123,
//!     action: "login".to_string(),
//! };
//! info!("User action recorded").with_data(&data);
//! ```

use opentelemetry::logs::Severity;
use serde::Serialize;
use serde_json::Value;

/// Builder for creating log entries with optional structured data.
///
/// This builder uses the Drop trait to automatically emit logs when it goes out of scope.
/// This allows for a fluent API where you can chain method calls to add data to the log.
pub struct LogBuilder {
    level: Severity,
    message: String,
    file: &'static str,
    line: u32,
    data: Option<Value>,
}

impl LogBuilder {
    /// Creates a new log builder with the specified severity level and message.
    ///
    /// # Arguments
    ///
    /// * `level` - The severity level of the log entry
    /// * `message` - The log message
    /// * `file` - The source file where the log was created (typically from `file!()` macro)
    /// * `line` - The line number where the log was created (typically from `line!()` macro)
    ///
    /// # Examples
    ///
    /// ```no_run
    /// use telemetry::logging::LogBuilder;
    /// use opentelemetry::logs::Severity;
    ///
    /// let builder = LogBuilder::new(
    ///     Severity::Info,
    ///     "Application started".to_string(),
    ///     file!(),
    ///     line!()
    /// );
    /// ```
    pub fn new(level: Severity, message: String, file: &'static str, line: u32) -> Self {
        Self {
            level,
            message,
            file,
            line,
            data: None,
        }
    }

    /// Attaches structured data to the log entry.
    ///
    /// The data will be serialized to JSON and included in the log output.
    ///
    /// # Arguments
    ///
    /// * `data` - Any serializable data to attach to the log entry
    ///
    /// # Examples
    ///
    /// ```no_run
    /// use telemetry::info;
    /// use serde::Serialize;
    ///
    /// #[derive(Serialize)]
    /// struct RequestInfo {
    ///     method: String,
    ///     path: String,
    /// }
    ///
    /// let request = RequestInfo {
    ///     method: "GET".to_string(),
    ///     path: "/api/users".to_string(),
    /// };
    ///
    /// info!("HTTP request received").with_data(&request);
    /// ```
    pub fn with_data<T: Serialize>(mut self, data: &T) -> Self {
        self.data = serde_json::to_value(data).ok();
        self
    }
}

impl Drop for LogBuilder {
    /// Automatically emits the log entry when the builder is dropped.
    ///
    /// This is called automatically when the builder goes out of scope,
    /// ensuring that logs are always emitted even if the user forgets to
    /// explicitly call a logging method.
    fn drop(&mut self) {
        if let Some(logger) = super::get() {
            logger.log_with_location(
                self.level,
                self.message.clone(),
                self.data.take(),
                self.file,
                self.line,
            );
        }
    }
}
