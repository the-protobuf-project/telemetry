//! Metrics collection and recording implementation.
//!
//! This module provides the core metrics functionality including metric types,
//! the Metrics struct for recording, and derive macro support.

use anyhow::Result;
use opentelemetry::metrics::{Counter, Gauge, Histogram, Meter, MeterProvider};
use opentelemetry_sdk::metrics::SdkMeterProvider;
use std::collections::HashMap;
use std::sync::{Arc, Mutex, RwLock};

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
/// let metrics = Metrics::new(service_opts, None, None).unwrap();
///
/// metrics.counter("requests_total", 1.0).unwrap();
/// metrics.histogram("request_duration_ms", 42.5).unwrap();
/// metrics.gauge("active_connections", 10.0).unwrap();
/// ```
pub struct Metrics {
    mcap_writer: Option<Arc<Mutex<MetricMcapWriter>>>,
    meter: Option<Meter>,
    counters: Instruments<Counter<f64>>,
    histograms: Instruments<Histogram<f64>>,
    gauges: Instruments<Gauge<f64>>,
    service_name: String,
}

/// One kind of instrument by name, made on first use. Shared, so `Metrics`
/// records from any thread through `&self`: an existing instrument is found
/// under a read lock, which threads take at once.
struct Instruments<I> {
    by_name: RwLock<HashMap<String, I>>,
}

impl<I> Default for Instruments<I> {
    fn default() -> Self {
        Self {
            by_name: RwLock::new(HashMap::new()),
        }
    }
}

impl<I> Instruments<I> {
    /// Calls `f` with the instrument named `name`, made with `make` the first
    /// time; `make` is given the name to keep.
    fn with(&self, name: &str, make: impl FnOnce(&'static str) -> I, f: impl FnOnce(&I)) {
        if let Some(i) = self
            .by_name
            .read()
            .unwrap_or_else(|e| e.into_inner())
            .get(name)
        {
            return f(i);
        }
        let mut by_name = self.by_name.write().unwrap_or_else(|e| e.into_inner());
        // OpenTelemetry keeps instrument names for the program's life; leaked
        // once per instrument, not once per value.
        let i = by_name
            .entry(name.to_string())
            .or_insert_with(|| make(Box::leak(name.to_string().into_boxed_str())));
        f(i)
    }
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
            counters: Instruments::default(),
            histograms: Instruments::default(),
            gauges: Instruments::default(),
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
    /// let metrics = Metrics::new(service_opts, None, None).unwrap();
    /// let my_metrics = MyMetrics { counter: 42 };
    /// metrics.record(&my_metrics).unwrap();
    /// ```
    pub fn record<T: RecordMetrics>(&self, model: &T) -> Result<()> {
        for field in model.metric_fields() {
            // Prefix metric name with service name
            let prefixed_name = format!("{}.{}", self.service_name, field.name);
            self.record_dynamic(&prefixed_name, field.metric_type, field.value)?;
        }
        Ok(())
    }

    /// Records a metric dynamically with runtime type determination.
    fn record_dynamic(&self, name: &str, metric_type: MetricType, value: f64) -> Result<()> {
        match metric_type {
            MetricType::Counter => self.record_counter(name, value),
            MetricType::Histogram => self.record_histogram(name, value),
            MetricType::Gauge => self.record_gauge(name, value),
        }
    }

    /// Records a counter metric.
    fn record_counter(&self, name: &str, value: f64) -> Result<()> {
        if let Some(ref meter) = self.meter {
            self.counters.with(
                name,
                |n| meter.f64_counter(n).build(),
                |c| c.add(value, &[]),
            );
        }
        if let Some(ref writer) = self.mcap_writer {
            writer.lock().unwrap().write_counter(name, value)?;
        }
        Ok(())
    }

    /// Records a histogram metric.
    fn record_histogram(&self, name: &str, value: f64) -> Result<()> {
        if let Some(ref meter) = self.meter {
            self.histograms.with(
                name,
                |n| meter.f64_histogram(n).build(),
                |h| h.record(value, &[]),
            );
        }
        if let Some(ref writer) = self.mcap_writer {
            writer.lock().unwrap().write_histogram(name, value)?;
        }
        Ok(())
    }

    /// Records a gauge metric.
    fn record_gauge(&self, name: &str, value: f64) -> Result<()> {
        if let Some(ref meter) = self.meter {
            self.gauges.with(
                name,
                |n| meter.f64_gauge(n).build(),
                |g| g.record(value, &[]),
            );
        }
        if let Some(ref writer) = self.mcap_writer {
            writer.lock().unwrap().write_gauge(name, value)?;
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
    pub fn counter(&self, name: &'static str, value: f64) -> Result<()> {
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
    pub fn histogram(&self, name: &'static str, value: f64) -> Result<()> {
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
    pub fn gauge(&self, name: &'static str, value: f64) -> Result<()> {
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
