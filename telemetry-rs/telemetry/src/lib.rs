//! Telemetry - Unified observability library for robotics and distributed systems.
//!
//! Telemetry provides integrated logging, metrics, and tracing with support for
//! multiple backends including console output, MCAP files (Foxglove), and
//! OpenTelemetry.
//!
//! # Features
//!
//! - **Logging**: Structured logging with colored console output, MCAP recording,
//!   and OpenTelemetry integration
//! - **Metrics**: Counter, histogram, and gauge metrics with derive macro support
//! - **Tracing**: Distributed tracing with OpenTelemetry
//! - **MCAP Recording**: Record logs, metrics, and traces to MCAP files for
//!   visualization in Foxglove Studio
//!
//! # Examples
//!
//! ```no_run
//! use telemetry::Telemetry;
//! use telemetry::logger;
//!
//! let _telemetry = Telemetry::builder("my-service", "1.0.0")
//!     .with_mcap("output.mcap")
//!     .build()
//!     .unwrap();
//!
//! logger::info!("Application started");
//! // Resources are automatically cleaned up when telemetry goes out of scope
//! ```

pub mod config;
pub mod derive;
pub mod foxglove;
pub mod logging;
pub mod metrics;
pub mod options;
pub mod telemetry;
pub mod tracing;
pub mod traits;

#[doc(inline)]
pub use crate::macros::DEFAULT_OTEL_COLLECTOR_OTLP_PORT;

mod macros;

use anyhow::{Result, anyhow};
use std::collections::HashMap;
use std::sync::{Arc, Mutex};

pub use config::TelemetryConfig;
pub use logging::Logger;
pub use logging::global as logger;
pub use metrics::Metrics;
pub use opentelemetry;
pub use options::{Environment, LogLevel};

/// Main Telemetry instance that manages all observability components.
///
/// This struct provides access to logging, metrics, and tracing functionality,
/// and manages the lifecycle of MCAP writers and telemetry providers.
pub struct Telemetry {
    pub logger: Logger,
    pub tracing: Option<tracing::TelemetryTracing>,
    pub metrics: Metrics,
    mcap_writer: Option<Arc<Mutex<foxglove::UnifiedMcapWriter>>>,
    telemetry: Option<telemetry::TelemetryProvider>,
    closed: bool,
}

impl Telemetry {
    /// Creates a new builder that auto-discovers configuration.
    ///
    /// This is the recommended way to initialize Telemetry. Configuration is loaded from:
    /// 1. `TELEMETRY_CONFIG_PATH` environment variable
    /// 2. `telemetry.toml` in current directory
    /// 3. `telemetry.yaml` / `telemetry.yml` / `telemetry.json`
    /// 4. `.config/telemetry.toml` / `.config/telemetry.yaml` / `.config/telemetry.json`
    ///
    /// # Examples
    ///
    /// ```no_run
    /// use telemetry::Telemetry;
    ///
    /// // Auto-discovers telemetry.toml or uses defaults
    /// let telemetry = Telemetry::new()
    ///     .with_service("my-service", "1.0.0")
    ///     .build()
    ///     .unwrap();
    /// ```
    #[allow(clippy::new_ret_no_self)]
    pub fn new() -> TelemetryBuilder {
        TelemetryBuilder::from_config()
    }

    /// Creates a new builder for configuring Telemetry.
    ///
    /// # Arguments
    ///
    /// * `name` - Service name
    /// * `version` - Service version
    ///
    /// # Examples
    ///
    /// ```no_run
    /// use telemetry::{Telemetry, Environment};
    ///
    /// let telemetry = Telemetry::builder("my-service", "1.0.0")
    ///     .environment(Environment::Production)
    ///     .with_otlp("localhost", 4317)
    ///     .build()
    ///     .unwrap();
    /// ```
    pub fn builder(name: impl Into<String>, version: impl Into<String>) -> TelemetryBuilder {
        TelemetryBuilder::new(name, version)
    }

