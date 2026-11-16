//! Writes N log records, spans or counter increments through the Rust SDK to
//! an OTLP collector and prints one JSON line with how long it took. Driven
//! by `bench/bench_all.py`, which counts what reached the collector.
//!
//! ```text
//! cargo run --release -p telemetry-examples --bin loadgen -- <signal> <n> <host:port>
//! ```
use std::time::Instant;

use telemetry::logger;

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let args: Vec<String> = std::env::args().collect();
    let signal = args.get(1).map(String::as_str).unwrap_or("logs");
    let n: usize = args.get(2).and_then(|s| s.parse().ok()).unwrap_or(1_000_000);
    let (host, port) = args
        .get(3)
        .and_then(|ep| ep.rsplit_once(':'))
        .map(|(h, p)| (h.to_string(), p.parse().unwrap_or(4317)))
        .unwrap_or(("127.0.0.1".into(), 4317));

    // LOADGEN_MCAP=path records to MCAP; an endpoint of "none" skips OTLP,
    // to measure MCAP recording alone.
    let mut builder = telemetry::Telemetry::builder("loadgen-rs", "1.0.0").with_tracing();
    if args.get(3).map(String::as_str) != Some("none") {
        builder = builder.with_otlp(host, port);
    }
    if let Ok(path) = std::env::var("LOADGEN_MCAP") {
        builder = builder.with_mcap(path);
    }
    let mut telemetry = builder.build()?;

    let start = Instant::now();
    match signal {
        "logs" => {
            for i in 0..n {
                logger::info!("load {i}");
            }
        }
        "spans" => {
            let tracing = telemetry.tracing.as_ref().expect("tracing enabled");
            if !tracing.is_enabled() {
                anyhow::bail!("spans: the Rust SDK has no tracer without OTLP export");
            }
            for _ in 0..n {
                let span = tracing.start_span("load.op");
                span.end();
            }
        }
        "metrics" => {
            for _ in 0..n {
                telemetry.metrics.counter("ops", 1.0)?;
            }
        }
        // Distributed logging: records logged while a span is attached carry
        // its trace id. Prints the trace id for the caller to look for.
        "correlated" => {
            let tracing = telemetry.tracing.as_ref().expect("tracing enabled");
            let span = tracing.start_span_with("load.correlated", telemetry::tracing::SpanKind::Internal, None);
            {
                let _current = span.attach();
                for i in 0..n {
                    logger::info!("correlated {i}");
                }
            }
            eprintln!("trace_id={}", span.trace_id().unwrap_or_default());
            span.end();
        }
        other => anyhow::bail!("unknown signal {other}"),
    }
    let emit = start.elapsed();

    let start = Instant::now();
    let close_err = telemetry.close().err().map(|e| e.to_string());
    let close = start.elapsed();

    println!(
        "{}",
        serde_json::json!({
            "lang": "rust", "signal": signal, "n": n, "workers": 1,
            "emit_s": emit.as_secs_f64(), "close_s": close.as_secs_f64(),
            "close_error": close_err,
        })
    );
    Ok(())
}
