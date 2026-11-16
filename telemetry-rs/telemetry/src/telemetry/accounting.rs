//! Accounting for every log record and span the program hands to OTLP export.
//!
//! The SDK's batch processors drop what does not fit in their queue and say
//! so only through internal logging, so a burst of a million spans could lose
//! most of them without a word. These wrappers count what was emitted,
//! exported and failed per signal; what was emitted and never left the
//! process by shutdown was dropped, and [`crate::Telemetry::close`] says so.
//! With [`ExportOptions::lossless`], a writer waits while the queue is full
//! instead of the record being dropped.

use std::fmt::Debug;
use std::future::Future;
use std::sync::Arc;
use std::sync::atomic::{AtomicBool, AtomicU64, AtomicUsize, Ordering};
use std::time::Duration;

use opentelemetry::Context;
use opentelemetry::InstrumentationScope;
use opentelemetry::logs::Severity;
use opentelemetry_sdk::Resource;
use opentelemetry_sdk::error::OTelSdkResult;
use opentelemetry_sdk::logs::{LogBatch, LogExporter, LogProcessor, SdkLogRecord};
use opentelemetry_sdk::trace::{Span, SpanData, SpanExporter, SpanProcessor};

use crate::options::ExportOptions;

/// What happened to one signal's records.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub struct SignalStats {
    /// Handed to the pipeline by the program.
    pub emitted: u64,
    /// Accepted by the collector.
    pub exported: u64,
    /// In an export request that failed (collector down, rejected, timed out).
    pub failed: u64,
    /// Not yet exported: still queued while running, dropped once closed.
    pub pending: u64,
}

impl SignalStats {
    /// Records that never left the process: meaningful after close, when
    /// nothing is still in flight.
    pub fn dropped(&self) -> u64 {
        self.pending
    }
}

/// Counters for one signal, shared by its processor and exporter wrappers.
#[derive(Debug, Default)]
pub struct Counters {
    emitted: AtomicU64,
    exported: AtomicU64,
    failed: AtomicU64,
}

impl Counters {
    fn pending(&self) -> u64 {
        let done = self.exported.load(Ordering::Acquire) + self.failed.load(Ordering::Acquire);
        self.emitted.load(Ordering::Acquire).saturating_sub(done)
    }

    /// A snapshot of the counts.
    pub fn stats(&self) -> SignalStats {
        let exported = self.exported.load(Ordering::Acquire);
        let failed = self.failed.load(Ordering::Acquire);
        let emitted = self.emitted.load(Ordering::Acquire);
        SignalStats {
            emitted,
            exported,
            failed,
            pending: emitted.saturating_sub(exported + failed),
        }
    }

    fn record(&self, n: u64, result: &OTelSdkResult) {
        match result {
            Ok(()) => self.exported.fetch_add(n, Ordering::AcqRel),
            Err(_) => self.failed.fetch_add(n, Ordering::AcqRel),
        };
    }
}

/// Holds a writer back while the queue behind it is full (lossless mode),
/// rather than letting the batch processor drop the record.
#[derive(Debug)]
struct Gate {
    lossless: bool,
    // Records in the pipeline (queued or exporting) a writer may add to; kept
    // below the queue size by enough headroom for writers racing past the
    // check at once.
    capacity: u64,
    closed: AtomicBool,
}

impl Gate {
    // Sized by one exporter's queue even when there are several: records are
    // dealt round robin, but one exporter may drain slower than the rest, and
    // its queue must never overflow while the others have room.
    fn new(opts: &ExportOptions) -> Self {
        let queue = opts.queue_size as u64;
        Self {
            lossless: opts.lossless,
            capacity: queue.saturating_sub((queue / 4).min(1024)).max(1),
            closed: AtomicBool::new(false),
        }
    }

    fn wait(&self, counters: &Counters) {
        if !self.lossless {
            return;
        }
        let mut backoff = Duration::from_micros(1);
        while counters.pending() >= self.capacity && !self.closed.load(Ordering::Acquire) {
            std::thread::sleep(backoff);
            backoff = (backoff * 2).min(Duration::from_millis(1));
        }
    }
}

