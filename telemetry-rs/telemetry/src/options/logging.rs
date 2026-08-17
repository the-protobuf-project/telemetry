//! Logging configuration options.

use serde::{Deserialize, Serialize};
use std::collections::HashMap;

/// Log level for a service or module.
///
/// Higher levels produce more verbose output.
///
/// - `Level1` (Error) — stable, production-ready module; minimal noise
/// - `Level2` (Info)  — normal operation; standard telemetry
/// - `Level3` (Debug) — active development; full observability
#[allow(non_camel_case_types)]
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize, Default)]
pub enum LogLevel {
    /// No explicit level set; fall back to environment-based default.
    #[default]
    #[serde(rename = "0")]
    Unset,
    /// Error only — stable module.
    #[serde(rename = "1")]
    ModuleLevel_1,
    /// Info — normal operation.
    #[serde(rename = "2")]
    ModuleLevel_2,
    /// Debug — full observability.
    #[serde(rename = "3")]
    ModuleLevel_3,
}

impl LogLevel {
    /// Converts a serialized numeric level to its corresponding logging level.
    ///
    /// Values `1` through `3` map to explicit module levels; all other values map to
    /// [`LogLevel::Unset`].
    ///
    /// # Examples
    ///
    /// ```
    /// assert_eq!(LogLevel::from_u8(3), LogLevel::ModuleLevel_3);
    /// assert_eq!(LogLevel::from_u8(0), LogLevel::Unset);
    /// ```
    ///
    /// # Parameters
    ///
    /// * `v` - Numeric level value from serialized configuration.
    ///
    /// # Returns
    ///
    /// The corresponding [`LogLevel`].
    pub fn from_u8(v: u8) -> Self {
        match v {
            1 => LogLevel::ModuleLevel_1,
            2 => LogLevel::ModuleLevel_2,
            3 => LogLevel::ModuleLevel_3,
            _ => LogLevel::Unset,
        }
    }

    /// Maps the configured logging level to its corresponding [`log::LevelFilter`].
    ///
    /// # Examples
    ///
    /// ```
    /// assert_eq!(
    ///     LogLevel::ModuleLevel_1.to_level_filter(),
    ///     log::LevelFilter::Error
    /// );
    /// ```
    ///
    /// # Returns
    ///
    /// The corresponding log filter; unset levels use [`log::LevelFilter::Info`].
    pub fn to_level_filter(self) -> log::LevelFilter {
        match self {
            LogLevel::ModuleLevel_1 => log::LevelFilter::Error,
            LogLevel::ModuleLevel_2 => log::LevelFilter::Info,
            LogLevel::ModuleLevel_3 => log::LevelFilter::Debug,
            LogLevel::Unset => log::LevelFilter::Info,
        }
    }

    /// Determines whether the logging level is explicitly configured.
    ///
    /// # Returns
    ///
    /// `true` for an explicit logging level, `false` for [`LogLevel::Unset`].
    ///
    /// # Examples
    ///
    /// ```
    /// assert!(LogLevel::ModuleLevel_1.is_set());
    /// assert!(!LogLevel::Unset.is_set());
    /// ```
    pub fn is_set(self) -> bool {
        self != LogLevel::Unset
    }
}

/// Per-module logging overrides.
///
/// When set in config, these take highest priority (after env vars).
#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct ModuleOptions {
    /// Log level override for this module.
    #[serde(default)]
    pub level: LogLevel,
}

/// Time format options for log output.
#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub enum TimeFormat {
    #[default]
    RFC3339,
    RFC3339Nano,
    Kitchen,
    Stamp,
    Custom,
}

/// Log output configuration.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct LogOptions {
    /// Prefix string in log output.
    #[serde(default)]
    pub prefix: String,
    /// Include caller info in logs.
    #[serde(default = "default_true")]
    pub report_caller: bool,
    /// Include timestamp in logs.
    #[serde(default = "default_true")]
    pub report_timestamp: bool,
    /// Named time format enum.
    #[serde(default)]
    pub time_format_key: TimeFormat,
    /// Custom format if time_format_key == Custom.
    #[serde(default)]
    pub custom_format: String,
    /// Adjust call depth for correct file/line display.
    #[serde(default = "default_caller_offset")]
    pub caller_offset: i32,
}

/// Provides a default enabled value.
///
/// # Examples
///
/// ```
/// assert!(default_true());
/// ```
///
fn default_true() -> bool {
    true
}

/// Provides the default caller offset for log messages.

///

/// # Examples

///

/// ```

/// assert_eq!(default_caller_offset(), 3);

/// ```
fn default_caller_offset() -> i32 {
    3
}

impl Default for LogOptions {
    /// Creates logging options with caller and timestamp reporting enabled and the default timestamp format.
    ///
    /// # Examples
    ///
    /// ```
    /// let options = LogOptions::default();
    /// assert!(options.report_caller);
    /// assert!(options.report_timestamp);
    /// ```
    ///
    fn default() -> Self {
        Self {
            prefix: String::new(),
            report_caller: true,
            report_timestamp: true,
            time_format_key: TimeFormat::default(),
            custom_format: String::new(),
            caller_offset: 3,
        }
    }
}

/// Logging options container.
#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct LoggingOptions {
    /// Log options.
    #[serde(default)]
    pub log: LogOptions,
    /// Global log level (overrides environment-based default).
    #[serde(default)]
    pub level: LogLevel,
    /// Per-module log level overrides keyed by service name.
    #[serde(default)]
    pub modules: HashMap<String, ModuleOptions>,
}
