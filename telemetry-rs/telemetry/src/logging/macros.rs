//! Logging macros for use with Logger instances.
//!
//! These macros provide convenient logging with automatic file and line capture
//! for use with `Logger` instances. They integrate with MCAP and OpenTelemetry backends.
//!
//! # Examples
//!
//! ```no_run
//! use telemetry::{telemetry_info, Logger};
//!
//! let logger = Logger::new(
//!     "my-service".to_string(),
//!     "1.0.0".to_string(),
//!     "production".to_string(),
//!     None,
//!     None,
//! );
//!
//! telemetry_info!(logger, "Application started");
//! ```

/// Logs a debug-level message using a Logger instance.
///
/// Captures file location and line number automatically.
#[macro_export]
macro_rules! telemetry_debug {
    ($logger:expr, $msg:expr) => {
        log::debug!(target: module_path!(), "{}", $msg);
        if let Some(ref otel) = $logger.otel_logger() {
            otel.debug(
                $msg,
                vec![
                    $crate::opentelemetry::KeyValue::new("code.filepath", file!().to_string()),
                    $crate::opentelemetry::KeyValue::new("code.lineno", line!() as i64),
                ],
            );
        }
    };
    ($logger:expr, $msg:expr, $data:expr) => {
        log::debug!(target: module_path!(), "{}", $msg);
        let data_value: Option<serde_json::Value> = $data.as_ref().and_then(|d| serde_json::to_value(d).ok());
        if let Some(ref value) = data_value {
            print!("{}", $crate::logging::formatter::format_data_output(value));
        }
        if let Some(ref mcap) = $logger.mcap_writer() {
            let _ = mcap.write_log("debug", $msg, file!(), line!(), data_value.clone());
        }
        if let Some(ref otel) = $logger.otel_logger() {
            let mut attrs = vec![
                $crate::opentelemetry::KeyValue::new("code.filepath", file!().to_string()),
                $crate::opentelemetry::KeyValue::new("code.lineno", line!() as i64),
            ];
            if let Some(ref v) = data_value {
                attrs.push($crate::opentelemetry::KeyValue::new("data", v.to_string()));
            }
            otel.debug($msg, attrs);
        }
    };
}

#[macro_export]
macro_rules! telemetry_info {
    ($logger:expr, $msg:expr) => {
        log::info!(target: module_path!(), "{}", $msg);
        if let Some(ref otel) = $logger.otel_logger() {
            otel.info(
                $msg,
                vec![
                    $crate::opentelemetry::KeyValue::new("code.filepath", file!().to_string()),
                    $crate::opentelemetry::KeyValue::new("code.lineno", line!() as i64),
                ],
            );
        }
    };
    ($logger:expr, $msg:expr, $data:expr) => {
        log::info!(target: module_path!(), "{}", $msg);
        let data_value: Option<serde_json::Value> = $data.as_ref().and_then(|d| serde_json::to_value(d).ok());
        if let Some(ref value) = data_value {
            print!("{}", $crate::logging::formatter::format_data_output(value));
        }
        if let Some(ref mcap) = $logger.mcap_writer() {
            let _ = mcap.write_log("info", $msg, file!(), line!(), data_value.clone());
        }
        if let Some(ref otel) = $logger.otel_logger() {
            let mut attrs = vec![
                $crate::opentelemetry::KeyValue::new("code.filepath", file!().to_string()),
                $crate::opentelemetry::KeyValue::new("code.lineno", line!() as i64),
            ];
            if let Some(ref v) = data_value {
                attrs.push($crate::opentelemetry::KeyValue::new("data", v.to_string()));
            }
            otel.info($msg, attrs);
        }
    };
}

#[macro_export]
macro_rules! telemetry_warn {
    ($logger:expr, $msg:expr) => {
        log::warn!(target: module_path!(), "{}", $msg);
        if let Some(ref otel) = $logger.otel_logger() {
            otel.warn(
                $msg,
                vec![
                    $crate::opentelemetry::KeyValue::new("code.filepath", file!().to_string()),
                    $crate::opentelemetry::KeyValue::new("code.lineno", line!() as i64),
                ],
            );
        }
    };
    ($logger:expr, $msg:expr, $data:expr) => {
        log::warn!(target: module_path!(), "{}", $msg);
        let data_value: Option<serde_json::Value> = $data.as_ref().and_then(|d| serde_json::to_value(d).ok());
        if let Some(ref value) = data_value {
            print!("{}", $crate::logging::formatter::format_data_output(value));
        }
        if let Some(ref mcap) = $logger.mcap_writer() {
            let _ = mcap.write_log("warn", $msg, file!(), line!(), data_value.clone());
        }
        if let Some(ref otel) = $logger.otel_logger() {
            let mut attrs = vec![
                $crate::opentelemetry::KeyValue::new("code.filepath", file!().to_string()),
                $crate::opentelemetry::KeyValue::new("code.lineno", line!() as i64),
            ];
            if let Some(ref v) = data_value {
                attrs.push($crate::opentelemetry::KeyValue::new("data", v.to_string()));
            }
            otel.warn($msg, attrs);
        }
    };
}

#[macro_export]
macro_rules! telemetry_error {
    ($logger:expr, $msg:expr) => {
        log::error!(target: module_path!(), "{}", $msg);
        if let Some(ref otel) = $logger.otel_logger() {
            otel.error(
                $msg,
                vec![
                    $crate::opentelemetry::KeyValue::new("code.filepath", file!().to_string()),
                    $crate::opentelemetry::KeyValue::new("code.lineno", line!() as i64),
                ],
            );
        }
    };
    ($logger:expr, $msg:expr, $data:expr) => {
        log::error!(target: module_path!(), "{}", $msg);
        let data_value: Option<serde_json::Value> = $data.as_ref().and_then(|d| serde_json::to_value(d).ok());
        if let Some(ref value) = data_value {
            print!("{}", $crate::logging::formatter::format_data_output(value));
        }
        if let Some(ref mcap) = $logger.mcap_writer() {
            let _ = mcap.write_log("error", $msg, file!(), line!(), data_value.clone());
        }
        if let Some(ref otel) = $logger.otel_logger() {
            let mut attrs = vec![
                $crate::opentelemetry::KeyValue::new("code.filepath", file!().to_string()),
                $crate::opentelemetry::KeyValue::new("code.lineno", line!() as i64),
            ];
            if let Some(ref v) = data_value {
                attrs.push($crate::opentelemetry::KeyValue::new("data", v.to_string()));
            }
            otel.error($msg, attrs);
        }
    };
}