/// A span exporter that counts what it exports and what fails.
#[derive(Debug)]
pub struct CountingSpanExporter<E> {
    inner: E,
    counters: Arc<Counters>,
}

impl<E> CountingSpanExporter<E> {
    pub fn new(inner: E, counters: Arc<Counters>) -> Self {
        Self { inner, counters }
    }
}

impl<E: SpanExporter> SpanExporter for CountingSpanExporter<E> {
    fn export(&self, batch: Vec<SpanData>) -> impl Future<Output = OTelSdkResult> + Send {
        let n = batch.len() as u64;
        async move {
            let result = self.inner.export(batch).await;
            self.counters.record(n, &result);
            result
        }
    }

    fn shutdown_with_timeout(&self, timeout: Duration) -> OTelSdkResult {
        self.inner.shutdown_with_timeout(timeout)
    }

    fn set_resource(&mut self, resource: &Resource) {
        self.inner.set_resource(resource);
    }
}

/// A log exporter that counts what it exports and what fails.
#[derive(Debug)]
pub struct CountingLogExporter<E> {
    inner: E,
    counters: Arc<Counters>,
}

impl<E> CountingLogExporter<E> {
    pub fn new(inner: E, counters: Arc<Counters>) -> Self {
        Self { inner, counters }
    }
}

impl<E: LogExporter> LogExporter for CountingLogExporter<E> {
    fn export(&self, batch: LogBatch<'_>) -> impl Future<Output = OTelSdkResult> + Send {
        let n = batch.iter().count() as u64;
        async move {
            let result = self.inner.export(batch).await;
            self.counters.record(n, &result);
            result
        }
    }

    fn shutdown_with_timeout(&self, timeout: Duration) -> OTelSdkResult {
        self.inner.shutdown_with_timeout(timeout)
    }

    fn event_enabled(&self, level: Severity, target: &str, name: Option<&str>) -> bool {
        self.inner.event_enabled(level, target, name)
    }

    fn set_resource(&mut self, resource: &Resource) {
        self.inner.set_resource(resource);
    }
}

/// The span processor in front of the SDK's batch processor: counts what is
/// emitted and, when lossless, waits for room.
#[derive(Debug)]
pub struct AccountedSpanProcessor<P> {
    inner: P,
    counters: Arc<Counters>,
    gate: Gate,
}

impl<P> AccountedSpanProcessor<P> {
    pub fn new(inner: P, counters: Arc<Counters>, opts: &ExportOptions) -> Self {
        Self {
            inner,
            counters,
            gate: Gate::new(opts),
        }
    }
}

impl<P: SpanProcessor> SpanProcessor for AccountedSpanProcessor<P> {
    fn on_start(&self, span: &mut Span, cx: &Context) {
        self.inner.on_start(span, cx);
    }

    fn on_end(&self, span: SpanData) {
        self.gate.wait(&self.counters);
        self.counters.emitted.fetch_add(1, Ordering::AcqRel);
        self.inner.on_end(span);
    }

    fn force_flush(&self) -> OTelSdkResult {
        self.inner.force_flush()
    }

    fn shutdown_with_timeout(&self, timeout: Duration) -> OTelSdkResult {
        self.gate.closed.store(true, Ordering::Release);
        self.inner.shutdown_with_timeout(timeout)
    }

    fn set_resource(&mut self, resource: &Resource) {
        self.inner.set_resource(resource);
    }
}

/// The log processor in front of the SDK's batch processor: counts what is
/// emitted and, when lossless, waits for room.
#[derive(Debug)]
pub struct AccountedLogProcessor<P> {
    inner: P,
    counters: Arc<Counters>,
    gate: Gate,
}

impl<P> AccountedLogProcessor<P> {
    pub fn new(inner: P, counters: Arc<Counters>, opts: &ExportOptions) -> Self {
        Self {
            inner,
            counters,
            gate: Gate::new(opts),
        }
    }
}

