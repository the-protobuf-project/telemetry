//! Global logger for application-wide logging.
//!
//! This module provides a global logger instance that can be accessed from anywhere
//! in the application. It integrates with multiple backends including console output,
//! MCAP files, and OpenTelemetry.
//!
//! # Examples
//!
//! ```no_run
//! use telemetry::logging::{GlobalLogger, init, get};
//! use telemetry::info;
//!
//! // Initialize the global logger (typically done once at startup)
//! let logger = GlobalLogger::new(
//!     "my-service".to_string(),
//!     "1.0.0".to_string(),
//!     "production".to_string(),
//!     None,
//!     None,
//! );
//! init(logger);
//!
//! // Use the logger from anywhere in the application
//! info!("Application started");
//! ```

use chrono::Local;
use colored::Colorize;
use opentelemetry::KeyValue;
use opentelemetry::logs::Severity;
use serde_json::Value;
use std::cell::RefCell;
use std::fmt::Write as _;
use std::io::Write as _;
use std::sync::Arc;
use std::sync::OnceLock;
use std::time::{SystemTime, UNIX_EPOCH};

use super::{LogMcapWriter, OtelLogger, formatter};

pub use super::log_builder::LogBuilder;

static GLOBAL_LOGGER: OnceLock<Arc<GlobalLogger>> = OnceLock::new();

/// Global logger that handles logging to multiple backends.
///
/// The global logger writes logs to:
/// - Console output with colored formatting
/// - MCAP files (if configured)
/// - OpenTelemetry (if configured)
pub struct GlobalLogger {
    mcap_writer: Option<Arc<LogMcapWriter>>,
    otel_logger: Option<Arc<OtelLogger>>,
    // "name (version | environment)", colored once rather than on every line.
    service_info: String,
    console: bool,
}

thread_local! {
    // The console line being written, reused across calls.
    static LINE: RefCell<String> = const { RefCell::new(String::new()) };
    // The console timestamp and the second it shows: formatting a local time
    // looks up the time zone, so it is done once a second, not once a line.
    static STAMP: RefCell<(u64, String)> = const { RefCell::new((u64::MAX, String::new())) };
}

impl GlobalLogger {
    /// Creates a new global logger instance.
    ///
    /// # Arguments
    ///
    /// * `service_name` - Name of the service
    /// * `service_version` - Version of the service
    /// * `service_environment` - Deployment environment (e.g., "production", "development")
    /// * `mcap_writer` - Optional MCAP writer for recording logs to files
    /// * `otel_logger` - Optional OpenTelemetry logger for distributed tracing
    ///
    /// # Examples
    ///
    /// ```no_run
    /// use telemetry::logging::GlobalLogger;
    ///
    /// let logger = GlobalLogger::new(
    ///     "my-service".to_string(),
    ///     "1.0.0".to_string(),
    ///     "production".to_string(),
    ///     None,
    ///     None,
    /// );
    /// ```
    pub fn new(
        service_name: String,
        service_version: String,
        service_environment: String,
        mcap_writer: Option<Arc<LogMcapWriter>>,
        otel_logger: Option<Arc<OtelLogger>>,
    ) -> Self {
        let service_info = format!("{service_name} ({service_version} | {service_environment})")
            .cyan()
            .to_string();
        Self {
            mcap_writer,
            otel_logger,
            service_info,
            console: true,
        }
    }

    /// Prints each line to stdout, or not; on by default.
    pub fn with_console(mut self, console: bool) -> Self {
        self.console = console;
        self
    }

    /// Creates OpenTelemetry attributes for a log entry. The service name,
    /// version and environment are not repeated here: they are resource
    /// attributes, sent once per batch.
    fn otel_attributes(
        &self,
        data: Option<&Value>,
        file: &'static str,
        line: u32,
    ) -> Vec<KeyValue> {
        let mut attrs = Vec::with_capacity(3);
        attrs.push(KeyValue::new("code.filepath", file));
        attrs.push(KeyValue::new("code.lineno", line as i64));

        if let Some(v) = data {
            attrs.push(KeyValue::new("data", v.to_string()));
        }

        attrs
    }

    /// Logs a message with its source location to the console and configured backends.
    ///
    /// # Arguments
    ///
    /// * `level` - Severity of the message.
    /// * `message` - Message text to log.
    /// * `data` - Optional structured data associated with the message.
    /// * `file` - Source file containing the log call.
    /// * `line` - Source line containing the log call.
    ///
    /// # Examples
    ///
    /// ```
    /// # use opentelemetry::logs::Severity;
    /// # use telemetry::logging::GlobalLogger;
    /// # let logger = GlobalLogger::new(
    /// #     "example".to_owned(),
    /// #     "1.0.0".to_owned(),
    /// #     "development".to_owned(),
    /// #     None,
    /// #     None,
    /// # );
    /// logger.log_with_location(
    ///     Severity::Info,
    ///     "Service started".to_owned(),
    ///     None,
    ///     file!(),
    ///     line!(),
    /// );
    /// ```
    pub fn log_with_location(
        &self,
        level: Severity,
        message: String,
        data: Option<Value>,
        file: &'static str,
        line: u32,
    ) {
        let level_str = match level {
            Severity::Debug => "debug",
            Severity::Info => "info",
            Severity::Warn => "warn",
            Severity::Error => "error",
            Severity::Fatal => "fatal",
            _ => "info",
        };

        if self.console {
            self.print(level, &message, data.as_ref(), file, line);
        }

        if let Some(ref mcap) = self.mcap_writer {
            let _ = mcap.write_log(level_str, &message, file, line, data.clone());
        }

        if let Some(ref otel) = self.otel_logger {
            otel.log(
                level,
                &message,
                self.otel_attributes(data.as_ref(), file, line),
            );
        }
    }

