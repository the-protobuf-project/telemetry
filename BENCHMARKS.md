# Benchmarks

Every SDK writes **1,000,000** log records, spans and counter increments over OTLP/gRPC to one
in-process counting collector, so the table shows what each SDK **delivered**, not only how
fast the calls returned. Regenerate with `bench/bench_all.py`.

Measured 2026-11-16 on Darwin arm64, 14 CPUs,
Python 3.15.2. Console log output is discarded (Go) or sent to /dev/null
(Rust, Python), so the terminal is not what is measured.

| SDK | Mode | Signal | Delivered | Lost | Emit rate | Per call | Flush on close |
| --- | ---- | ------ | --------: | ---: | --------: | -------: | -------------: |
| Go | default (drop when full), 1 goroutine | logs | 570,456 | 43.0% | 561,785/s | 1,780 ns | 0.01 s |
| Go | default (drop when full), 1 goroutine | spans | 528,926 | 47.1% | 1,807,997/s | 553 ns | 0.00 s |
| Go | default (drop when full), 1 goroutine | metrics | 1,000,000 | 0.0% | 8,094,449/s | 124 ns | 0.00 s |
| Go | Lossless, 1 goroutine | logs | 1,000,000 | 0.0% | 323,687/s | 3,089 ns | 0.01 s |
| Go | Lossless, 1 goroutine | spans | 1,000,000 | 0.0% | 1,097,604/s | 911 ns | 0.00 s |
| Go | Lossless, 1 goroutine | metrics | 1,000,000 | 0.0% | 8,360,732/s | 120 ns | 0.00 s |
| Go | Lossless, 14 goroutines, batch 8192 | logs | 1,000,000 | 0.0% | 385,545/s | 2,594 ns | 0.21 s |
| Go | Lossless, 14 goroutines, batch 8192 | spans | 1,000,000 | 0.0% | 1,467,835/s | 681 ns | 0.05 s |
| Go | Lossless, 14 goroutines, batch 8192 | metrics | 1,000,000 | 0.0% | 12,810,050/s | 78 ns | 0.00 s |
| Rust | default (SDK batch processor) | logs | 684,953 | 31.5% | 550,741/s | 1,816 ns | 0.01 s |
| Rust | default (SDK batch processor) | spans | 161,232 | 83.9% | 7,484,374/s | 134 ns | 0.00 s |
| Rust | default (SDK batch processor) | metrics | 1,000,000 | 0.0% | 26,414,932/s | 38 ns | 0.00 s |
| Python | default (SDK batch processor) | logs | 1,000,000 | 0.0% | 7,441/s | 134,384 ns | 0.00 s |
| Python | default (SDK batch processor) | spans | 534,085 | 46.6% | 64,573/s | 15,486 ns | 0.01 s |
| Python | default (SDK batch processor) | metrics | 1,000,000 | 0.0% | 430,706/s | 2,322 ns | 0.00 s |

**Lost** is what never reached the collector. Metrics are aggregated in-process (a counter
exports its running total), so they cannot lose increments; logs and spans are exported one by
one through a bounded queue, and a queue that fills either drops (default) or makes the
program wait (`ExportOptions.Lossless`, Go). Go reports every drop in `Telemetry.Stats()` and
from `Close()`; the Rust and Python SDKs still drop silently when their queue (2048 by
default; `OTEL_BSP_MAX_QUEUE_SIZE` / `OTEL_BLRP_MAX_QUEUE_SIZE`) overflows.

## Go: cost per call

`go test -bench . ./telemetry-go` — `export=false` is the SDK's own overhead with nothing
exported; `export=true` exports to an in-process collector. `-8` runs 8 goroutines in parallel.

| Benchmark | ns/op | B/op | allocs/op |
| --------- | ----: | ---: | --------: |
| `BenchmarkLogInfo/export=false` | 107.1 | 336 | 2 |
| `BenchmarkLogInfo/export=false-8` | 83.23 | 336 | 2 |
| `BenchmarkLogInfo/export=true` | 1649 | 1653 | 7 |
| `BenchmarkLogInfo/export=true-8` | 718.0 | 2225 | 16 |
| `BenchmarkSpan/export=false` | 87.16 | 160 | 5 |
| `BenchmarkSpan/export=false-8` | 20.49 | 160 | 5 |
| `BenchmarkSpan/export=true` | 500.1 | 1074 | 7 |
| `BenchmarkSpan/export=true-8` | 165.4 | 1069 | 7 |
| `BenchmarkCounter` | 114.3 | 44 | 2 |
| `BenchmarkCounter-8` | 78.32 | 42 | 2 |
