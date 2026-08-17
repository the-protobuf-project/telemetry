//! Traits for Telemetry functionality.
//!
//! This module defines traits that types can implement to integrate with Telemetry.

/// Trait for types that can be recorded as metrics.
///
/// Implement this trait to enable automatic metric recording for your types.
///
/// # Examples
///
/// ```no_run
/// use telemetry::traits::RecordMetrics;
/// use telemetry::metrics::{MetricField, MetricType};
///
/// struct MyMetrics {
///     counter: u64,
/// }
///
/// impl RecordMetrics for MyMetrics {
///     fn metric_fields(&self) -> Vec<MetricField> {
///         vec![
///             MetricField {
///                 name: "my_counter".to_string(),
///                 metric_type: MetricType::Counter,
///                 description: "My counter metric".to_string(),
///                 value: self.counter as f64,
///             }
///         ]
///     }
/// }
/// ```
pub trait RecordMetrics {
    /// Returns the metric fields for this type.
    fn metric_fields(&self) -> Vec<crate::metrics::MetricField>;
}
