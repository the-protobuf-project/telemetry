# Changelog

All notable changes to Telemetry will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
  built-in pprof profiling; `BENCHMARKS.md`.
- Releases: one `vX.Y.Z` tag releases every SDK at that version, including the
  `telemetry-go/vX.Y.Z` tag Go needs to resolve the module.
- A `justfile` runs every task — tests, lint, benchmarks, the cross-language
  check, image builds, releases (`just release 1.3.0`) and deployments — in
  place of the shell runners.

### Changed

- Rust: the crate is published as `telemetry-rs` (`telemetry` on crates.io is
  an unrelated project); code still imports it as `telemetry`.
- Python: log records no longer repeat the service attributes and labels the
  resource already carries.

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

[1.0.0]: https://github.com/the-protobuf-project/telemetry/releases/tag/v1.0.0

---

**Note:** This is the initial open source release of Telemetry by The Protobuf Project.