    /// Writes one line to stdout in a single write, formatted into a reused
    /// buffer.
    fn print(&self, level: Severity, message: &str, data: Option<&Value>, file: &str, line: u32) {
        let relative_path = if let Some(idx) = file.rfind("/telemetry-rs/") {
            &file[idx + 10..]
        } else if let Some(idx) = file.rfind("/src/") {
            &file[idx + 1..]
        } else if let Some(idx) = file.rfind("/examples/") {
            &file[idx + 1..]
        } else {
            file
        };
        let level_colored = match level {
            Severity::Error | Severity::Fatal => "ERROR".red().bold(),
            Severity::Warn => "WARNING".yellow().bold(),
            Severity::Info => "INFO".green().bold(),
            Severity::Debug => "DEBUG".blue().bold(),
            _ => "INFO".green().bold(),
        };
        LINE.with_borrow_mut(|out| {
            out.clear();
            STAMP.with_borrow_mut(|(second, stamp)| {
                let now = SystemTime::now()
                    .duration_since(UNIX_EPOCH)
                    .map_or(0, |d| d.as_secs());
                if *second != now {
                    *second = now;
                    stamp.clear();
                    let _ = write!(stamp, "{}", Local::now().format("%Y-%m-%dT%H:%M:%S"));
                }
                out.push_str(stamp);
            });
            let _ = writeln!(
                out,
                " {level_colored}: <{relative_path}:{line}> {}: {message}",
                self.service_info
            );
            if let Some(value) = data {
                out.push_str(&formatter::format_data_output(value));
            }
            let _ = std::io::stdout().lock().write_all(out.as_bytes());
        });
    }
}

/// Initializes the global logger.
///
/// This should be called once at application startup. Subsequent calls will be ignored.
///
/// # Examples
///
/// ```no_run
/// use telemetry::logging::{GlobalLogger, init};
///
/// let logger = GlobalLogger::new(
///     "my-service".to_string(),
///     "1.0.0".to_string(),
///     "production".to_string(),
///     None,
///     None,
/// );
/// init(logger);
/// ```
pub fn init(logger: GlobalLogger) {
    let _ = GLOBAL_LOGGER.set(Arc::new(logger));
}

/// Gets a reference to the global logger.
///
/// Returns `None` if the logger has not been initialized.
///
/// # Examples
///
/// ```no_run
/// use telemetry::logging::get;
///
/// if let Some(logger) = get() {
///     // Use the logger
/// }
/// ```
pub fn get() -> Option<&'static Arc<GlobalLogger>> {
    GLOBAL_LOGGER.get()
}

/// Creates a debug-level log entry.
///
/// This macro captures the file location and line number automatically.
/// Returns a `LogBuilder` that can be used to attach structured data.
#[macro_export]
macro_rules! debug {
    ($($arg:tt)*) => {
        $crate::logging::global::LogBuilder::new(
            $crate::opentelemetry::logs::Severity::Debug,
            format!($($arg)*),
            file!(),
            line!(),
        )
    };
}

/// Creates an info-level log entry.
///
/// This macro captures the file location and line number automatically.
/// Returns a `LogBuilder` that can be used to attach structured data.
#[macro_export]
macro_rules! info {
    ($($arg:tt)*) => {
        $crate::logging::global::LogBuilder::new(
            $crate::opentelemetry::logs::Severity::Info,
            format!($($arg)*),
            file!(),
            line!(),
        )
    };
}

/// Creates a warning-level log entry.
///
/// This macro captures the file location and line number automatically.
/// Returns a `LogBuilder` that can be used to attach structured data.
#[macro_export]
macro_rules! log_warn {
    ($($arg:tt)*) => {
        $crate::logging::global::LogBuilder::new(
            $crate::opentelemetry::logs::Severity::Warn,
            format!($($arg)*),
            file!(),
            line!(),
        )
    };
}

/// Creates an error-level log entry.
///
/// This macro captures the file location and line number automatically.
/// Returns a `LogBuilder` that can be used to attach structured data.
#[macro_export]
macro_rules! log_error {
    ($($arg:tt)*) => {
        $crate::logging::global::LogBuilder::new(
            $crate::opentelemetry::logs::Severity::Error,
            format!($($arg)*),
            file!(),
            line!(),
        )
    };
}

pub use debug;
pub use info;
pub use log_error as error;
pub use log_warn as warn;
