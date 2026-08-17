//! Schema registry for Foxglove MCAP schemas.
//!
//! This module manages JSON schemas for various message types that can be
//! written to MCAP files and visualized in Foxglove Studio.

use std::collections::HashMap;

const FOXGLOVE_LOG_SCHEMA: &str = include_str!("schemas/foxglove.Log.json");
const METRIC_SCHEMA: &str = include_str!("schemas/the-protobuf-project.metric.json");
const TRACE_SCHEMA: &str = include_str!("schemas/the-protobuf-project.trace.json");

/// Registry for managing MCAP message schemas.
///
/// Stores JSON schemas for different message types used in MCAP files.
pub struct SchemaRegistry {
    schemas: HashMap<String, String>,
}

impl SchemaRegistry {
    /// Creates a new schema registry with built-in schemas.
    ///
    /// # Examples
    ///
    /// ```no_run
    /// use telemetry::foxglove::SchemaRegistry;
    ///
    /// let registry = SchemaRegistry::new();
    /// ```
    pub fn new() -> Self {
        let mut registry = Self {
            schemas: HashMap::new(),
        };
        registry.register_builtin_schemas();
        registry
    }

    /// Registers the built-in schemas (Foxglove Log, metrics, traces).
    fn register_builtin_schemas(&mut self) {
        self.schemas
            .insert("foxglove.Log".to_string(), FOXGLOVE_LOG_SCHEMA.to_string());
        self.schemas.insert(
            "the-protobuf-project.metric".to_string(),
            METRIC_SCHEMA.to_string(),
        );
        self.schemas.insert(
            "the-protobuf-project.trace".to_string(),
            TRACE_SCHEMA.to_string(),
        );
    }

    /// Registers a custom schema.
    ///
    /// # Arguments
    ///
    /// * `name` - Schema name
    /// * `schema` - JSON schema definition
    pub fn register(&mut self, name: String, schema: String) {
        self.schemas.insert(name, schema);
    }

    /// Retrieves a schema by name.
    ///
    /// # Arguments
    ///
    /// * `name` - Schema name to retrieve
    pub fn get(&self, name: &str) -> Option<&String> {
        self.schemas.get(name)
    }

    /// Lists all registered schema names.
    pub fn list(&self) -> Vec<&String> {
        self.schemas.keys().collect()
    }
}

impl Default for SchemaRegistry {
    fn default() -> Self {
        Self::new()
    }
}
