//! Configuration loading with Figment.
//!
//! This module provides configuration loading from multiple sources:
//! - TOML, YAML, JSON files (auto-discovered or specified)
//! - Environment variables with `TELEMETRY_` prefix
//! - Programmatic defaults
//!
//! # Configuration File Discovery
//!
//! Telemetry auto-discovers config files in this order:
//! 1. `TELEMETRY_CONFIG_PATH` environment variable
//! 2. `telemetry.toml` in current directory
//! 3. `telemetry.yaml` / `telemetry.yml` / `telemetry.json`
//! 4. `.config/telemetry.toml` / `.config/telemetry.yaml` / `.config/telemetry.json`
//!
//! # Example Configuration (TOML)
//!
//! ```toml
//! [service]
//! name = "my-service"
//! version = "1.0.0"
//! environment = "production"
//! description = "My awesome service"
//!
//! [service.labels]
//! robot_id = "robot-001"
//! fleet_id = "fleet-alpha"
//!
//! [telemetry]
//! enabled = true
//!
//! [telemetry.otlp]
//! enabled = true
//! endpoint = "otel-collector:4317"
//! auth_token = "your-bearer-token"
//!
//! [foxglove]
//! enabled = false
//! file_path = "./recordings/session.mcap"
//!
//! [tracing]
//! enabled = true
//! ```

use std::collections::HashMap;
use std::path::Path;

use figment::{
    Figment,
    providers::{Env, Format, Json, Serialized, Toml, Yaml},
};
use serde::{Deserialize, Serialize};

use crate::options::{
    Environment, FoxgloveOptions, LogLevel, LoggingOptions, ModuleOptions, OTLPOptions,
    OpenTelemetryOptions, ProfilingOptions, ServiceOptions, TelemetryOptions, TracingOptions,
};

/// Complete Telemetry configuration loaded from files/environment.
///
/// This struct represents the full configuration that can be loaded from
/// TOML, YAML, JSON files or environment variables.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default)]
pub struct TelemetryConfig {
    pub service: ServiceConfig,
    pub telemetry: OpenTelemetryConfig,
    pub foxglove: FoxgloveConfig,
    pub tracing: TracingConfig,
    pub logging: LoggingConfig,
    pub profiling: ProfilingConfig,
}

/// Service identification configuration.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(default)]
pub struct ServiceConfig {
    pub name: String,
    pub version: String,
    pub environment: String,
    pub description: String,
    /// Global labels added to ALL telemetry (logs, metrics, traces).
    #[serde(default)]
    pub labels: HashMap<String, String>,
}

impl Default for ServiceConfig {
    fn default() -> Self {
        Self {
            name: "unknown".to_string(),
            version: "0.0.0".to_string(),
            environment: "development".to_string(),
            description: String::new(),
            labels: HashMap::new(),
        }
    }
}

/// Telemetry configuration.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(default)]
pub struct OpenTelemetryConfig {
    pub enabled: bool,
    pub otlp: OtlpConfig,
    pub metrics: MetricsConfig,
}

impl Default for OpenTelemetryConfig {
    fn default() -> Self {
        Self {
            enabled: true,
            otlp: OtlpConfig::default(),
            metrics: MetricsConfig::default(),
        }
    }
}

/// OTLP exporter configuration.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(default)]
pub struct OtlpConfig {
    pub enabled: bool,
    /// OTLP endpoint (host:port or full URL).
    pub endpoint: String,
    /// Optional bearer token for authentication.
    pub auth_token: Option<String>,
    /// Use secure connection (auto-detected for non-localhost).
    pub secure: Option<bool>,
    /// Use HTTP instead of gRPC.
    pub use_http: bool,
    /// Additional headers to send with requests.
    #[serde(default)]
    pub headers: HashMap<String, String>,
}

impl Default for OtlpConfig {
    fn default() -> Self {
        Self {
            enabled: false,
            endpoint: "localhost:4317".to_string(),
            auth_token: None,
            secure: None,
            use_http: false,
            headers: HashMap::new(),
        }
    }
}

/// Metrics export configuration.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(default)]
pub struct MetricsConfig {
    pub export_interval_seconds: u64,
}

impl Default for MetricsConfig {
    fn default() -> Self {
        Self {
            export_interval_seconds: 10,
        }
    }
}