impl<P: LogProcessor> LogProcessor for AccountedLogProcessor<P> {
    fn emit(&self, data: &mut SdkLogRecord, instrumentation: &InstrumentationScope) {
        self.gate.wait(&self.counters);
        self.counters.emitted.fetch_add(1, Ordering::AcqRel);
        self.inner.emit(data, instrumentation);
    }

    fn force_flush(&self) -> OTelSdkResult {
        self.inner.force_flush()
    }

    fn shutdown_with_timeout(&self, timeout: Duration) -> OTelSdkResult {
        self.gate.closed.store(true, Ordering::Release);
        self.inner.shutdown_with_timeout(timeout)
    }

    fn event_enabled(&self, level: Severity, target: &str, name: Option<&str>) -> bool {
        self.inner.event_enabled(level, target, name)
    }

    fn set_resource(&mut self, resource: &Resource) {
        self.inner.set_resource(resource);
    }
}

/// Spreads records over several processors, each with its own exporter,
/// round robin: one exporter sends one batch at a time, so n of them send n
/// batches at once.
#[derive(Debug)]
pub struct Shards<P> {
    shards: Vec<P>,
    next: AtomicUsize,
}

impl<P> Shards<P> {
    pub fn new(shards: Vec<P>) -> Self {
        assert!(!shards.is_empty(), "at least one shard");
        Self {
            shards,
            next: AtomicUsize::new(0),
        }
    }

    fn pick(&self) -> &P {
        &self.shards[self.next.fetch_add(1, Ordering::Relaxed) % self.shards.len()]
    }

    fn each(&self, f: impl Fn(&P) -> OTelSdkResult) -> OTelSdkResult {
        self.shards.iter().map(f).fold(Ok(()), |acc, r| acc.and(r))
    }
}

impl<P: SpanProcessor> SpanProcessor for Shards<P> {
    fn on_start(&self, _span: &mut Span, _cx: &Context) {}

    fn on_end(&self, span: SpanData) {
        self.pick().on_end(span);
    }

    fn force_flush(&self) -> OTelSdkResult {
        self.each(|p| p.force_flush())
    }

    fn shutdown_with_timeout(&self, timeout: Duration) -> OTelSdkResult {
        self.each(|p| p.shutdown_with_timeout(timeout))
    }

    fn set_resource(&mut self, resource: &Resource) {
        for p in &mut self.shards {
            p.set_resource(resource);
        }
    }
}

impl<P: LogProcessor> LogProcessor for Shards<P> {
    fn emit(&self, data: &mut SdkLogRecord, instrumentation: &InstrumentationScope) {
        self.pick().emit(data, instrumentation);
    }

    fn force_flush(&self) -> OTelSdkResult {
        self.each(|p| p.force_flush())
    }

    fn shutdown_with_timeout(&self, timeout: Duration) -> OTelSdkResult {
        self.each(|p| p.shutdown_with_timeout(timeout))
    }

    fn event_enabled(&self, level: Severity, target: &str, name: Option<&str>) -> bool {
        self.shards[0].event_enabled(level, target, name)
    }

    fn set_resource(&mut self, resource: &Resource) {
        for p in &mut self.shards {
            p.set_resource(resource);
        }
    }
}

/// Describes the records that never reached the collector, or `None`.
pub fn loss_report(logs: SignalStats, spans: SignalStats) -> Option<String> {
    let mut parts = Vec::new();
    for (name, s) in [("log records", logs), ("spans", spans)] {
        if s.dropped() > 0 {
            parts.push(format!(
                "{} of {} {name} dropped (export queue full)",
                s.dropped(),
                s.emitted
            ));
        }
        if s.failed > 0 {
            parts.push(format!(
                "{} of {} {name} failed to export",
                s.failed, s.emitted
            ));
        }
    }
    if parts.is_empty() {
        return None;
    }
    let mut report = format!("telemetry: {}", parts.join("; "));
    if logs.dropped() + spans.dropped() > 0 {
        report.push_str("; use with_export(ExportOptions { lossless: true, .. }) or a larger queue_size to keep them");
    }
    Some(report)
}
