//! Metrics collection and recording.
//!
//! This module provides functionality for collecting and recording metrics
//! to both OpenTelemetry and MCAP backends.

mod handles;
mod mcap;
#[allow(clippy::module_inception)]
pub mod metrics;

pub use handles::{CounterHandle, GaugeHandle, HistogramHandle};
pub use metrics::{MetricField, MetricType, Metrics, RecordMetrics};
