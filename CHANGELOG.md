# Changelog

All notable changes to Telemetry will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## 1.3.1 - unreleased

### Fixed

- C++: metrics were exported without the service's resource, so every one
  reached the backend as `unknown_service`.
- Python: `Telemetry.close()` left metrics to be exported at interpreter
  exit; it now exports them before returning.

### Added

- Go: `tel.Metrics.Counter(name, attrs...)`, `Gauge` and `Histogram` bind a
  metric series once, so recording does no reflection, lookup or allocation;
  the recommended API for hot paths.
- Rust: `Metrics::counter_handle`, `gauge_handle` and `histogram_handle` look
  up an instrument once, so hot paths record to it directly.

### Changed

- Counters and gauges record into the SDK, in per-thread or per-CPU slots,
  and OpenTelemetry reads them when it collects, rather than every value
  going through OpenTelemetry's synchronous instruments. Every increment is
  still exported, at shutdown included. Against the official SDKs on one
  million counter increments: Go 7.3x on one goroutine and 44x on 14 with
  handles (2.5x and 21x with struct tags), Rust 3.6x on one thread and far
  more on 14, Python 9.7x and 19x on free-threaded 3.14, C++ 2.6x and 20x.
  Histograms still record through OpenTelemetry.
- Go and Rust: counters ignore negative and NaN values; a counter only goes
  up.
- Go: the struct-tag `Record` path parses each struct type's tags once.
- C++: gauges are exported as OpenTelemetry gauges holding the last value
  set, rather than as up-down counters; `Metrics::counter`, `histogram`,
  `gauge` and the `get_*` lookups take a `std::string_view`, which changes
  the library's ABI.
- Python: if this `Telemetry` created the global meter provider, `close()`
  shuts it down.
- C++: the Bazel module is `tpp_telemetry_cpp` (was `telemetry.cpp`, which
  the Bazel Central Registry found too generic); its test and example
  dependencies are dev dependencies, and the unused grpc, fmt and freertos
  dependencies are gone, so a module that depends on it resolves less.

## [1.3.0] - 2026-11-16

### Fixed

- Go: logs and spans written faster than the collector accepted them were
  dropped silently once the SDK's 2048-record queue filled (63% of 1M logs and
  3% of 1M spans arrived). Every record is now counted; drops are warned about
  while they happen and returned by `Close`, and `ExportOptions.Lossless` makes
  producers wait instead of dropping.
- Go: export errors were discarded by a no-op error handler; they are reported,
  rate-limited.
- Go: struct-tag histograms lost their labels; struct-tag gauges summed every
  value recorded instead of reporting the last.
- Go: span attributes of types such as `int32` were recorded as the string
  `"<int32 Value>"`.
- Go: `Warnf` and the other formatted log methods reported the wrong caller.
- Go: config files loaded one after another leaked keys into each other; a
  broken auto-discovered config file was ignored.
- C++: spans were written to MCAP only and never exported over OTLP, and child
  spans did not record their parent.
