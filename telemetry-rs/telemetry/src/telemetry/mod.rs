//! OpenTelemetry telemetry integration.
//!
//! This module provides OpenTelemetry integration for distributed tracing
//! and metrics collection.

pub mod provider;

pub use provider::TelemetryProvider;
