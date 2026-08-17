//! Metrics collection and recording implementation.
//!
//! This module provides the core metrics functionality including metric types,
//! the Metrics struct for recording, and derive macro support.

use anyhow::Result;
use opentelemetry::metrics::{Counter, Gauge, Histogram, Meter, MeterProvider};
use opentelemetry_sdk::metrics::SdkMeterProvider;
use std::collections::HashMap;
use std::sync::{Arc, Mutex};

use super::mcap::MetricMcapWriter;
use crate::foxglove::UnifiedMcapWriter;
use crate::options::ServiceOptions;

/// Types of metrics supported.
///
/// # Examples
///
/// ```no_run
/// use telemetry::metrics::MetricType;
///
/// let counter_type = MetricType::Counter;
/// let histogram_type = MetricType::Histogram;
/// let gauge_type = MetricType::Gauge;
/// ```
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum MetricType {
    Counter,
    Histogram,
    Gauge,
}

/// A metric field with metadata.
///
/// Used by the derive macro and manual implementations of `RecordMetrics`.
#[derive(Debug, Clone)]
pub struct MetricField {
    pub name: String,
    pub metric_type: MetricType,
    pub description: String,
    pub value: f64,
}

pub use crate::traits::RecordMetrics;

/// Metrics recorder for collecting and exporting metrics.
///
/// Records metrics to both OpenTelemetry and MCAP backends.
///
/// # Examples
///
/// ```no_run
/// use telemetry::Metrics;
/// use telemetry::options::ServiceOptions;
///
/// let service_opts = ServiceOptions::new("my-service", "1.0.0");
/// let mut metrics = Metrics::new(service_opts, None, None).unwrap();
///
/// metrics.counter("requests_total", 1.0).unwrap();
/// metrics.histogram("request_duration_ms", 42.5).unwrap();
/// metrics.gauge("active_connections", 10.0).unwrap();
/// ```
pub struct Metrics {
    mcap_writer: Option<Arc<Mutex<MetricMcapWriter>>>,
    meter: Option<Meter>,
    counters: HashMap<String, Counter<f64>>,
    histograms: HashMap<String, Histogram<f64>>,
    gauges: HashMap<String, Gauge<f64>>,
    service_name: String,
}

