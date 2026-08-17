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

fn default_true() -> bool {
    true
}

fn default_sample_ratio() -> f64 {
    1.0
}

impl Default for TracingOptions {
    fn default() -> Self {
        Self {
            enabled: true,
            sample_ratio: 1.0,
        }
    }
}
