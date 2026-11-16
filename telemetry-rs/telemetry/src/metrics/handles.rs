//! Instrument handles: a named counter, gauge or histogram looked up once and
//! then recorded to directly.
//!
//! Counters and gauges keep their value here, in atomics, and OpenTelemetry
//! reads it through an observable instrument when it collects. A synchronous
//! upstream counter takes a shared lock and hashes the attributes on every
//! add, which is what made counting slow, and slower still across threads.

use anyhow::Result;
use opentelemetry::metrics::Histogram;
use std::cell::Cell;
use std::sync::atomic::{AtomicBool, AtomicU64, AtomicUsize, Ordering::*};
use std::sync::{Arc, Mutex, OnceLock};

use super::mcap::MetricMcapWriter;

pub(crate) type McapWriter = Option<Arc<Mutex<MetricMcapWriter>>>;

/// Integral increments below this are added as integers: exact, and a
/// cheaper add than a float one.
const EXACT_INT: f64 = (1u64 << 53) as f64;

/// A thread's slot index before it has asked for one.
const UNLEASED: usize = usize::MAX;
/// The index of a thread that found every slot taken: it adds to the shared
/// overflow slot instead.
const OVERFLOW: usize = usize::MAX - 1;

/// Slots per counter. Each live thread owns at most one slot index, the same
/// one in every counter, so it is the only writer of that slot: an add is a
/// plain load and store, with no read-modify-write and no cache line shared
/// with another thread.
fn slot_count() -> usize {
    static COUNT: OnceLock<usize> = OnceLock::new();
    *COUNT.get_or_init(|| {
        let cores = std::thread::available_parallelism().map_or(8, |n| n.get());
        (cores * 4).clamp(16, 1024)
    })
}

/// Slot indexes handed to threads, and the ones threads gave back on exit.
static NEXT_SLOT: AtomicUsize = AtomicUsize::new(0);
static FREE_SLOTS: Mutex<Vec<usize>> = Mutex::new(Vec::new());

thread_local! {
    static SLOT: Cell<usize> = const { Cell::new(UNLEASED) };
    static LEASE: Lease = const { Lease };
}

/// Returns the thread's slot index to the pool when the thread exits. What
/// the thread added stays in its slots; the next owner adds on top.
struct Lease;

impl Drop for Lease {
    fn drop(&mut self) {
        let index = SLOT.with(|s| s.replace(OVERFLOW));
        if index < slot_count() {
            FREE_SLOTS
                .lock()
                .unwrap_or_else(|e| e.into_inner())
                .push(index);
        }
    }
}

/// The calling thread's slot index, leased on first use.
#[inline]
fn slot_index() -> usize {
    match SLOT.try_with(Cell::get) {
        Ok(UNLEASED) => lease(),
        Ok(index) => index,
        // Thread-local storage is being torn down.
        Err(_) => OVERFLOW,
    }
}

#[cold]
fn lease() -> usize {
    // Without a lease to give the index back, the slot would be lost when
    // the thread exits; register it first.
    if LEASE.try_with(|_| ()).is_err() {
        return OVERFLOW;
    }
    let index = FREE_SLOTS
        .lock()
        .unwrap_or_else(|e| e.into_inner())
        .pop()
        .or_else(|| {
            let n = NEXT_SLOT.fetch_add(1, Relaxed);
            (n < slot_count()).then_some(n)
        })
        .unwrap_or(OVERFLOW);
    SLOT.with(|s| s.set(index));
    index
}

/// One thread's share of a counter, on a cache line of its own.
#[derive(Default)]
#[repr(align(128))]
struct Slot {
    /// Sum of integral increments.
    ints: AtomicU64,
    /// Sum of the other increments, as f64 bits.
    floats: AtomicU64,
}

impl Slot {
    /// Adds as the slot's only writer.
    #[inline]
    fn add_owned(&self, value: f64) {
        if value < EXACT_INT && value.fract() == 0.0 {
            self.ints
                .store(self.ints.load(Relaxed).wrapping_add(value as u64), Relaxed);
        } else {
            let sum = f64::from_bits(self.floats.load(Relaxed)) + value;
            self.floats.store(sum.to_bits(), Relaxed);
        }
    }

    /// Adds alongside other writers.
    fn add_shared(&self, value: f64) {
        if value < EXACT_INT && value.fract() == 0.0 {
            self.ints.fetch_add(value as u64, Relaxed);
        } else {
            let mut bits = self.floats.load(Relaxed);
            while let Err(now) = self.floats.compare_exchange_weak(
                bits,
                (f64::from_bits(bits) + value).to_bits(),
                Relaxed,
                Relaxed,
            ) {
                bits = now;
            }
        }
    }

    fn read(&self) -> (u64, f64) {
        (
            self.ints.load(Relaxed),
            f64::from_bits(self.floats.load(Relaxed)),
        )
    }
}

/// A counter's running total, split across per-thread slots.
pub(crate) struct CounterCell {
    slots: Box<[Slot]>,
    overflow: Slot,
}

impl CounterCell {
    pub(crate) fn new() -> Self {
        Self {
            slots: (0..slot_count()).map(|_| Slot::default()).collect(),
            overflow: Slot::default(),
        }
    }

    #[inline]
    fn add(&self, value: f64) {
        match self.slots.get(slot_index()) {
            Some(slot) => slot.add_owned(value),
            None => self.overflow.add_shared(value),
        }
    }

    /// The total so far; called by the observable counter at collection.
    pub(crate) fn sum(&self) -> f64 {
        let (ints, floats) = self
            .slots
            .iter()
            .chain(std::iter::once(&self.overflow))
            .map(Slot::read)
            .fold((0u64, 0.0f64), |(i, f), (si, sf)| {
                (i.wrapping_add(si), f + sf)
            });
        ints as f64 + floats
    }
}

