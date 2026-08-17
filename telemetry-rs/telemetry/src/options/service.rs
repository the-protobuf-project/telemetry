//! Service configuration options.
//!
//! This module defines service metadata and deployment environment configuration.

use serde::{Deserialize, Serialize};
use std::collections::HashMap;

/// Deployment environment types.
///
/// Represents different deployment environments for service configuration.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub enum Environment {
    #[default]
    Development,
    Staging,
    Production,
    Jetson,
}

impl std::fmt::Display for Environment {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Environment::Development => write!(f, "development"),
            Environment::Staging => write!(f, "staging"),
            Environment::Production => write!(f, "production"),
            Environment::Jetson => write!(f, "jetson"),
        }
    }
}

/// Service configuration options.
///
/// Contains metadata about the service including name, version, and environment.
///
/// # Examples
///
/// ```no_run
/// use telemetry::options::{ServiceOptions, Environment};
///
/// let opts = ServiceOptions::new("my-service", "1.0.0")
///     .with_description("My service description")
///     .with_environment(Environment::Production);
/// ```
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ServiceOptions {
    pub name: String,
    pub description: String,
    pub version: String,
    pub environment: Environment,
    /// Global labels added to ALL telemetry (logs, metrics, traces).
    #[serde(default)]
    pub labels: HashMap<String, String>,
}

impl ServiceOptions {
    /// Creates new service options with name and version.
    ///
    /// # Arguments
    ///
    /// * `name` - Service name
    /// * `version` - Service version
    pub fn new(name: impl Into<String>, version: impl Into<String>) -> Self {
        Self {
            name: name.into(),
            description: String::new(),
            version: version.into(),
            environment: Environment::default(),
            labels: HashMap::new(),
        }
    }

    /// Sets the service description.
    pub fn with_description(mut self, description: impl Into<String>) -> Self {
        self.description = description.into();
        self
    }

    /// Sets the deployment environment.
    pub fn with_environment(mut self, environment: Environment) -> Self {
        self.environment = environment;
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
}
