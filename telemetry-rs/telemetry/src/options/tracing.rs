//! Distributed tracing configuration options.

use serde::{Deserialize, Serialize};

/// Tracing options for distributed tracing.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TracingOptions {
    /// Enable distributed tracing.
    #[serde(default = "default_true")]
    pub enabled: bool,
    /// Sampling ratio (0.0 to 1.0).
    #[serde(default = "default_sample_ratio")]
    pub sample_ratio: f64,
}

/// Provides `true` as the default value for enabled options.
///
/// # Examples
///
/// ```
/// assert!(default_true());
/// ```
fn default_true() -> bool {
    true
}

/// Provides the default sampling ratio for tracing.
///
/// # Examples
///
/// ```
/// assert_eq!(default_sample_ratio(), 1.0);
/// ```
fn default_sample_ratio() -> f64 {
    1.0
}

impl Default for TracingOptions {
    /// Creates tracing options with tracing enabled and full sampling.
    ///
    /// # Examples
    ///
    /// ```
    /// let options = TracingOptions::default();
    /// assert!(options.enabled);
    /// assert_eq!(options.sample_ratio, 1.0);
    /// ```
    fn default() -> Self {
        Self {
            enabled: true,
            sample_ratio: 1.0,
        }
    }
}
