//! Metrics collection and recording implementation.
//!
//! This module provides the core metrics functionality including metric types,
//! the Metrics struct for recording, and derive macro support.

use anyhow::Result;
use opentelemetry::metrics::{Meter, MeterProvider};
use opentelemetry_sdk::metrics::SdkMeterProvider;
use std::any::Any;
use std::cell::RefCell;
use std::collections::HashMap;
use std::hash::{BuildHasherDefault, Hasher};
use std::sync::atomic::{AtomicU64, Ordering::Relaxed};
use std::sync::{Arc, Mutex, RwLock};
use std::thread::LocalKey;

use super::handles::{
    CounterCell, CounterHandle, GaugeCell, GaugeHandle, HistogramHandle, McapWriter,
};
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
    mcap_writer: McapWriter,
    meter: Option<Meter>,
    /// Tells this recorder's entries apart in the per-thread name caches.
    id: u64,
    counters: Instruments<CounterHandle>,
    histograms: Instruments<HistogramHandle>,
    gauges: Instruments<GaugeHandle>,
    service_name: String,
}

/// One kind of instrument by name, made on first use. Shared, so `Metrics`
/// records from any thread through `&self`.
struct Instruments<H> {
    by_name: RwLock<HashMap<String, Registered<H>>>,
}

struct Registered<H> {
    handle: H,
    /// The OpenTelemetry instrument reading the handle's value, kept for as
    /// long as the handle can be recorded to.
    _observer: Option<Box<dyn Any + Send + Sync>>,
}

impl<H> Default for Instruments<H> {
    fn default() -> Self {
        Self {
            by_name: RwLock::new(HashMap::new()),
        }
    }
}