- Rust: `with_profiling` was stored and never used.
- Rust, Python: logs and spans beyond the SDK's export queue were dropped
  silently (87% of 1M Rust spans, 47% of Python's). Every record is now counted
  (`stats()`), the loss is reported on close, and `with_export` offers a
  lossless mode, as in Go.
- Python: a log call took 134 µs, 70% of it `inspect.stack()` reading source
  files to find the caller; it now takes 37 µs.
- C++: a span started without an explicit parent always began a new trace, even
  inside another span.
- Python: `close()` did not flush pending spans.
- Rust: in lossless mode with several exporters, one exporter draining slower
  than the rest could overflow its queue and drop records; each exporter is
  now gated on its own queue.
- Python: `with_otlp_http()` was ignored; every signal exported over gRPC.
- Python: an export that took longer than 10 seconds, retries included, lost
  its whole batch; a large batch under load could. The deadline is
  `with_export(timeout=...)`, 30 seconds by default as in Go.
- Rust: `Metrics::record` leaked a copy of every metric name on every call;
  names are now kept once per instrument.
- C++: every counter, histogram or gauge value created its OpenTelemetry
  instrument anew; each is now made once. A gauge was exported as an
  up-down counter given the full value each time, so setting it to 5 twice
  read 10; it is now given each change, and reads the last value set.
- Rust: log attributes were all sent as strings (an integer became `"42"`);
  they keep their type.
- Single image: Loki's and Tempo's default ingest limits refused almost all of
  a high-volume load; Grafana's trace ↔ log links matched text that OTLP logs
  do not contain.

### Added

- Go: logging rebuilt on `log/slog`: `WithLogHandler` / `WithLogger` bring your
  own logger, `Telemetry.Slog()` and `InfoContext` & co. correlate with traces.
- Go: `WithExport(ExportOptions{...})` (queue, batch, lossless, sharded
  exporters), `Telemetry.Stats()`, `Tracing.SetBaggage` / `Baggage`; spans are
  recorded to MCAP.
- Rust, Python, C++: W3C trace-context and baggage propagation
  (`inject` / `extract`), so one trace spans services in any of the four
  languages.
- Rust: `Span::attach` for logs correlated with a span; Pyroscope profiling;
  spans recorded to MCAP.
- C++: distributed logging — logs written while a span is attached (or inside
  `TELEMETRY_SPAN`) carry its trace and span id.
- Python: profiling (`[profiling]` extra).
- Single image: Pyroscope, a collector sized from the container's limits with a
  `memory_limiter`, and its own metrics scraped by Prometheus.
- `bench/`: `bench_all.py` (every SDK, OTLP and MCAP), `stack_bench.py` (the
  single image under load), `just crosslang`, and `telemetry-loadgen` with
  built-in pprof profiling; `docs/benchmark.md`.
- Rust: `ExportOptions::exporters` sends each signal through several
  exporters at once, as Go's `Exporters` does.
- C++: `with_export(ExportOptions{...})` (queue, batch, lossless, several
  exporters), `Telemetry::stats()`, and a report on `close()` of any log
  records or spans that never reached the collector.
- Rust, Python, C++: `with_console(false)` turns off printing every log line,
  for services read in Loki rather than a terminal; records still go to OTLP
  and MCAP.
- Python: free-threaded Python 3.14 (`3.14t`) is supported, and is what
  development uses. Logs and spans sent over OTLP/HTTP are encoded straight to
  protobuf bytes rather than through the exporters' message objects, which
  protobuf builds in pure Python on 3.14t; `with_export(exporters=n)` spreads
  a signal over several exporters.
- Python: on free-threaded builds, the logger's records go through
  per-thread buffers encoded straight to OTLP bytes, rather than the SDK's
  shared queue, which every writer thread waited on.
- Python 3.15 and 3.15t are supported and tested, end to end: logs and spans
  from several threads to a collector, every record once, logs carrying the
  span they were written in.
- `bench/bench_all.py` runs each SDK head to head against its official SDK
  tuned to drop nothing (`--rounds` sets the runs per configuration).
- C++: published to the Bazel Central Registry as `tpp_telemetry_cpp` by the
  release workflow (`.bcr/`, with a `BCR_PUBLISH_TOKEN` secret); a CMake
  build with `find_package(telemetry)` and `FetchContent`; baggage
  (`Span::set_baggage`) and in-process CPU profiling (`with_profiling`).
- C++: the release asset is the source archive, for Bazel's
  `archive_override` and CMake's `FetchContent`; the static library it
  replaced held only one of the SDK's object files.
- Releases: one `vX.Y.Z` tag releases every SDK at that version, including the
  `telemetry-go/vX.Y.Z` tag Go needs to resolve the module.
- A `justfile` runs every task — tests, lint, benchmarks, the cross-language
  check, image builds, releases (`just release 1.3.0`) and deployments — in
  place of the shell runners.

### Changed

- Releases no longer publish to crates.io or PyPI: the Rust crates and Python
  wheel are attached to the GitHub release, and install from it or the tag.
- Python: gRPC export is the `grpc` extra (`telemetry-py[grpc]`), since grpcio
  cannot be installed on free-threaded Python; without it, gRPC export falls
  back to OTLP/HTTP, from port 4317 to 4318, and warns once.
- Go: the export queue hands records to the exporter a batch at a time rather
  than one by one over a channel, which spent a third of the CPU waking
  goroutines; a log call's file and line are resolved once per call site.
- Rust: a log call prints its console line in one write with a cached
  timestamp and service tag, and no longer repeats the service name, version
  and environment as record attributes (the resource carries them).
- Rust: `Metrics::counter`, `histogram`, `gauge` and `record` take `&self`, so
  one `Metrics` records from any number of threads.
- C++: counters and gauges add atomically rather than under a lock, and
  instruments are found under a reader-writer lock.
- C++: the tracer and logger are looked up once; looking them up per span took
  the provider's lock on every span.
- Rust: the crate is published as `telemetry-rs` (`telemetry` on crates.io is
  an unrelated project); code still imports it as `telemetry`.
- Python: log records no longer repeat the service attributes and labels the
  resource already carries.
- Rust: `with_tracing()` no longer turns on OTLP export, and neither does
  `[telemetry] enabled = true` alone; export needs `with_otlp`,
  `telemetry_local_otel!` or `[telemetry.otlp] enabled = true`, as in Go.
  Spans still get real ids without a collector, for propagation and MCAP.

## [1.0.0] - 2025-11-09

### Added

#### Go SDK

- Complete Go SDK implementation with OpenTelemetry integration
- Structured logging with automatic trace correlation
- Metrics collection (counters, histograms, gauges)
- Distributed tracing with span management
- Continuous profiling with Pyroscope integration
- MCAP recording for offline analysis with Foxglove Studio
- Five comprehensive examples (logging, metrics, tracing, profiling, MCAP)
- Full API documentation in `go/README.md`

#### Rust SDK

- Production-ready Rust implementation by @shyamant and @MrSingletonDude
- OTLP logging and stdout support
- Distributed tracing capabilities
- Metrics recording with declarative macros
- Module-level filtering for logs
- Nix flake support for reproducible builds
- Configuration deserialization from TOML
- Comprehensive examples

#### Observability Stack

- Complete Docker Compose stack with:
  - Loki (log aggregation) - port 3100
  - Tempo (distributed tracing) - port 3200
  - Prometheus (metrics) - port 9090
  - Pyroscope (profiling) - port 4040
  - OpenTelemetry Collector - ports 4317/4318
  - Grafana (visualization) - port 3000
- Pre-configured Grafana dashboards:
  - Logging dashboard with trace correlation
  - Metrics visualization
  - Distributed tracing
  - Continuous profiling
- Automatic datasource configuration

#### CI/CD

- GitHub Actions workflow for Go SDK testing (Ubuntu, macOS)
- Code coverage reporting with Codecov
- Linting for Go, Markdown, and YAML
- Docker Compose stack validation

#### Documentation

- Comprehensive main README with architecture diagrams
- Detailed Go SDK documentation with examples
- OpenTelemetry stack setup guide
- Issue templates (bug reports, feature requests, chores)
- Contributing guidelines
- Apache 2.0 license

### Changed

- Migrated from Bazel to Go modules for simpler dependency management
- Rebranded from Kodo to Telemetry
- Updated all documentation with Telemetry branding
- Renamed project references for consistency
- Updated LICENSE to 2025

### Removed

- Bazel build system configuration
- BuildKite CI/CD (replaced with GitHub Actions)
- Old Python/C++ implementation attempts
- Legacy build scripts and configurations
- Outdated linting configurations

### Contributors

- @shyamant (Shyamant Achar) - Rust SDK foundation
- @MrSingletonDude (Aditya Jindal) - Rust SDK enhancements
- @oh-tarnished (Srikanth Kandarpa) - Go SDK, migration, open source preparation

---

## Release Links

[Unreleased]: https://github.com/the-protobuf-project/telemetry/compare/v1.3.0...HEAD
[1.3.0]: https://github.com/the-protobuf-project/telemetry/releases/tag/v1.3.0
[1.0.0]: https://github.com/the-protobuf-project/telemetry/releases/tag/v1.0.0

---

**Note:** This is the initial open source release of Telemetry by The Protobuf Project.