    /// Creates a new Telemetry instance from options (legacy API).
    ///
    /// Initializes all observability components based on the provided configuration.
    /// For new code, prefer using `Telemetry::new()` or `Telemetry::builder()` instead.
    ///
    /// # Arguments
    ///
    /// * `service_opts` - Service configuration (name, version, environment)
    /// * `telemetry_opts` - Telemetry configuration (telemetry, Foxglove options)
    ///
    /// # Examples
    ///
    /// ```no_run
    /// use telemetry::{Telemetry, options::{ServiceOptions, TelemetryOptions}};
    ///
    /// let service_opts = ServiceOptions::new("my-service", "1.0.0");
    /// let telemetry_opts = TelemetryOptions::new();
    /// let telemetry = Telemetry::init(service_opts, telemetry_opts).unwrap();
    /// ```
    pub fn init(
        service_opts: options::ServiceOptions,
        telemetry_opts: options::TelemetryOptions,
    ) -> Result<Self> {
        let formatter = logging::TelemetryFormatter::new();
        formatter.set_service_info(
            service_opts.name.clone(),
            service_opts.version.clone(),
            service_opts.environment.to_string(),
        );

        // Resolve log level using priority chain:
        //   per-module override > global logging.level > RUST_LOG > environment default
        let env_default = match service_opts.environment {
            options::Environment::Development | options::Environment::Jetson => {
                log::LevelFilter::Debug
            }
            options::Environment::Staging => log::LevelFilter::Warn,
            options::Environment::Production => log::LevelFilter::Info,
        };

        let mut default_level = std::env::var("RUST_LOG")
            .ok()
            .and_then(|s| s.parse::<log::LevelFilter>().ok())
            .unwrap_or(env_default);

        // Global logging.level overrides environment default
        if telemetry_opts.logging.level.is_set() {
            default_level = telemetry_opts.logging.level.to_level_filter();
        }

        // Per-module override for this service (highest priority)
        if let Some(mod_opts) = telemetry_opts.logging.modules.get(&service_opts.name)
            && mod_opts.level.is_set()
        {
            default_level = mod_opts.level.to_level_filter();
        }

        let _ = log4rs::init_file("log4rs.yaml", Default::default()).or_else(|_| {
            let stdout = log4rs::append::console::ConsoleAppender::builder()
                .encoder(Box::new(formatter))
                .build();
            let config = log4rs::config::Config::builder()
                .appender(log4rs::config::Appender::builder().build("stdout", Box::new(stdout)))
                .build(
                    log4rs::config::Root::builder()
                        .appender("stdout")
                        .build(default_level),
                )
                .unwrap();
            log4rs::init_config(config).map(|_| ())
        });

        let mcap_writer = if telemetry_opts.foxglove.enabled
            && !telemetry_opts.foxglove.mcap_path.is_empty()
        {
            let writer = foxglove::UnifiedMcapWriter::new(
                &service_opts,
                &telemetry_opts.foxglove.mcap_path,
            )?;
            Some(Arc::new(Mutex::new(writer)))
        } else {
            None
        };

        let mcap_log_writer = mcap_writer
            .as_ref()
            .map(|writer| logging::LogMcapWriter::new(&service_opts, Arc::clone(writer)))
            .transpose()?;

        let telemetry = Some(telemetry::TelemetryProvider::new(
            &service_opts,
            &telemetry_opts.telemetry,
        )?);
        let otel_logger = telemetry
            .as_ref()
            .and_then(|t| t.get_logger("telemetry"));

        let logger = Logger::new(
            service_opts.name.clone(),
            service_opts.version.clone(),
            service_opts.environment.to_string(),
            mcap_log_writer,
            otel_logger,
        );

        let global_logger = logging::GlobalLogger::new(
            service_opts.name.clone(),
            service_opts.version.clone(),
            service_opts.environment.to_string(),
            logger.mcap_writer_arc(),
            logger.otel_logger_arc(),
        );
        logging::init(global_logger);

        let tracing_tracer = telemetry
            .as_ref()
            .and_then(|t| t.get_tracer(&service_opts.name));

        // Initialize tokio-rs/tracing with the same OpenTelemetry tracer provider
        // used by Telemetry telemetry, so shutdown/flush is centrally managed.
        if let Some(tracer) = tracing_tracer.clone() {
            tracing::init_tokio_tracing(tracer)?;
        }

        // Initialize TelemetryTracing for manual span management.
        let tracing_instance = Some(tracing::TelemetryTracing::new(tracing_tracer));

        // Initialize metrics
        let metrics = metrics::Metrics::new(
            service_opts.clone(),
            mcap_writer.clone(),
            telemetry.as_ref().and_then(|t| t.meter_provider()),
        )?;

        Ok(Self {
            logger,
            tracing: tracing_instance,
            metrics,
            mcap_writer,
            telemetry,
            closed: false,
        })
    }