/// A gauge's last recorded value.
#[derive(Default)]
pub(crate) struct GaugeCell {
    bits: AtomicU64,
    set: AtomicBool,
}

impl GaugeCell {
    #[inline]
    fn set(&self, value: f64) {
        self.bits.store(value.to_bits(), Relaxed);
        // Written once, so recording stays a single store afterwards.
        if !self.set.load(Relaxed) {
            self.set.store(true, Release);
        }
    }

    /// The last value, or `None` before the first; called by the observable
    /// gauge at collection.
    pub(crate) fn get(&self) -> Option<f64> {
        self.set
            .load(Acquire)
            .then(|| f64::from_bits(self.bits.load(Relaxed)))
    }
}

/// A counter looked up once by [`Metrics::counter_handle`](super::Metrics::counter_handle).
///
/// Cheap to clone and to share between threads. Adding is a few
/// nanoseconds and touches no memory another thread writes; OpenTelemetry
/// reads the total when it collects, including the final collection at
/// shutdown.
///
/// # Examples
///
/// ```no_run
/// use telemetry::Metrics;
/// use telemetry::options::ServiceOptions;
///
/// let metrics = Metrics::new(ServiceOptions::new("my-service", "1.0.0"), None, None).unwrap();
/// let requests = metrics.counter_handle("requests_total");
/// requests.add(1.0).unwrap();
/// ```
#[derive(Clone)]
pub struct CounterHandle {
    pub(crate) cell: Arc<CounterCell>,
    pub(crate) name: &'static str,
    pub(crate) mcap: McapWriter,
}

impl CounterHandle {
    /// Adds `value` to the counter. Counters only go up: negative and NaN
    /// values are ignored, as the OpenTelemetry API asks of a counter.
    #[inline]
    pub fn add(&self, value: f64) -> Result<()> {
        // Written so that NaN fails it too.
        #[allow(clippy::neg_cmp_op_on_partial_ord)]
        if !(value >= 0.0) {
            return Ok(());
        }
        self.cell.add(value);
        if let Some(ref writer) = self.mcap {
            writer
                .lock()
                .unwrap_or_else(|e| e.into_inner())
                .write_counter(self.name, value)?;
        }
        Ok(())
    }
}

/// A gauge looked up once by [`Metrics::gauge_handle`](super::Metrics::gauge_handle).
///
/// Cheap to clone and to share between threads. OpenTelemetry reports the
/// last value recorded when it collects.
#[derive(Clone)]
pub struct GaugeHandle {
    pub(crate) cell: Arc<GaugeCell>,
    pub(crate) name: &'static str,
    pub(crate) mcap: McapWriter,
}

impl GaugeHandle {
    /// Sets the gauge to `value`.
    #[inline]
    pub fn record(&self, value: f64) -> Result<()> {
        self.cell.set(value);
        if let Some(ref writer) = self.mcap {
            writer
                .lock()
                .unwrap_or_else(|e| e.into_inner())
                .write_gauge(self.name, value)?;
        }
        Ok(())
    }
}

/// A histogram looked up once by [`Metrics::histogram_handle`](super::Metrics::histogram_handle).
///
/// Cheap to clone and to share between threads. Records go straight to the
/// OpenTelemetry histogram, which keeps the buckets.
#[derive(Clone)]
pub struct HistogramHandle {
    pub(crate) histogram: Option<Histogram<f64>>,
    pub(crate) name: &'static str,
    pub(crate) mcap: McapWriter,
}

impl HistogramHandle {
    /// Records `value` in the histogram.
    #[inline]
    pub fn record(&self, value: f64) -> Result<()> {
        if let Some(ref h) = self.histogram {
            h.record(value, &[]);
        }
        if let Some(ref writer) = self.mcap {
            writer
                .lock()
                .unwrap_or_else(|e| e.into_inner())
                .write_histogram(self.name, value)?;
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::Barrier;

    #[test]
    fn counter_cell_sums_integral_and_fractional_adds() {
        let cell = CounterCell::new();
        for _ in 0..10 {
            cell.add(1.0);
            cell.add(0.25);
        }
        assert_eq!(cell.sum(), 12.5);
    }

    #[test]
    fn counter_cell_is_exact_with_more_threads_than_slots() {
        // Every thread alive at once, so the ones past the slot count share
        // the overflow slot.
        let threads = slot_count() + 8;
        let cell = CounterCell::new();
        let barrier = Barrier::new(threads);
        std::thread::scope(|s| {
            for _ in 0..threads {
                s.spawn(|| {
                    barrier.wait();
                    for _ in 0..1000 {
                        cell.add(1.0);
                        cell.add(0.5);
                    }
                    barrier.wait();
                });
            }
        });
        assert_eq!(cell.sum(), threads as f64 * 1500.0);
    }

    #[test]
    fn slots_given_back_by_exited_threads_keep_their_counts() {
        let cell = CounterCell::new();
        for _ in 0..3 {
            std::thread::scope(|s| {
                for _ in 0..4 {
                    s.spawn(|| (0..100).for_each(|_| cell.add(1.0)));
                }
            });
        }
        assert_eq!(cell.sum(), 1200.0);
    }

    #[test]
    fn gauge_cell_is_unset_until_recorded() {
        let cell = GaugeCell::default();
        assert_eq!(cell.get(), None);
        cell.set(3.0);
        cell.set(-1.5);
        assert_eq!(cell.get(), Some(-1.5));
    }
}
