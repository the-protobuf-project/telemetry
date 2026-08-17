//! Profiling configuration options for Pyroscope integration.

use serde::{Deserialize, Serialize};
use std::collections::HashMap;

/// Profiling options for continuous profiling with Pyroscope.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ProfilingOptions {
    /// Enable continuous profiling.
    #[serde(default)]
    pub enabled: bool,
    /// Pyroscope server URL (e.g., "http://localhost:4040").
    #[serde(default = "default_server_address")]
    pub server_address: String,

    // Authentication (optional, required for Grafana Cloud)
    /// Basic auth username.
    #[serde(default)]
    pub basic_auth_user: String,
    /// Basic auth password.
    #[serde(default)]
    pub basic_auth_password: String,
    /// Tenant ID for multi-tenancy (optional).
    #[serde(default)]
    pub tenant_id: String,

    // Profile types - enable/disable specific profiling types
    /// CPU profiling (default: true).
    #[serde(default = "default_true")]
    pub profile_cpu: bool,
    /// Allocation objects profiling (default: true).
    #[serde(default = "default_true")]
    pub profile_alloc_objects: bool,
    /// Allocation space profiling (default: true).
    #[serde(default = "default_true")]
    pub profile_alloc_space: bool,
    /// In-use objects profiling (default: true).
    #[serde(default = "default_true")]
    pub profile_inuse_objects: bool,
    /// In-use space profiling (default: true).
    #[serde(default = "default_true")]
    pub profile_inuse_space: bool,
    /// Goroutines profiling (default: false).
    #[serde(default)]
    pub profile_goroutines: bool,
    /// Mutex count profiling (default: false).
    #[serde(default)]
    pub profile_mutex_count: bool,
    /// Mutex duration profiling (default: false).
    #[serde(default)]
    pub profile_mutex_duration: bool,
    /// Block count profiling (default: false).
    #[serde(default)]
    pub profile_block_count: bool,
    /// Block duration profiling (default: false).
    #[serde(default)]
    pub profile_block_duration: bool,

    // Profile rates
    /// Mutex profile fraction (e.g., 5 = 1/5 events reported).
    #[serde(default = "default_profile_rate")]
    pub mutex_profile_rate: i32,
    /// Block profile rate in nanoseconds (e.g., 5).
    #[serde(default = "default_profile_rate")]
    pub block_profile_rate: i32,

    /// Additional tags to attach to profiles.
    #[serde(default)]
    pub tags: HashMap<String, String>,
}

fn default_true() -> bool {
    true
}

fn default_server_address() -> String {
    "http://localhost:4040".to_string()
}

fn default_profile_rate() -> i32 {
    5
}

impl Default for ProfilingOptions {
    fn default() -> Self {
        Self {
            enabled: false,
            server_address: default_server_address(),
            basic_auth_user: String::new(),
            basic_auth_password: String::new(),
            tenant_id: String::new(),
            profile_cpu: true,
            profile_alloc_objects: true,
            profile_alloc_space: true,
            profile_inuse_objects: true,
            profile_inuse_space: true,
            profile_goroutines: false,
            profile_mutex_count: false,
            profile_mutex_duration: false,
            profile_block_count: false,
            profile_block_duration: false,
            mutex_profile_rate: 5,
            block_profile_rate: 5,
            tags: HashMap::new(),
        }
    }
}