/// Foxglove MCAP recording configuration.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default)]
pub struct FoxgloveConfig {
    pub enabled: bool,
    pub file_path: String,
}

/// Tracing configuration.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(default)]
pub struct TracingConfig {
    pub enabled: bool,
    /// Sampling ratio (0.0 to 1.0).
    pub sample_ratio: f64,
}

impl Default for TracingConfig {
    fn default() -> Self {
        Self {
            enabled: true,
            sample_ratio: 1.0,
        }
    }
}

/// Logging configuration.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default)]
pub struct LoggingConfig {
    pub log: LogConfig,
    /// Global log level (1=Error, 2=Info, 3=Debug). Overrides environment-based default.
    #[serde(default)]
    pub level: u8,
    /// Per-module log level overrides keyed by service name.
    #[serde(default)]
    pub modules: HashMap<String, ModuleConfig>,
}

/// Per-module config from TOML.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default)]
pub struct ModuleConfig {
    pub level: u8,
}

/// Log output configuration.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(default)]
pub struct LogConfig {
    pub report_caller: bool,
    pub report_timestamp: bool,
    pub level: String,
}

impl Default for LogConfig {
    fn default() -> Self {
        Self {
            report_caller: true,
            report_timestamp: true,
            level: "info".to_string(),
        }
    }
}

/// Profiling configuration.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(default)]
pub struct ProfilingConfig {
    pub enabled: bool,
    pub server_address: String,
}

impl Default for ProfilingConfig {
    fn default() -> Self {
        Self {
            enabled: false,
            server_address: "http://localhost:4040".to_string(),
        }
    }
}

impl TelemetryConfig {
    /// Creates a Figment instance for loading configuration.
    ///
    /// Configuration is loaded in this priority order (highest to lowest):
    /// 1. Environment variables (`TELEMETRY_*`)
    /// 2. Config file (auto-discovered or specified)
    /// 3. Default values
    pub fn figment() -> Figment {
        Self::figment_with_path(None)
    }

    /// Creates a Figment instance with a specific config file path.
    pub fn figment_with_path(config_path: Option<&str>) -> Figment {
        let mut figment = Figment::from(Serialized::defaults(TelemetryConfig::default()));

        // Try to load from config file
        if let Some(path) = config_path {
            figment = Self::merge_config_file(figment, path);
        } else if let Ok(path) = std::env::var("TELEMETRY_CONFIG_PATH") {
            figment = Self::merge_config_file(figment, &path);
        } else {
            // Auto-discover config files
            figment = Self::auto_discover_config(figment);
        }

        // Merge environment variables (highest priority)
        figment.merge(Env::prefixed("TELEMETRY_").split("_"))
    }

    /// Auto-discover and load configuration files.
    fn auto_discover_config(figment: Figment) -> Figment {
        let config_paths = [
            "telemetry.toml",
            "telemetry.yaml",
            "telemetry.yml",
            "telemetry.json",
            ".config/telemetry.toml",
            ".config/telemetry.yaml",
            ".config/telemetry.yml",
            ".config/telemetry.json",
        ];

        for path in config_paths {
            if Path::new(path).exists() {
                return Self::merge_config_file(figment, path);
            }
        }

        figment
    }

    /// Merge a config file based on its extension.
    fn merge_config_file(figment: Figment, path: &str) -> Figment {
        if path.ends_with(".toml") {
            figment.merge(Toml::file(path))
        } else if path.ends_with(".yaml") || path.ends_with(".yml") {
            figment.merge(Yaml::file(path))
        } else if path.ends_with(".json") {
            figment.merge(Json::file(path))
        } else {
            // Try TOML by default
            figment.merge(Toml::file(path))
        }
    }

    /// Load configuration from auto-discovered sources.
    #[allow(clippy::result_large_err)]
    pub fn load() -> Result<Self, figment::Error> {
        Self::figment().extract()
    }

    /// Load configuration from a specific file path.
    #[allow(clippy::result_large_err)]
    pub fn load_from(path: &str) -> Result<Self, figment::Error> {
        Self::figment_with_path(Some(path)).extract()
    }