impl<H: Clone> Instruments<H> {
    /// The handle named `name`, made with `make` the first time; `make` is
    /// given the name to keep.
    fn get(
        &self,
        name: &str,
        make: impl FnOnce(&'static str) -> (H, Option<Box<dyn Any + Send + Sync>>),
    ) -> H {
        if let Some(r) = self
            .by_name
            .read()
            .unwrap_or_else(|e| e.into_inner())
            .get(name)
        {
            return r.handle.clone();
        }
        let mut by_name = self.by_name.write().unwrap_or_else(|e| e.into_inner());
        by_name
            .entry(name.to_string())
            .or_insert_with(|| {
                // OpenTelemetry keeps instrument names for the program's
                // life; leaked once per instrument, not once per value.
                let (handle, observer) = make(Box::leak(name.to_string().into_boxed_str()));
                Registered {
                    handle,
                    _observer: observer,
                }
            })
            .handle
            .clone()
    }
}

/// Key of a per-thread name cache entry: the recorder's id and the address
/// and length of the `&'static str` name. Equal names at different addresses
/// get separate entries for the same handle.
type NameKey = (u64, usize, usize);

/// Hashes a [`NameKey`] in a few instructions; the default hasher would cost
/// more than the add it looks up.
#[derive(Default)]
struct NameKeyHasher(u64);

impl Hasher for NameKeyHasher {
    fn finish(&self) -> u64 {
        self.0
    }
    fn write(&mut self, bytes: &[u8]) {
        for &b in bytes {
            self.write_u64(b as u64);
        }
    }
    fn write_u64(&mut self, n: u64) {
        self.0 = (self.0.rotate_left(5) ^ n).wrapping_mul(0x51_7c_c1_b7_27_22_0a_95);
    }
    fn write_usize(&mut self, n: usize) {
        self.write_u64(n as u64);
    }
}

type NameCache<H> = RefCell<HashMap<NameKey, H, BuildHasherDefault<NameKeyHasher>>>;

thread_local! {
    // By-name recording finds its handle here without touching the shared
    // registry, whose lock every thread would otherwise contend on.
    static COUNTER_NAMES: NameCache<CounterHandle> = RefCell::default();
    static GAUGE_NAMES: NameCache<GaugeHandle> = RefCell::default();
    static HISTOGRAM_NAMES: NameCache<HistogramHandle> = RefCell::default();
}

/// Calls `f` with the handle for the static `name`, from the thread's cache,
/// or from `lookup` the first time this thread uses the name.
#[inline]
fn cached<H: Clone + 'static, R>(
    cache: &'static LocalKey<NameCache<H>>,
    id: u64,
    name: &'static str,
    lookup: impl FnOnce() -> H,
    f: impl FnOnce(&H) -> R,
) -> R {
    let key = (id, name.as_ptr() as usize, name.len());
    // `f` runs while the handle stays in the cache, so a hit does not touch
    // the handle's shared reference count.
    let mut f = Some(f);
    let hit = cache
        .try_with(|c| {
            let c = c.try_borrow().ok()?;
            let h = c.get(&key)?;
            f.take().map(|f| f(h))
        })
        .ok()
        .flatten();
    if let Some(r) = hit {
        return r;
    }
    let h = lookup();
    // Fails only while thread-local storage is torn down; then every call
    // takes the registry path.
    let _ = cache.try_with(|c| {
        if let Ok(mut c) = c.try_borrow_mut() {
            c.insert(key, h.clone());
        }
    });
    (f.take().expect("f runs once"))(&h)
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
        static NEXT_ID: AtomicU64 = AtomicU64::new(0);

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
            id: NEXT_ID.fetch_add(1, Relaxed),
            counters: Instruments::default(),
            histograms: Instruments::default(),
            gauges: Instruments::default(),
            service_name,
        })
    }

    /// Returns a handle to the counter named `name`, creating it on first
    /// use. Recording through a kept handle skips the lookup by name.
    ///
    /// # Examples
    ///
    /// ```no_run
    /// use telemetry::Metrics;
    /// use telemetry::options::ServiceOptions;
    ///
    /// let metrics = Metrics::new(ServiceOptions::new("my-service", "1.0.0"), None, None).unwrap();
    /// let ops = metrics.counter_handle("ops");
    /// for _ in 0..1000 {
    ///     ops.add(1.0).unwrap();
    /// }
    /// ```
    pub fn counter_handle(&self, name: &str) -> CounterHandle {
        self.counters.get(name, |name| {
            let cell = Arc::new(CounterCell::new());
            let observer = self.meter.as_ref().map(|meter| {
                let cell = Arc::clone(&cell);
                Box::new(
                    meter
                        .f64_observable_counter(name)
                        .with_callback(move |o| o.observe(cell.sum(), &[]))
                        .build(),
                ) as Box<dyn Any + Send + Sync>
            });
            let handle = CounterHandle {
                cell,
                name,
                mcap: self.mcap_writer.clone(),
            };
            (handle, observer)
        })
    }

    /// Returns a handle to the gauge named `name`, creating it on first use.
    pub fn gauge_handle(&self, name: &str) -> GaugeHandle {
        self.gauges.get(name, |name| {
            let cell = Arc::new(GaugeCell::default());
            let observer = self.meter.as_ref().map(|meter| {
                let cell = Arc::clone(&cell);
                Box::new(
                    meter
                        .f64_observable_gauge(name)
                        .with_callback(move |o| {
                            if let Some(v) = cell.get() {
                                o.observe(v, &[]);
                            }
                        })
                        .build(),
                ) as Box<dyn Any + Send + Sync>
            });
            let handle = GaugeHandle {
                cell,
                name,
                mcap: self.mcap_writer.clone(),
            };
            (handle, observer)
        })
    }

    /// Returns a handle to the histogram named `name`, creating it on first
    /// use.
    pub fn histogram_handle(&self, name: &str) -> HistogramHandle {
        self.histograms.get(name, |name| {
            let handle = HistogramHandle {
                histogram: self.meter.as_ref().map(|m| m.f64_histogram(name).build()),
                name,
                mcap: self.mcap_writer.clone(),
            };
            (handle, None)
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
            match field.metric_type {
                MetricType::Counter => self.counter_handle(&prefixed_name).add(field.value)?,
                MetricType::Histogram => {
                    self.histogram_handle(&prefixed_name).record(field.value)?
                }
                MetricType::Gauge => self.gauge_handle(&prefixed_name).record(field.value)?,
            }
        }
        Ok(())
    }

    /// Records a counter metric.
    ///
    /// Counters are monotonically increasing values; negative and NaN values
    /// are ignored. To record often, keep a [`Metrics::counter_handle`].
    ///
    /// # Arguments
    ///
    /// * `name` - Metric name (must be static)
    /// * `value` - Value to add to the counter
    pub fn counter(&self, name: &'static str, value: f64) -> Result<()> {
        cached(
            &COUNTER_NAMES,
            self.id,
            name,
            || self.counter_handle(name),
            |h| h.add(value),
        )
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
        cached(
            &HISTOGRAM_NAMES,
            self.id,
            name,
            || self.histogram_handle(name),
            |h| h.record(value),
        )
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
        cached(
            &GAUGE_NAMES,
            self.id,
            name,
            || self.gauge_handle(name),
            |h| h.record(value),
        )
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

#[cfg(test)]
mod tests {
    use super::*;
    use opentelemetry_sdk::metrics::data::{AggregatedMetrics, MetricData};
    use opentelemetry_sdk::metrics::{InMemoryMetricExporter, PeriodicReader};

    fn recorder() -> (Metrics, Arc<SdkMeterProvider>, InMemoryMetricExporter) {
        let exporter = InMemoryMetricExporter::default();
        let provider = Arc::new(
            SdkMeterProvider::builder()
                .with_reader(PeriodicReader::builder(exporter.clone()).build())
                .build(),
        );
        let metrics = Metrics::new(
            ServiceOptions::new("metrics-test", "1.0.0"),
            None,
            Some(Arc::clone(&provider)),
        )
        .unwrap();
        (metrics, provider, exporter)
    }

    /// The last exported value of the sum or gauge named `name`.
    fn last_value(exporter: &InMemoryMetricExporter, name: &str) -> Option<f64> {
        let mut last = None;
        for rm in exporter.get_finished_metrics().unwrap() {
            for m in rm.scope_metrics().flat_map(|s| s.metrics()) {
                if m.name() != name {
                    continue;
                }
                last = match m.data() {
                    AggregatedMetrics::F64(MetricData::Sum(s)) => {
                        s.data_points().map(|p| p.value()).last()
                    }
                    AggregatedMetrics::F64(MetricData::Gauge(g)) => {
                        g.data_points().map(|p| p.value()).last()
                    }
                    _ => None,
                }
                .or(last);
            }
        }
        last
    }

    #[test]
    fn counter_exports_the_sum_across_threads() {
        let (metrics, provider, exporter) = recorder();
        let ops = metrics.counter_handle("ops");
        std::thread::scope(|s| {
            for _ in 0..8 {
                s.spawn(|| {
                    for _ in 0..10_000 {
                        ops.add(1.0).unwrap();
                        metrics.counter("ops", 0.5).unwrap();
                        metrics.counter("named_ops", 2.0).unwrap();
                    }
                });
            }
        });
        provider.shutdown().unwrap();
        assert_eq!(last_value(&exporter, "ops"), Some(120_000.0));
        assert_eq!(last_value(&exporter, "named_ops"), Some(160_000.0));
    }

    #[test]
    fn counter_ignores_negative_and_nan_values() {
        let (metrics, provider, exporter) = recorder();
        metrics.counter("ops", 3.0).unwrap();
        metrics.counter("ops", -1.0).unwrap();
        metrics.counter("ops", f64::NAN).unwrap();
        provider.shutdown().unwrap();
        assert_eq!(last_value(&exporter, "ops"), Some(3.0));
    }

    #[test]
    fn gauge_exports_the_last_value() {
        let (metrics, provider, exporter) = recorder();
        let temp = metrics.gauge_handle("temperature");
        temp.record(20.0).unwrap();
        temp.record(21.5).unwrap();
        metrics.gauge("temperature", 19.0).unwrap();
        // Made but never recorded: nothing to report.
        metrics.gauge_handle("idle");
        provider.shutdown().unwrap();
        assert_eq!(last_value(&exporter, "temperature"), Some(19.0));
        assert_eq!(last_value(&exporter, "idle"), None);
    }

    #[test]
    fn shutdown_exports_what_was_added_after_the_last_collection() {
        let (metrics, provider, exporter) = recorder();
        let ops = metrics.counter_handle("ops");
        ops.add(5.0).unwrap();
        provider.force_flush().unwrap();
        assert_eq!(last_value(&exporter, "ops"), Some(5.0));
        ops.add(7.0).unwrap();
        provider.shutdown().unwrap();
        assert_eq!(last_value(&exporter, "ops"), Some(12.0));
    }

    #[test]
    fn record_and_histograms_still_export() {
        struct Batch {
            items: u64,
        }
        impl RecordMetrics for Batch {
            fn metric_fields(&self) -> Vec<MetricField> {
                vec![MetricField {
                    name: "items".into(),
                    metric_type: MetricType::Counter,
                    description: "Items".into(),
                    value: self.items as f64,
                }]
            }
        }
        let (metrics, provider, exporter) = recorder();
        metrics.record(&Batch { items: 4 }).unwrap();
        metrics.record(&Batch { items: 6 }).unwrap();
        metrics.histogram("latency_ms", 12.0).unwrap();
        provider.shutdown().unwrap();
        assert_eq!(last_value(&exporter, "metrics-test.items"), Some(10.0));
        let histogram_count: u64 = exporter
            .get_finished_metrics()
            .unwrap()
            .iter()
            .flat_map(|rm| rm.scope_metrics().flat_map(|s| s.metrics()))
            .filter(|m| m.name() == "latency_ms")
            .filter_map(|m| match m.data() {
                AggregatedMetrics::F64(MetricData::Histogram(h)) => {
                    h.data_points().map(|p| p.count()).last()
                }
                _ => None,
            })
            .last()
            .unwrap();
        assert_eq!(histogram_count, 1);
    }
}
