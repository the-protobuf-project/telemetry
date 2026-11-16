//! OpenTelemetry telemetry integration.
//!
//! This module provides OpenTelemetry integration for distributed tracing
//! and metrics collection.

pub mod accounting;
pub mod provider;

pub use accounting::SignalStats;
pub use provider::TelemetryProvider;
