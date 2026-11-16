# Benchmarks

Every SDK writes **1,000,000** log records, spans and counter increments over OTLP/gRPC to one
in-process counting collector, so the table shows what each SDK **delivered**, not only how
fast the calls returned. Regenerate with `bench/bench_all.py`.

Measured 2026-11-16 on Darwin arm64, 14 CPUs,
Python 3.15.2. Console log output is discarded (Go) or sent to /dev/null
(Rust, Python), so the terminal is not what is measured.

| SDK | Mode | Signal | Delivered | Lost | Emit rate | Per call | Flush on close |
| --- | ---- | ------ | --------: | ---: | --------: | -------: | -------------: |
| Go | default (drop when full), 1 goroutine | logs | 560,067 | 44.0% | 563,810/s | 1,774 ns | 0.01 s |
| Go | default (drop when full), 1 goroutine | spans | 538,112 | 46.2% | 1,826,562/s | 547 ns | 0.00 s |
| Go | default (drop when full), 1 goroutine | metrics | 1,000,000 | 0.0% | 8,325,398/s | 120 ns | 0.00 s |
| Go | Lossless, 1 goroutine | logs | 1,000,000 | 0.0% | 312,270/s | 3,202 ns | 0.01 s |
| Go | Lossless, 1 goroutine | spans | 1,000,000 | 0.0% | 1,060,558/s | 943 ns | 0.00 s |
| Go | Lossless, 1 goroutine | metrics | 1,000,000 | 0.0% | 7,649,493/s | 131 ns | 0.00 s |
| Go | Lossless, 14 goroutines, batch 8192 | logs | 1,000,000 | 0.0% | 378,927/s | 2,639 ns | 0.21 s |
| Go | Lossless, 14 goroutines, batch 8192 | spans | 1,000,000 | 0.0% | 1,460,729/s | 685 ns | 0.05 s |
| Go | Lossless, 14 goroutines, batch 8192 | metrics | 1,000,000 | 0.0% | 10,814,304/s | 92 ns | 0.00 s |
| Go | Lossless, 14 goroutines, batch 8192, 4 exporters | logs | 1,000,000 | 0.0% | 1,418,411/s | 705 ns | 0.25 s |
| Go | Lossless, 14 goroutines, batch 8192, 4 exporters | spans | 1,000,000 | 0.0% | 3,582,276/s | 279 ns | 0.06 s |
| Go | Lossless, 14 goroutines, batch 8192, 4 exporters | metrics | 1,000,000 | 0.0% | 11,792,754/s | 85 ns | 0.00 s |
| Rust | default (SDK batch processor) | logs | 712,155 | 28.8% | 492,847/s | 2,029 ns | 0.01 s |
| Rust | default (SDK batch processor) | spans | 129,063 | 87.1% | 7,897,896/s | 127 ns | 0.00 s |
| Rust | default (SDK batch processor) | metrics | 1,000,000 | 0.0% | 25,273,507/s | 40 ns | 0.00 s |
| Python | default (SDK batch processor) | logs | 1,000,000 | 0.0% | 6,695/s | 149,355 ns | 0.00 s |
| Python | default (SDK batch processor) | spans | 532,480 | 46.8% | 61,335/s | 16,304 ns | 0.02 s |
| Python | default (SDK batch processor) | metrics | 1,000,000 | 0.0% | 419,385/s | 2,384 ns | 0.00 s |

**Lost** is what never reached the collector. Metrics are aggregated in-process (a counter
exports its running total), so they cannot lose increments; logs and spans are exported one by
one through a bounded queue, and a queue that fills either drops (default) or makes the
program wait (`ExportOptions.Lossless`, Go). Go reports every drop in `Telemetry.Stats()` and
from `Close()`; the Rust and Python SDKs still drop silently when their queue (2048 by
default; `OTEL_BSP_MAX_QUEUE_SIZE` / `OTEL_BLRP_MAX_QUEUE_SIZE`) overflows.

## MCAP recording

The same 1,000,000 records per signal, recorded to an MCAP file for Foxglove with OTLP off,
then read back with the `mcap` library: **Recorded** is what the file holds.

| SDK | Signal | Recorded | Write rate | Per call | File size | Bytes / record |
| --- | ------ | -------: | ---------: | -------: | --------: | -------------: |
| Go | logs | 1,000,000 | 346,644/s | 2,885 ns | 27.3 MiB | 29 |
| Go | spans | 1,000,000 | 820,765/s | 1,218 ns | 56.1 MiB | 59 |
| Go | metrics | 1,000,000 | 1,720,415/s | 581 ns | 18.8 MiB | 20 |
| Rust | logs | 1,000,000 | 293,202/s | 3,411 ns | 27.6 MiB | 29 |
| Rust | spans | 0 | 8,890,137/s | 112 ns | 0.0 MiB | 0 |
| Rust | metrics | 1,000,000 | 1,602,995/s | 624 ns | 18.6 MiB | 19 |
| Python | logs | 1,000,000 | 7,426/s | 134,668 ns | 29.5 MiB | 31 |
| Python | spans | 1,000,000 | 53,454/s | 18,708 ns | 56.1 MiB | 59 |
| Python | metrics | 1,000,000 | 166,256/s | 6,015 ns | 23.3 MiB | 24 |

A row with 0 recorded is a missing feature, not a loss: the Rust SDK records logs and
metrics to MCAP but has no MCAP path for spans yet.

## Go: cost per call

`go test -bench . ./telemetry-go` — `export=false` is the SDK's own overhead with nothing
exported; `export=true` exports to an in-process collector. `-8` runs 8 goroutines in parallel.

| Benchmark | ns/op | B/op | allocs/op |
| --------- | ----: | ---: | --------: |
| `BenchmarkLogInfo/export=false` | 110.9 | 336 | 2 |
| `BenchmarkLogInfo/export=false-8` | 77.58 | 336 | 2 |
| `BenchmarkLogInfo/export=true` | 1600 | 1650 | 7 |
| `BenchmarkLogInfo/export=true-8` | 427.9 | 1862 | 10 |
| `BenchmarkSpan/export=false` | 105.9 | 240 | 5 |
| `BenchmarkSpan/export=false-8` | 30.83 | 240 | 5 |
| `BenchmarkSpan/export=true` | 530.1 | 1155 | 7 |
| `BenchmarkSpan/export=true-8` | 171.4 | 1148 | 7 |
| `BenchmarkCounter` | 113.7 | 43 | 2 |
| `BenchmarkCounter-8` | 80.01 | 42 | 2 |