impl Metrics {
    /// Creates a new metrics recorder.
    ///
    /// # Arguments
    ///
    /// * `service_opts` - Service configuration
    /// * `mcap_writer` - Optional MCAP writer for recording metrics
    /// * `meter_provider` - Optional OpenTelemetry meter provider
    pub fn new(
        service_opts: ServiceOptions,
        mcap_writer: Option<Arc<Mutex<UnifiedMcapWriter>>>,
        meter_provider: Option<Arc<SdkMeterProvider>>,
    ) -> Result<Self> {
        let service_name = service_opts.name.clone();
        let meter_name = service_name.clone();
        let meter = meter_provider.map(|p| p.meter(meter_name.leak() as &'static str));

        let mcap_metric_writer = mcap_writer.map(|w| {
            Arc::new(Mutex::new(
                MetricMcapWriter::new(&service_opts, w).expect("Failed to create metric writer"),
            ))
        });

        Ok(Self {
            mcap_writer: mcap_metric_writer,
            meter,
            counters: HashMap::new(),
            histograms: HashMap::new(),
            gauges: HashMap::new(),
            service_name,
        })
    }

    /// Records metrics from a type implementing `RecordMetrics`.
    ///
    /// # Arguments
    ///
    /// * `model` - Object implementing RecordMetrics trait
    ///
    /// # Examples
    ///
    /// ```no_run
    /// use telemetry::{Metrics, derive::Metrics as MetricsDerive};
    /// use telemetry::options::ServiceOptions;
    ///
    /// #[derive(MetricsDerive)]
    /// struct MyMetrics {
    ///     #[metric(name = "my_counter", counter, description = "A counter")]
    ///     counter: u64,
    /// }
    ///
    /// let service_opts = ServiceOptions::new("my-service", "1.0.0");
    /// let mut metrics = Metrics::new(service_opts, None, None).unwrap();
    /// let my_metrics = MyMetrics { counter: 42 };
    /// metrics.record(&my_metrics).unwrap();
    /// ```
    pub fn record<T: RecordMetrics>(&mut self, model: &T) -> Result<()> {
        for field in model.metric_fields() {
            // Prefix metric name with service name
            let prefixed_name = format!("{}.{}", self.service_name, field.name);
            self.record_dynamic(&prefixed_name, field.metric_type, field.value)?;
        }
        Ok(())
    }

    /// Records a metric dynamically with runtime type determination.
    fn record_dynamic(&mut self, name: &str, metric_type: MetricType, value: f64) -> Result<()> {
        // Convert to 'static by leaking - this is acceptable for metric names which are typically
        // a small, fixed set defined at compile time via the derive macro
        let static_name: &'static str = Box::leak(name.to_string().into_boxed_str());

        // OTel with static name
        if let Some(ref meter) = self.meter {
            match metric_type {
                MetricType::Counter => {
                    let counter = self
                        .counters
                        .entry(name.to_string())
                        .or_insert_with(|| meter.f64_counter(static_name).build());
                    counter.add(value, &[]);
                }
                MetricType::Histogram => {
                    let histogram = self
                        .histograms
                        .entry(name.to_string())
                        .or_insert_with(|| meter.f64_histogram(static_name).build());
                    histogram.record(value, &[]);
                }
                MetricType::Gauge => {
                    let gauge = self
                        .gauges
                        .entry(name.to_string())
                        .or_insert_with(|| meter.f64_gauge(static_name).build());
                    gauge.record(value, &[]);
                }
            }
        }

        // MCAP
        if let Some(ref writer) = self.mcap_writer {
            let mut w = writer.lock().unwrap();
            match metric_type {
                MetricType::Counter => w.write_counter(name, value)?,
                MetricType::Histogram => w.write_histogram(name, value)?,
                MetricType::Gauge => w.write_gauge(name, value)?,
            }
        }
        Ok(())
    }

    /// Records a counter metric with static name.
    fn record_counter(&mut self, name: &'static str, value: f64) -> Result<()> {
        // OTel counter - create once and cache
        if let Some(ref meter) = self.meter {
            let counter = self
                .counters
                .entry(name.to_string())
                .or_insert_with(|| meter.f64_counter(name).build());
            counter.add(value, &[]);
        }

        // MCAP
        if let Some(ref writer) = self.mcap_writer {
            let mut w = writer.lock().unwrap();
            w.write_counter(name, value)?;
        }
        Ok(())
    }

    /// Records a histogram metric with static name.
    fn record_histogram(&mut self, name: &'static str, value: f64) -> Result<()> {
        // OTel histogram - create once and cache
        if let Some(ref meter) = self.meter {
            let histogram = self
                .histograms
                .entry(name.to_string())
                .or_insert_with(|| meter.f64_histogram(name).build());
            histogram.record(value, &[]);
        }

        // MCAP
        if let Some(ref writer) = self.mcap_writer {
            let mut w = writer.lock().unwrap();
            w.write_histogram(name, value)?;
        }
        Ok(())
    }

    /// Records a gauge metric with static name.
    fn record_gauge(&mut self, name: &'static str, value: f64) -> Result<()> {
        // OTel gauge - create once and cache
        if let Some(ref meter) = self.meter {
            let gauge = self
                .gauges
                .entry(name.to_string())
                .or_insert_with(|| meter.f64_gauge(name).build());
            gauge.record(value, &[]);
        }

        // MCAP
        if let Some(ref writer) = self.mcap_writer {
            let mut w = writer.lock().unwrap();
            w.write_gauge(name, value)?;
        }
        Ok(())
    }

    /// Records a counter metric.
    ///
    /// Counters are monotonically increasing values.
    ///
    /// # Arguments
    ///
    /// * `name` - Metric name (must be static)
    /// * `value` - Value to add to the counter
    pub fn counter(&mut self, name: &'static str, value: f64) -> Result<()> {
        self.record_counter(name, value)
    }

    /// Records a histogram metric.
    ///
    /// Histograms track distributions of values.
    ///
    /// # Arguments
    ///
    /// * `name` - Metric name (must be static)
    /// * `value` - Value to record
    pub fn histogram(&mut self, name: &'static str, value: f64) -> Result<()> {
        self.record_histogram(name, value)
    }

    /// Records a gauge metric.
    ///
    /// Gauges represent point-in-time values that can go up or down.
    ///
    /// # Arguments
    ///
    /// * `name` - Metric name (must be static)
    /// * `value` - Current value
    pub fn gauge(&mut self, name: &'static str, value: f64) -> Result<()> {
        self.record_gauge(name, value)
    }
}

#[macro_export]
macro_rules! impl_metrics {
    ($name:ident { $( $field:ident : $metric_type:ident => $metric_name:literal ),* $(,)? }) => {
        impl $crate::metrics::RecordMetrics for $name {
            fn metric_fields(&self) -> Vec<$crate::metrics::MetricField> {
                vec![
                    $(
                        $crate::metrics::MetricField {
                            name: $metric_name.to_string(),
                            metric_type: $crate::impl_metrics!(@type $metric_type),
                            value: self.$field as f64,
                        },
                    )*
                ]
            }
        }
    };

    (@type counter) => { $crate::metrics::MetricType::Counter };
    (@type histogram) => { $crate::metrics::MetricType::Histogram };
    (@type gauge) => { $crate::metrics::MetricType::Gauge };
}

pub use impl_metrics;
