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
    let n: usize = args
        .get(2)
        .and_then(|s| s.parse().ok())
        .unwrap_or(1_000_000);
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
    // LOADGEN_CONSOLE=1 also prints every log line, as a service watched in a
    // terminal would; off by default, like telemetry-loadgen, to measure the
    // pipeline.
    builder = builder.with_console(std::env::var("LOADGEN_CONSOLE").is_ok_and(|v| v == "1"));
    if let Ok(path) = std::env::var("LOADGEN_MCAP") {
        builder = builder.with_mcap(path);
    }
    // LOADGEN_LOSSLESS=1 waits for room instead of dropping; LOADGEN_QUEUE
    // sets the export queue size and LOADGEN_EXPORTERS the exporters per signal.
    let lossless = std::env::var("LOADGEN_LOSSLESS").is_ok_and(|v| v == "1");
    let queue_size = std::env::var("LOADGEN_QUEUE")
        .ok()
        .and_then(|v| v.parse().ok())
        .unwrap_or(if lossless { 65_536 } else { 2048 });
    builder = builder.with_export(telemetry::options::ExportOptions {
        lossless,
        queue_size,
        batch_size: if lossless { 8192 } else { 512 },
        exporters: std::env::var("LOADGEN_EXPORTERS")
            .ok()
            .and_then(|v| v.parse().ok())
            .unwrap_or(1),
    });
    let mut telemetry = builder.build()?;

    // LOADGEN_WORKERS threads write concurrently (default 1).
    let workers: usize = std::env::var("LOADGEN_WORKERS")
        .ok()
        .and_then(|v| v.parse().ok())
        .unwrap_or(1)
        .max(1);

    let start = Instant::now();
    match signal {
        "logs" => fan_out(n, workers, |i| {
            logger::info!("load {i}");
        }),
        "spans" => {
            let tracing = telemetry.tracing.as_ref().expect("tracing enabled");
            if !tracing.is_enabled() {
                anyhow::bail!("spans: the Rust SDK has no tracer without OTLP export");
            }
            fan_out(n, workers, |_| {
                let span = tracing.start_span("load.op");
                span.end();
            });
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
            let span = tracing.start_span_with(
                "load.correlated",
                telemetry::tracing::SpanKind::Internal,
                None,
            );
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
    let stats = telemetry.stats();
    let dropped = match signal {
        "logs" | "correlated" => stats.logs.dropped(),
        "spans" => stats.spans.dropped(),
        _ => 0,
    };

    println!(
        "{}",
        serde_json::json!({
            "lang": "rust", "signal": signal, "n": n, "workers": workers,
            "emit_s": emit.as_secs_f64(), "close_s": close.as_secs_f64(),
            "dropped_reported": dropped, "close_error": close_err,
        })
    );
    Ok(())
}

/// Calls `f` n times across `workers` threads.
fn fan_out(n: usize, workers: usize, f: impl Fn(usize) + Sync) {
    let per = n / workers;
    std::thread::scope(|scope| {
        for w in 0..workers {
            let count = if w == workers - 1 {
                n - per * (workers - 1)
            } else {
                per
            };
            let f = &f;
            scope.spawn(move || (0..count).for_each(f));
        }
    });
}