    /// Flushes all pending telemetry data.
    ///
    /// This should be called before shutting down to ensure all data is sent.
    pub fn flush(&self) -> Result<()> {
        if let Some(ref t) = self.telemetry {
            t.flush()?;
        }
        Ok(())
    }

    /// Returns a reference to the MCAP writer if configured.
    pub fn mcap_writer(&self) -> Option<Arc<Mutex<foxglove::UnifiedMcapWriter>>> {
        self.mcap_writer.clone()
    }

    /// Returns the OpenTelemetry meter provider if configured.
    pub fn meter_provider(&self) -> Option<Arc<opentelemetry_sdk::metrics::SdkMeterProvider>> {
        self.telemetry.as_ref().and_then(|t| t.meter_provider())
    }

    /// Manually closes the Telemetry instance and cleans up resources.
    ///
    /// This shuts down telemetry providers and closes MCAP writers.
    ///
    /// **Note**: Resources are automatically cleaned up when Telemetry goes out of scope
    /// via the Drop trait. You only need to call this manually if you want explicit
    /// error handling during cleanup.
    ///
    /// # Examples
    ///
    /// ```no_run
    /// use telemetry::{Telemetry, options::{ServiceOptions, TelemetryOptions}};
    ///
    /// let service_opts = ServiceOptions::new("my-service", "1.0.0");
    /// let telemetry_opts = TelemetryOptions::new();
    /// let mut telemetry = Telemetry::init(service_opts, telemetry_opts).unwrap();
    ///
    /// // Use telemetry...
    ///
    /// // Optional: Manually close if you need error handling
    /// telemetry.close().unwrap();
    ///
    /// // Otherwise, resources are cleaned up automatically when telemetry goes out of scope
    /// ```
    pub fn close(&mut self) -> Result<()> {
        if self.closed {
            return Ok(());
        }
        self.closed = true;

        let mut errors = Vec::new();

        // Flush + shutdown OTLP (batch processors); idempotent if already shut down
        if let Some(t) = self.telemetry.take()
            && let Err(err) = t.shutdown()
        {
            errors.push(format!("telemetry shutdown: {err}"));
        }

        if let Some(writer) = self.mcap_writer.take() {
            match writer.lock() {
                Ok(mut w) => {
                    if let Err(err) = w.close() {
                        errors.push(format!("mcap close: {err}"));
                    }
                }
                Err(err) => {
                    errors.push(format!("mcap lock poisoned: {err}"));
                }
            }
        }

        if errors.is_empty() {
            Ok(())
        } else {
            Err(anyhow!(errors.join("; ")))
        }
    }
}

impl Drop for Telemetry {
    fn drop(&mut self) {
        let _ = self.close();
    }
}

/// Builder for configuring and creating a Telemetry instance.
///
/// Provides a fluent API for configuring observability options.
///
/// # Examples
///
/// ```no_run
/// use telemetry::{Telemetry, Environment};
///
/// let telemetry = Telemetry::builder("my-service", "1.0.0")
///     .description("My awesome service")
///     .environment(Environment::Production)
///     .with_otlp("localhost", 4317)
///     .with_mcap("output.mcap")
///     .build()
///     .unwrap();
/// ```
pub struct TelemetryBuilder {
    name: Option<String>,
    version: Option<String>,
    description: Option<String>,
    environment: Option<options::Environment>,
    labels: HashMap<String, String>,
    otlp_host: Option<String>,
    otlp_port: Option<u16>,
    otlp_auth_token: Option<String>,
    otlp_headers: HashMap<String, String>,
    otlp_secure: Option<bool>,
    mcap_path: Option<String>,
    config_path: Option<String>,
    tracing_enabled: bool,
    profiling_address: Option<String>,
    log_level: Option<options::LogLevel>,
    service_from_code: bool,
}

pub use telemetry_derive::{instrument, trace};

