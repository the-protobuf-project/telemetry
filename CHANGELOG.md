# Changelog

All notable changes to Telemetry will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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

**Note:** This is the initial open source release of Telemetry by Machani Robotics.
