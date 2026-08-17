//! Custom log formatter for Telemetry with colored output and service information.
//!
//! This module provides a formatter that integrates with log4rs to produce
//! colored, structured log output with service metadata.

use chrono::Local;
use colored::Colorize;
use log::{Level, Record};
use log4rs::encode::{Encode, Write};
use serde_json::Value;
use std::sync::{Arc, Mutex};

/// Custom log formatter with colored output and service information.
///
/// This formatter produces log output in the format:
/// `TIMESTAMP LEVEL: <file:line> service (version | environment): message`
#[derive(Clone)]
pub struct TelemetryFormatter {
    service_name: Arc<Mutex<String>>,
    service_version: Arc<Mutex<String>>,
    service_environment: Arc<Mutex<String>>,
}

impl std::fmt::Debug for TelemetryFormatter {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("TelemetryFormatter").finish()
    }
}

impl Default for TelemetryFormatter {
    /// Creates a formatter with empty service metadata.
    ///
    /// # Examples
    ///
    /// ```
    /// let formatter = TelemetryFormatter::default();
    /// ```
    fn default() -> Self {
        Self::new()
    }
}

impl TelemetryFormatter {
    /// Creates a new formatter instance.
    ///
    /// # Examples
    ///
    /// ```no_run
    /// use telemetry::logging::TelemetryFormatter;
    ///
    /// let formatter = TelemetryFormatter::new();
    /// ```
    pub fn new() -> Self {
        Self {
            service_name: Arc::new(Mutex::new(String::new())),
            service_version: Arc::new(Mutex::new(String::new())),
            service_environment: Arc::new(Mutex::new(String::new())),
        }
    }

    /// Updates the service name, version, and deployment environment used by the formatter.
    ///
    /// # Arguments
    ///
    /// * `name` - Service name.
    /// * `version` - Service version.
    /// * `environment` - Deployment environment.
    ///
    /// # Examples
    ///
    /// ```
    /// let formatter = TelemetryFormatter::new();
    /// formatter.set_service_info(
    ///     "checkout".to_owned(),
    ///     "1.0.0".to_owned(),
    ///     "production".to_owned(),
    /// );
    /// ```
    pub fn set_service_info(&self, name: String, version: String, environment: String) {
        *self.service_name.lock().unwrap() = name;
        *self.service_version.lock().unwrap() = version;
        *self.service_environment.lock().unwrap() = environment;
    }
}

impl Encode for TelemetryFormatter {
    /// Formats a log record with timestamp, severity, source location, service metadata, and message.
    ///
    /// # Examples
    ///
    /// ```
    /// use log4rs::encode::Encode;
    /// use telemetry::logging::formatter::TelemetryFormatter;
    ///
    /// let formatter = TelemetryFormatter::new();
    /// let mut output = Vec::new();
    ///
    /// let record = log::Record::builder()
    ///     .level(log::Level::Info)
    ///     .file(Some("src/main.rs"))
    ///     .line(Some(10))
    ///     .args(format_args!("Service started"))
    ///     .build();
    ///
    /// formatter.encode(&mut output, &record)?;
    /// assert!(String::from_utf8(output)?.contains("Service started"));
    /// # Ok::<(), Box<dyn std::error::Error>>(())
    /// ```
    ///
    /// # Errors
    ///
    /// Returns an error if writing the formatted record fails.
    ///
    /// @returns `Ok(())` after the record is written, or the underlying write error.
    fn encode(&self, w: &mut dyn Write, record: &Record) -> anyhow::Result<()> {
        let timestamp = Local::now().format("%Y-%m-%dT%H:%M::%S");
        let level = record.level();
        let file_path = record.file().unwrap_or("unknown");
        let line = record.line().unwrap_or(0);

        let relative_path = if let Some(idx) = file_path.rfind("/telemetry-rs/") {
            &file_path[idx + 10..]
        } else if let Some(idx) = file_path.rfind("/src/") {
            &file_path[idx + 1..]
        } else if let Some(idx) = file_path.rfind("/examples/") {
            &file_path[idx + 1..]
        } else {
            file_path
        };

        let service_name = self.service_name.lock().unwrap();
        let service_version = self.service_version.lock().unwrap();
        let service_environment = self.service_environment.lock().unwrap();

        let level_colored = match level {
            Level::Error => "ERROR".red().bold(),
            Level::Warn => "WARNING".yellow().bold(),
            Level::Info => "INFO".green().bold(),
            Level::Debug => "DEBUG".blue().bold(),
            Level::Trace => "TRACE".purple().bold(),
        };

        let service_info = format!(
            "{} ({} | {})",
            service_name, service_version, service_environment
        )
        .cyan();

        writeln!(
            w,
            "{} {}: <{}:{}> {}: {}",
            timestamp,
            level_colored,
            relative_path,
            line,
            service_info,
            record.args()
        )?;

        Ok(())
    }
}

/// Formats structured data for pretty-printed output.
///
/// Converts JSON data into a formatted string with indentation and box-drawing characters.
///
/// # Arguments
///
/// * `data` - JSON value to format
///
/// # Examples
///
/// ```no_run
/// use telemetry::logging::formatter::format_data_output;
/// use serde_json::json;
///
/// let data = json!({"key": "value"});
/// let formatted = format_data_output(&data);
/// ```
pub fn format_data_output(data: &Value) -> String {
    let json_str = serde_json::to_string_pretty(data).unwrap_or_default();
    let mut output = String::from("  data=\n");

    for line in json_str.lines() {
        output.push_str("  │ ");
        output.push_str(line);
        output.push('\n');
    }

    output
}