impl TelemetryBuilder {
    /// Creates a new builder with service name and version.
    pub fn new(name: impl Into<String>, version: impl Into<String>) -> Self {
        Self {
            name: Some(name.into()),
            version: Some(version.into()),
            description: None,
            environment: None,
            labels: HashMap::new(),
            otlp_host: None,
            otlp_port: None,
            otlp_auth_token: None,
            otlp_headers: HashMap::new(),
            otlp_secure: None,
            mcap_path: None,
            config_path: None,
            tracing_enabled: false,
            profiling_address: None,
            log_level: None,
            service_from_code: true,
        }
    }

    /// Creates a new builder that will auto-discover configuration.
    ///
    /// Configuration is loaded from:
    /// 1. `TELEMETRY_CONFIG_PATH` environment variable
    /// 2. `telemetry.toml` in current directory
    /// 3. `telemetry.yaml` / `telemetry.yml` / `telemetry.json`
    /// 4. `.config/telemetry.toml` / `.config/telemetry.yaml` / `.config/telemetry.json`
    pub fn from_config() -> Self {
        Self {
            name: None,
            version: None,
            description: None,
            environment: None,
            labels: HashMap::new(),
            otlp_host: None,
            otlp_port: None,
            otlp_auth_token: None,
            otlp_headers: HashMap::new(),
            otlp_secure: None,
            mcap_path: None,
            config_path: None,
            tracing_enabled: false,
            profiling_address: None,
            log_level: None,
            service_from_code: false,
        }
    }

    /// Load configuration from a specific file path.
    pub fn with_config(mut self, path: impl Into<String>) -> Self {
        self.config_path = Some(path.into());
        self
    }

    /// Sets the service name and version.
    pub fn with_service(mut self, name: impl Into<String>, version: impl Into<String>) -> Self {
        self.name = Some(name.into());
        self.version = Some(version.into());
        self
    }

    /// Sets the service description.
    pub fn description(mut self, description: impl Into<String>) -> Self {
        self.description = Some(description.into());
        self
    }

    /// Sets the deployment environment.
    pub fn environment(mut self, environment: options::Environment) -> Self {
        self.environment = Some(environment);
        self
    }

    /// Sets global labels that will be added to all telemetry.
    pub fn with_labels(mut self, labels: HashMap<String, String>) -> Self {
        self.labels = labels;
        self
    }

    /// Adds a single label.
    pub fn with_label(mut self, key: impl Into<String>, value: impl Into<String>) -> Self {
        self.labels.insert(key.into(), value.into());
        self
    }

    /// Enables OpenTelemetry OTLP export.
    ///
    /// # Arguments
    ///
    /// * `host` - OTLP collector host
    /// * `port` - OTLP collector port (gRPC default unless `with_otlp_http` / config `use_http`)
    pub fn with_otlp(mut self, host: impl Into<String>, port: u16) -> Self {
        self.otlp_host = Some(host.into());
        self.otlp_port = Some(port);
        self
    }

    /// OTLP to local collector on [`DEFAULT_OTEL_COLLECTOR_OTLP_PORT`] (gRPC), same as [`telemetry_local_otel!`].
    pub fn with_local_otel_collector(self) -> Self {
        self.with_otlp("localhost", DEFAULT_OTEL_COLLECTOR_OTLP_PORT)
    }

    /// Sets OTLP authentication token.
    pub fn with_otlp_auth(mut self, token: impl Into<String>) -> Self {
        self.otlp_auth_token = Some(token.into());
        self
    }

    /// Sets OTLP headers.
    pub fn with_otlp_headers(mut self, headers: HashMap<String, String>) -> Self {
        self.otlp_headers = headers;
        self
    }

    /// Sets whether to use secure connection for OTLP.
    pub fn with_otlp_secure(mut self, secure: bool) -> Self {
        self.otlp_secure = Some(secure);
        self
    }

    /// Sets the log level for this service/module.
    ///
    /// This acts as the code-level default. It can be overridden by the config file
    /// via `[logging.modules.<service-name>]` or env vars.
    ///
    /// Priority chain (highest to lowest):
    ///   env var > TOML per-module override > `with_log_level()` > environment default
    ///
    /// # Examples
    ///
    /// ```no_run
    /// use telemetry::{Telemetry, LogLevel};
    ///
    /// let telemetry = Telemetry::new()
    ///     .with_service("vision", "1.0.0")
    ///     .with_log_level(LogLevel::ModuleLevel_3)
    ///     .build()
    ///     .unwrap();
    /// ```
    pub fn with_log_level(mut self, level: options::LogLevel) -> Self {
        self.log_level = Some(level);
        self
    }