    /// Convert to ServiceOptions for Telemetry initialization.
    pub fn to_service_options(&self) -> ServiceOptions {
        let env = match self.service.environment.to_lowercase().as_str() {
            "production" | "prod" => Environment::Production,
            "staging" | "stage" => Environment::Staging,
            "jetson" => Environment::Jetson,
            _ => Environment::Development,
        };

        ServiceOptions::new(&self.service.name, &self.service.version)
            .with_description(&self.service.description)
            .with_environment(env)
            .with_labels(self.service.labels.clone())
    }

    /// Convert to TelemetryOptions for Telemetry initialization.
    pub fn to_telemetry_options(&self) -> TelemetryOptions {
        let (host, port) = Self::parse_endpoint(&self.telemetry.otlp.endpoint);

        let mut otlp = OTLPOptions::default();
        // OTLP is enabled if telemetry is enabled (no need for separate otlp.enabled)
        otlp.enabled = self.telemetry.enabled;
        otlp.endpoint = self.telemetry.otlp.endpoint.clone();
        otlp.host = host;
        otlp.port = port;

        if let Some(token) = &self.telemetry.otlp.auth_token {
            otlp.auth_token = Some(token.clone());
        }

        if let Some(secure) = self.telemetry.otlp.secure {
            otlp.secure = secure;
        }

        otlp.headers = self.telemetry.otlp.headers.clone();
        otlp.use_http = self.telemetry.otlp.use_http;

        let telemetry = OpenTelemetryOptions::default().with_otlp(otlp);

        let foxglove = if self.foxglove.enabled && !self.foxglove.file_path.is_empty() {
            FoxgloveOptions::new(&self.foxglove.file_path)
        } else {
            FoxgloveOptions::disabled()
        };

        let mut logging = LoggingOptions::default();
        logging.level = LogLevel::from_u8(self.logging.level);
        for (name, mod_cfg) in &self.logging.modules {
            logging.modules.insert(
                name.clone(),
                ModuleOptions {
                    level: LogLevel::from_u8(mod_cfg.level),
                },
            );
        }
        let profiling = ProfilingOptions::default();
        let tracing = TracingOptions::default();

        TelemetryOptions::new()
            .with_logging(logging)
            .with_telemetry(telemetry)
            .with_foxglove(foxglove)
            .with_profiling(profiling)
            .with_tracing(tracing)
    }

    /// Parse endpoint string into host and port.
    fn parse_endpoint(endpoint: &str) -> (String, u16) {
        // Remove protocol prefix if present
        let endpoint = endpoint
            .trim_start_matches("http://")
            .trim_start_matches("https://")
            .trim_start_matches("grpc://");

        if let Some((host, port_str)) = endpoint.rsplit_once(':')
            && let Ok(port) = port_str.parse::<u16>()
        {
            return (host.to_string(), port);
        }

        // Default to port 4317 if not specified
        (endpoint.to_string(), 4317)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_default_config() {
        let config = TelemetryConfig::default();
        assert_eq!(config.service.name, "unknown");
        assert_eq!(config.service.environment, "development");
        assert!(!config.telemetry.otlp.enabled);
        assert!(!config.foxglove.enabled);
    }

    #[test]
    fn test_parse_endpoint() {
        let (host, port) = TelemetryConfig::parse_endpoint("localhost:4317");
        assert_eq!(host, "localhost");
        assert_eq!(port, 4317);

        let (host, port) = TelemetryConfig::parse_endpoint("http://otel-collector:4317");
        assert_eq!(host, "otel-collector");
        assert_eq!(port, 4317);

        let (host, port) = TelemetryConfig::parse_endpoint("otel-collector");
        assert_eq!(host, "otel-collector");
        assert_eq!(port, 4317);
    }

    #[test]
    fn test_to_service_options() {
        let mut config = TelemetryConfig::default();
        config.service.name = "test-service".to_string();
        config.service.version = "1.0.0".to_string();
        config.service.environment = "production".to_string();
        config
            .service
            .labels
            .insert("robot_id".to_string(), "robot-001".to_string());

        let opts = config.to_service_options();
        assert_eq!(opts.name, "test-service");
        assert_eq!(opts.version, "1.0.0");
        assert_eq!(opts.labels.get("robot_id"), Some(&"robot-001".to_string()));
    }
}
