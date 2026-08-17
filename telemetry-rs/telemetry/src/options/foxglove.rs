//! Foxglove MCAP recording options.
//!
//! Configuration for enabling and configuring MCAP file recording.

use serde::{Deserialize, Serialize};

/// Configuration options for Foxglove MCAP recording.
///
/// # Examples
///
/// ```no_run
/// use telemetry::options::FoxgloveOptions;
///
/// let opts = FoxgloveOptions::new("output.mcap");
/// ```
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct FoxgloveOptions {
    pub enabled: bool,
    pub mcap_path: String,
}

impl FoxgloveOptions {
    /// Creates new Foxglove options with MCAP recording enabled.
    ///
    /// # Arguments
    ///
    /// * `mcap_path` - Path where MCAP file will be written
    pub fn new(mcap_path: impl Into<String>) -> Self {
        Self {
            enabled: true,
            mcap_path: mcap_path.into(),
        }
    }

    /// Creates Foxglove options with recording disabled.
    pub fn disabled() -> Self {
        Self::default()
    }
}