    /// Enables distributed tracing.
    pub fn with_tracing(mut self) -> Self {
        self.tracing_enabled = true;
        self
    }

    /// Enables profiling with Pyroscope.
    pub fn with_profiling(mut self, server_address: impl Into<String>) -> Self {
        self.profiling_address = Some(server_address.into());
        self
    }

    /// Enables MCAP recording to the specified file path.
    ///
    /// # Arguments
    ///
    /// * `path` - Path to the MCAP output file
    pub fn with_mcap(mut self, path: impl Into<String>) -> Self {
        self.mcap_path = Some(path.into());
        self
    }

    /// Builds and initializes the Telemetry instance.
    pub fn build(self) -> Result<Telemetry> {
        // Load config from file if available
        let config = if let Some(path) = &self.config_path {
            config::TelemetryConfig::load_from(path).ok()
        } else {
            config::TelemetryConfig::load().ok()
        };

        // Start with config values or defaults
        let (mut service_opts, mut telemetry_opts) = if let Some(cfg) = &config {
            (cfg.to_service_options(), cfg.to_telemetry_options())
        } else {
            let name = self.name.clone().unwrap_or_else(|| "unknown".to_string());
            let version = self.version.clone().unwrap_or_else(|| "0.0.0".to_string());
            (
                options::ServiceOptions::new(&name, &version),
                options::TelemetryOptions::new(),
            )
        };

        // If service was configured via code, ignore service-level config from file
        if self.service_from_code {
            let name = self
                .name
                .clone()
                .unwrap_or_else(|| "telemetry-service".to_string());
            let version = self.version.clone().unwrap_or_else(|| "1.0.0".to_string());
            service_opts = options::ServiceOptions::new(&name, &version)
                .with_description(self.description.as_deref().unwrap_or(""))
                .with_environment(
                    self.environment
                        .unwrap_or(options::Environment::Development),
                )
                .with_labels(self.labels.clone());
        } else {
            // Override with builder values (highest priority)
            if let Some(name) = self.name {
                service_opts.name = name;
            }
            if let Some(version) = self.version {
                service_opts.version = version;
            }
            if let Some(desc) = self.description {
                service_opts.description = desc;
            }
            if let Some(env) = self.environment {
                service_opts.environment = env;
            }

            // Merge labels (builder labels override config)
            for (k, v) in self.labels {
                service_opts.labels.insert(k, v);
            }
        }

        // Configure OTLP if specified via builder (overrides file; ensures telemetry is on)
        if let (Some(host), Some(port)) = (self.otlp_host.clone(), self.otlp_port) {
            telemetry_opts.telemetry.enabled = true;
            telemetry_opts.telemetry.otlp.enabled = true;
            telemetry_opts.telemetry.otlp.endpoint = format!("{}:{}", host, port);
            telemetry_opts.telemetry.otlp.host = host;
            telemetry_opts.telemetry.otlp.port = port;
        }

        if let Some(token) = self.otlp_auth_token {
            telemetry_opts.telemetry.otlp.auth_token = Some(token);
        }

        if !self.otlp_headers.is_empty() {
            telemetry_opts.telemetry.otlp.headers = self.otlp_headers;
        }

        if let Some(secure) = self.otlp_secure {
            telemetry_opts.telemetry.otlp.secure = secure;
        }

        // Apply code-level log level (only if config didn't already set a per-module override)
        if let Some(level) = self.log_level
            && !telemetry_opts
                .logging
                .modules
                .contains_key(&service_opts.name)
        {
            telemetry_opts
                .logging
                .modules
                .insert(service_opts.name.clone(), options::ModuleOptions { level });
        }

        // Tracing export: match telemetry-go — app tracing + OTLP must not disagree
        if self.tracing_enabled {
            telemetry_opts.telemetry.enabled = true;
            telemetry_opts.telemetry.otlp.enabled = true;
            telemetry_opts.telemetry.tracing.enabled = true;
        }

        // Configure MCAP if specified
        if let Some(path) = self.mcap_path {
            telemetry_opts.foxglove.enabled = true;
            telemetry_opts.foxglove.mcap_path = path;
        }

        Telemetry::init(service_opts, telemetry_opts)
    }
}
