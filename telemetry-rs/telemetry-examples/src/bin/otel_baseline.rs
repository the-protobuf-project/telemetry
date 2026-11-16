//! Writes the load `loadgen` writes — n log records, spans or counter
//! increments — through the upstream OpenTelemetry Rust SDK alone, with its
//! batch processors and OTLP/gRPC exporters, and prints one JSON line with how
//! long it took. `bench/bench_all.py` runs it beside `loadgen` to compare this
//! SDK with plain OpenTelemetry. The batch processors read the standard
//! `OTEL_BSP_*` and `OTEL_BLRP_*` variables, which is how the bench tunes them.
//!
//! ```text
//! cargo run --release -p telemetry-examples --bin otel_baseline -- <signal> <n> <host:port>
//! ```
use std::time::{Instant, SystemTime};

use opentelemetry::KeyValue;
use opentelemetry::logs::{AnyValue, LogRecord as _, Logger as _, LoggerProvider as _, Severity};
use opentelemetry::metrics::MeterProvider as _;
use opentelemetry::trace::{Span as _, Tracer as _, TracerProvider as _};
use opentelemetry_otlp::WithExportConfig;
use opentelemetry_sdk::logs::SdkLoggerProvider;
use opentelemetry_sdk::metrics::{PeriodicReader, SdkMeterProvider};
use opentelemetry_sdk::trace::SdkTracerProvider;

type Emit = Box<dyn Fn(usize) + Sync>;
type Close = Box<dyn FnOnce() -> Option<String>>;

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let args: Vec<String> = std::env::args().collect();
    let signal = args.get(1).map(String::as_str).unwrap_or("logs");
    let n: usize = args
        .get(2)
        .and_then(|s| s.parse().ok())
        .unwrap_or(1_000_000);
    let endpoint = format!(
        "http://{}",
        args.get(3).map(String::as_str).unwrap_or("127.0.0.1:4317")
    );

    let (emit, close): (Emit, Close) = match signal {
        "logs" => {
            let exporter = opentelemetry_otlp::LogExporter::builder()
                .with_tonic()
                .with_endpoint(&endpoint)
                .build()?;
            let provider = SdkLoggerProvider::builder()
                .with_batch_exporter(exporter)
                .build();
            let logger = provider.logger("otel-baseline");
            (
                Box::new(move |i| {
                    let mut record = logger.create_log_record();
                    record.set_timestamp(SystemTime::now());
                    record.set_severity_number(Severity::Info);
                    record.set_body(AnyValue::from("load"));
                    record.add_attribute("i", i as i64);
                    logger.emit(record);
                }),
                Box::new(move || provider.shutdown().err().map(|e| e.to_string())),
            )
        }
        "spans" => {
            let exporter = opentelemetry_otlp::SpanExporter::builder()
                .with_tonic()
                .with_endpoint(&endpoint)
                .build()?;
            let provider = SdkTracerProvider::builder()
                .with_batch_exporter(exporter)
                .build();
            let tracer = provider.tracer("otel-baseline");
            (
                Box::new(move |_| {
                    let mut span = tracer.start("load.op");
                    span.end();
                }),
                Box::new(move || provider.shutdown().err().map(|e| e.to_string())),
            )
        }
        "metrics" => {
            let exporter = opentelemetry_otlp::MetricExporter::builder()
                .with_tonic()
                .with_endpoint(&endpoint)
                .build()?;
            let provider = SdkMeterProvider::builder()
                .with_reader(PeriodicReader::builder(exporter).build())
                .build();
            let counter = provider.meter("otel-baseline").f64_counter("ops").build();
            (
                Box::new(move |_| counter.add(1.0, &[] as &[KeyValue])),
                Box::new(move || provider.shutdown().err().map(|e| e.to_string())),
            )
        }
        other => anyhow::bail!("unknown signal {other}"),
    };

    // OTEL_BASELINE_WORKERS threads write concurrently (default 1).
    let workers: usize = std::env::var("OTEL_BASELINE_WORKERS")
        .ok()
        .and_then(|v| v.parse().ok())
        .unwrap_or(1)
        .max(1);
    let start = Instant::now();
    let per = n / workers;
    std::thread::scope(|scope| {
        for w in 0..workers {
            let count = if w == workers - 1 {
                n - per * (workers - 1)
            } else {
                per
            };
            let emit = &emit;
            scope.spawn(move || (0..count).for_each(emit));
        }
    });
    let emitted = start.elapsed();

    let start = Instant::now();
    let close_error = close();
    let closed = start.elapsed();

    println!(
        "{}",
        serde_json::json!({
            "lang": "rust", "signal": signal, "n": n, "workers": workers,
            "emit_s": emitted.as_secs_f64(), "close_s": closed.as_secs_f64(),
            "close_error": close_error,
        })
    );
    Ok(())
}
