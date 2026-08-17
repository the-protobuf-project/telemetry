//! OpenTelemetry configuration options.
//!
//! Configuration for OpenTelemetry OTLP exporters.

use serde::{Deserialize, Serialize};
use std::collections::HashMap;

/// Telemetry configuration options.
///
/// Integrates OpenTelemetry for logging, metrics, and tracing.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct OpenTelemetryOptions {
    /// Enable all telemetry (logging, metrics, tracing).
    #[serde(default = "default_true")]
    pub enabled: bool,
    /// Logging telemetry options.
    #[serde(default)]
    pub logging: LoggingTelemetryOptions,
    /// Metrics telemetry options.
    #[serde(default)]
    pub metrics: MetricsTelemetryOptions,
    /// Tracing telemetry options.
    #[serde(default)]
    pub tracing: TracingTelemetryOptions,
    /// OTLP exporter options.
    #[serde(default)]
    pub otlp: OTLPOptions,
}

/// Provides a default enabled value.
///
/// # Examples
///
/// ```
/// assert!(default_true());
/// ```
fn default_true() -> bool {
    true
}

impl Default for OpenTelemetryOptions {
    /// Creates options with all telemetry categories enabled and their default configurations.
    ///
    /// # Examples
    ///
    /// ```
    /// let options = OpenTelemetryOptions::default();
    /// assert!(options.enabled);
    /// assert!(options.logging.enabled);
    /// assert!(options.metrics.enabled);
    /// assert!(options.tracing.enabled);
    /// ```
    fn default() -> Self {
        Self {
            enabled: true,
            logging: LoggingTelemetryOptions::default(),
            metrics: MetricsTelemetryOptions::default(),
            tracing: TracingTelemetryOptions::default(),
            otlp: OTLPOptions::default(),
        }
    }
}

impl OpenTelemetryOptions {
    /// Replaces the OTLP configuration with the provided settings.
    ///
    /// # Examples
    ///
    /// ```
    /// let options = OpenTelemetryOptions::default()
    ///     .with_otlp(OTLPOptions::default());
    /// ```
    pub fn with_otlp(mut self, otlp: OTLPOptions) -> Self {
        self.otlp = otlp;
        self
    }
}

/// Logging telemetry options.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct LoggingTelemetryOptions {
    /// Enable logging (inherits from telemetry.enabled if not set).
    #[serde(default = "default_true")]
    pub enabled: bool,
}

impl Default for LoggingTelemetryOptions {
    fn default() -> Self {
        Self { enabled: true }
    }
}

/// Metrics telemetry options.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MetricsTelemetryOptions {
    /// Enable metrics (inherits from telemetry.enabled if not set).
    #[serde(default = "default_true")]
    pub enabled: bool,
    /// Export interval in seconds.
    #[serde(default = "default_export_interval")]
    pub export_interval_seconds: u64,
}

fn default_export_interval() -> u64 {
    10
}

impl Default for MetricsTelemetryOptions {
    fn default() -> Self {
        Self {
            enabled: true,
            export_interval_seconds: 10,
        }
    }
}

/// Tracing telemetry options.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TracingTelemetryOptions {
    /// Enable tracing (inherits from telemetry.enabled if not set).
    #[serde(default = "default_true")]
    pub enabled: bool,
}

impl Default for TracingTelemetryOptions {
    fn default() -> Self {
        Self { enabled: true }
    }
}

/// OTLP exporter options.
///
/// Host is auto-detected: if it's a domain (not localhost/IP), secure is enabled automatically.
/// Port defaults to 4317 (gRPC) unless use_http is true, then 4318.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct OTLPOptions {
    /// OTLP endpoint (e.g., "otel.example.com" or "localhost:4317").
    #[serde(default = "default_endpoint")]
    pub endpoint: String,
    /// Bearer token for authentication (simpler than headers).
    #[serde(default)]
    pub auth_token: Option<String>,
    /// Enable OTLP export (if false, uses stdout).
    #[serde(default)]
    pub enabled: bool,
    /// Use TLS (auto-detected from endpoint if not set).
    #[serde(default)]
    pub secure: bool,
    /// Use HTTP instead of gRPC (default: false = gRPC).
    #[serde(default)]
    pub use_http: bool,
    /// Custom headers (use auth_token for simple auth).
    #[serde(default)]
    pub headers: HashMap<String, String>,
    /// OTLP collector host (deprecated, use endpoint).
    #[serde(default = "default_host")]
    pub host: String,
    /// OTLP collector port (deprecated, auto-detected).
    #[serde(default = "default_port")]
    pub port: u16,
}

fn default_endpoint() -> String {
    "localhost:4317".to_string()
}

fn default_host() -> String {
    "localhost".to_string()
}

fn default_port() -> u16 {
    4317
}

impl Default for OTLPOptions {
    fn default() -> Self {
        Self {
            endpoint: default_endpoint(),
            auth_token: None,
            enabled: false,
            secure: false,
            use_http: false,
            headers: HashMap::new(),
            host: default_host(),
            port: default_port(),
        }
    }
}

impl OTLPOptions {
    /// Creates new OTLP options with specified host and port.
    pub fn new(host: impl Into<String>, port: u16) -> Self {
        let host_str = host.into();
        let secure = !host_str.starts_with("localhost") && !host_str.starts_with("127.0.0.1");
        Self {
            endpoint: format!("{}:{}", host_str, port),
            auth_token: None,
            enabled: true,
            secure,
            use_http: false,
            headers: HashMap::new(),
            host: host_str,
            port,
        }
    }

    /// Sets the authentication token.
    pub fn with_auth_token(mut self, token: impl Into<String>) -> Self {
        self.auth_token = Some(token.into());
        self
    }

    /// Sets whether to use secure connection.
    pub fn with_secure(mut self, secure: bool) -> Self {
        self.secure = secure;
        self
    }

    /// Adds a header to send with requests.
    pub fn with_header(mut self, key: impl Into<String>, value: impl Into<String>) -> Self {
        self.headers.insert(key.into(), value.into());
        self
    }

    /// Sets multiple headers.
    pub fn with_headers(mut self, headers: HashMap<String, String>) -> Self {
        self.headers = headers;
        self
    }

    /// Returns the full OTLP endpoint URL.
    pub fn endpoint_url(&self) -> String {
        let scheme = if self.secure { "https" } else { "http" };
        if self.endpoint.contains("://") {
            self.endpoint.clone()
        } else if self.endpoint.contains(':') {
            // Port already specified
            format!("{}://{}", scheme, self.endpoint)
        } else {
            // No port - use protocol-appropriate default (443 for secure, 4317 gRPC, 4318 HTTP)
            let port = if self.secure {
                443
            } else if self.use_http {
                4318
            } else {
                4317
            };
            format!("{}://{}:{}", scheme, self.endpoint, port)
        }
    }
}

// Keep OtelOptions as alias for backward compatibility
pub type OtelOptions = OTLPOptions;
