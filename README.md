<!-- markdownlint-disable MD041 -->
<h1 align="center">Telemetry</h1>

<p align="center">
  <strong>One framework, every signal.</strong> Telemetry is a unified,
  config-first observability framework for Go, Python, Rust, and C++ — structured
  logging, metrics, distributed tracing, and continuous profiling, built on
  OpenTelemetry standards and shipped with a batteries-included Grafana stack.
</p>

<p align="center">
  <a href="https://github.com/the-protobuf-project/telemetry/actions/workflows/ci.yaml"><img src="https://github.com/the-protobuf-project/telemetry/actions/workflows/ci.yaml/badge.svg" alt="CI"></a>
  <a href="https://github.com/the-protobuf-project/telemetry/actions/workflows/linter.yaml"><img src="https://github.com/the-protobuf-project/telemetry/actions/workflows/linter.yaml/badge.svg" alt="Lint"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-blue.svg" alt="License: Apache 2.0"></a>
  <img src="https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go&logoColor=white" alt="Go">
  <img src="https://img.shields.io/badge/Python-3.12%2B-3776AB?logo=python&logoColor=white" alt="Python">
  <img src="https://img.shields.io/badge/Rust-1.91%2B-000000?logo=rust&logoColor=white" alt="Rust">
  <img src="https://img.shields.io/badge/C%2B%2B-17-00599C?logo=cplusplus&logoColor=white" alt="C++">
</p>

> The Protobuf Project is not affiliated with, endorsed by, or sponsored by
> Google LLC. Protocol Buffers, FlatBuffers, and the Google AIP standards are
> Google's work; gRPC is a Cloud Native Computing Foundation project. See
> [Trademarks and attribution](https://the-protobuf-project.org/license#attribution).

## Contents

- [Overview](#overview)
- [Features](#features)
- [Architecture](#architecture)
- [Language support](#language-support)
- [Configuration — `telemetry.toml`](#configuration--telemetrytoml)
- [Environment variables](#environment-variables)
- [Telemetry signals](#telemetry-signals)
- [Tracing across languages](#tracing-across-languages)
- [Writing at high volume](#writing-at-high-volume)
- [Observability stack](#observability-stack)
- [Repository layout](#repository-layout)
- [Development](#development)
- [Releases](#releases)
- [Use cases](#use-cases)
- [Contributing](#contributing)
- [License](#license)

## Overview

Telemetry is a comprehensive observability framework that provides unified
telemetry across languages. Instrument your code once with a single fluent API and
get structured logging, distributed tracing, metrics collection, and continuous
profiling — exported over standard OTLP and, optionally, recorded to MCAP for
offline analysis in Foxglove Studio.

Every SDK shares the same mental model: build an `Telemetry` instance from a
config file or code, then use the logger, metrics, and tracer it exposes. The same
`telemetry.toml` drives all four languages.

Why not the official OpenTelemetry SDK alone? Given a burst of 1,000,000 log
records, the official Go, Rust, Python and C++ SDKs delivered between 11% and
34% of them and dropped the rest without an error, warning or count. This SDK
counts every drop and reports it, and in lossless mode delivers all 1,000,000.
With parallel exporters, Go delivers every log at the official SDK's rate and
every span at twice its rate. See
[Writing at high volume](#writing-at-high-volume) for each step and what it
costs.

## Features

- **Structured logging** — context-aware logging with automatic trace correlation.
- **Per-module log levels** — fine-grained verbosity control per service or module.
- **Metrics collection** — counters, histograms, and gauges with OpenTelemetry.
- **Distributed tracing** — end-to-end request tracking across service boundaries.
- **Continuous profiling** — production performance analysis with Pyroscope.
- **MCAP recording** — a single file for offline analysis in Foxglove Studio.
- **Config-first** — auto-discovers `telemetry.toml`; TOML, YAML, JSON, and
  environment-variable overrides are all supported.
- **Zero-config defaults** — sensible defaults get you running with no setup.
- **OpenTelemetry native** — standard OTLP protocols for maximum compatibility.

## Architecture

```mermaid
graph TB
    App[Your Application]
    SDK[Telemetry SDK]

    subgraph Signals["Telemetry Signals"]
        Logs[Logs]
        Metrics[Metrics]
        Traces[Traces]
        Profiles[Profiles]
    end

    subgraph Live["Live Pipeline (OTLP)"]
        OTLP[OTLP Collector]
        Loki[Loki]
        Prometheus[Prometheus]
        Tempo[Tempo]
        Pyroscope[Pyroscope]
    end

    Grafana[Grafana Dashboards]

    subgraph Offline["Offline Analysis"]
        MCAP[(MCAP File)]
        Foxglove[Foxglove Studio]
    end

    App --> SDK
    SDK --> Logs
    SDK --> Metrics
    SDK --> Traces
    SDK --> Profiles

    Logs --> OTLP
    Metrics --> OTLP
    Traces --> OTLP
    Profiles --> Pyroscope

    OTLP --> Loki
    OTLP --> Prometheus
    OTLP --> Tempo

    Loki --> Grafana
    Prometheus --> Grafana
    Tempo --> Grafana
    Pyroscope --> Grafana

    Logs --> MCAP
    Metrics --> MCAP
    Traces --> MCAP
    MCAP --> Foxglove
```

## Language support

Four first-class SDKs share one configuration format and one API shape. Expand a
language for installation and a quick start.

| Language | Status | Minimum version | Documentation |
| -------- | ------ | --------------- | ------------- |
| Go       | Stable | 1.25            | [telemetry-go](telemetry-go/README.md) |
| Python   | Stable | 3.12            | [telemetry-py](telemetry-py/README.md) |
| Rust     | Stable | 1.91            | [telemetry-rs](telemetry-rs/README.md) |
| C++      | Beta   | C++17           | [telemetry-cpp](telemetry-cpp) |

<details>
<summary><strong>Go</strong></summary>

```bash
go get github.com/the-protobuf-project/telemetry/telemetry-go
```

```go
package main

import "github.com/the-protobuf-project/telemetry/telemetry-go"

func main() {
    // Auto-discovers telemetry.toml or uses defaults.
    p, err := telemetry.New().
        WithService("my-service", "1.0.0").
        Build()
    if err != nil {
        panic(err)
    }
    defer p.Close()

    p.Logger.Info("Service started")
}
```

See the [Go SDK documentation](telemetry-go/README.md).

</details>

<details>
<summary><strong>Python</strong></summary>

```bash
pip install "git+https://github.com/the-protobuf-project/telemetry.git#subdirectory=telemetry-py"
```

```python
from telemetry import Telemetry

# Auto-discovers telemetry.toml config.
with Telemetry.new().build() as o:
    o.logger.info("Service started")
    o.logger.warning("Rate limit approaching", {"percent": 85})
```

See the [Python SDK documentation](telemetry-py/README.md).

</details>

<details>
<summary><strong>Rust</strong></summary>

```toml
[dependencies]
telemetry = { package = "telemetry-rs", git = "https://github.com/the-protobuf-project/telemetry.git" }
tokio = { version = "1", features = ["macros", "rt-multi-thread"] }
anyhow = "1.0"
```

```rust
use telemetry::{Telemetry, Environment, logger};

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    // Auto-discovers telemetry.toml config.
    let _telemetry = Telemetry::new()
        .with_service("my-service", "1.0.0")
        .environment(Environment::Production)
        .build()?;

    logger::info!("Service started");
    Ok(())
}
```

See the [Rust SDK documentation](telemetry-rs/README.md).

</details>

<details>
<summary><strong>C++</strong></summary>

Add the module dependency in your `MODULE.bazel`:

```starlark
bazel_dep(name = "tpp_telemetry.cpp", version = "1.0.0")
```

```cpp
#include <telemetry/telemetry.hpp>

int main() {
    auto o = telemetry::Telemetry::builder("my-service", "1.0.0")
        .environment(telemetry::Environment::Development)
        .build();

    TELEMETRY_LOG_INFO("Service started");
    o.metrics().counter("requests_total", 1.0);

    auto span = o.tracer().start_span("process_request");
    span.set_attribute("user_id", "12345");
    span.end();
}
```

See the [C++ SDK sources and examples](telemetry-cpp).

</details>

## Configuration — `telemetry.toml`

All SDKs auto-discover `telemetry.toml` from your project root:

```toml
[service]
name = "my-service"
version = "1.0.0"
environment = "development"

[telemetry.otlp]
endpoint = "otel.example.com"   # Port 4317 auto-added
auth_token = "your-token"

[logging]
level = 2                        # Global log level (1=Error, 2=Info, 3=Debug)

[logging.modules.nats-module]
level = 1                        # Override: Error only for this module
```

**Precedence**, lowest to highest; each source overrides the ones above it:

1. Built-in defaults
2. `telemetry.toml`
3. `.env` and `TELEMETRY_*` environment variables
4. Settings made in code

See the full [Configuration Guide](docs/configuration.md).

## Environment variables

Every config key can be overridden with an `TELEMETRY_`-prefixed environment
variable. Nesting is expressed with a single underscore in Go, Rust, and C++, and a
double underscore in Python.

| Language        | Prefix            | Nesting            | Example |
| --------------- | ----------------- | ------------------ | ------- |
| Go / Rust / C++ | `TELEMETRY_` | `_` (single)       | `TELEMETRY_TELEMETRY_OTLP_ENDPOINT` |
| Python          | `TELEMETRY_` | `__` (double)      | `TELEMETRY_TELEMETRY__OTLP__ENDPOINT` |

```bash
export TELEMETRY_SERVICE_NAME=my-service
export TELEMETRY_TELEMETRY_OTLP_ENDPOINT=otel.example.com:4317
export TELEMETRY_LOGGING_MODULES_VISION_LEVEL=3
```

## Telemetry signals

| Signal      | What you get |
| ----------- | ------------ |
| Logging     | Structured, context-aware logs with trace correlation and per-module levels. |
| Metrics     | Counters, histograms, and gauges exported over OTLP. |
| Tracing     | Distributed spans with attributes and events across service boundaries. |
| Profiling   | Continuous CPU/memory profiling delivered to Pyroscope. |
| MCAP        | Unified recording of signals to an MCAP file for Foxglove Studio. |

### Every signal, every language

Each band is one signal an SDK supports, so a language's width is how much of
the framework it covers and a signal's width is how many SDKs have it. The
table below gives the API for each, and what is still missing.

```mermaid
---
config:
  sankey:
    showValues: false
    linkColor: source
    nodeAlignment: justify
---
sankey-beta

Go,Traces,1
Go,Metrics,1
Go,Logs,1
Go,Trace propagation,1
Go,Baggage,1
Go,Distributed logging,1
Go,Profiles,1
Go,MCAP,1
Rust,Traces,1
Rust,Metrics,1
Rust,Logs,1
Rust,Trace propagation,1
Rust,Baggage,1
Rust,Distributed logging,1
Rust,Profiles,1
Rust,MCAP,1
Python,Traces,1
Python,Metrics,1
Python,Logs,1
Python,Trace propagation,1
Python,Baggage,1
Python,Distributed logging,1
Python,Profiles,1
Python,MCAP,1
C++,Traces,1
C++,Metrics,1
C++,Logs,1
C++,Trace propagation,1
C++,Baggage,1
C++,Distributed logging,1
C++,MCAP,1
```

| Signal | Go | Rust | Python | C++ |
| ------ | -- | ---- | ------ | --- |
| Traces over OTLP | Yes | Yes | Yes | Yes (OTLP export added; was MCAP-only) |
| Metrics over OTLP | Yes | Yes | Yes | Yes |
| Logs over OTLP | Yes | Yes | Yes | Yes |
| Trace context across services (W3C `traceparent`) | Yes: `Inject` / `Extract` | Yes: `inject` / `extract` | Yes: `inject` / `extract` | Yes: `inject` / `extract` |
| Baggage | Yes: `SetBaggage` / `Baggage` | Yes: `set_baggage` / `baggage` | Yes: `set_baggage` / `get_baggage` | Yes: carried from `extract` to `inject` |
| Distributed logging (logs carry the trace id) | Yes: `InfoContext(ctx, …)`, `Logger.WithContext` | Yes: inside `span.attach()` | Yes: inside `start_span` | Yes: inside `span.attach()` / `TELEMETRY_SPAN` |
| Profiles to Pyroscope | Yes: `WithProfiling` | Yes: `with_profiling` (`profiling` feature) | Yes: `with_profiling` (`[profiling]` extra) | Through an eBPF profiler (no Pyroscope C++ client exists; see below) |
| MCAP recording | logs, metrics, spans | logs, metrics, spans | logs, metrics, spans | logs, metrics, spans |
| Drops counted and reported, lossless mode | Yes: `WithExport`, `Stats()`, `Close` error | Yes: `with_export`, `stats()`, `close` error | Yes: `with_export`, `stats()`, warning on `close` | Yes: `with_export`, `stats()`, warning on `close` |

`just crosslang` checks one trace across all four, passed from Go to Rust to
Python to C++, and CI runs it on every change.

C++ profiles come from outside the process: Pyroscope has no C++ client
library, so the supported route is an eBPF profiler — Grafana Alloy's
`pyroscope.ebpf` component or the OpenTelemetry eBPF profiler — running beside
the service on Linux, which profiles native code without changes to it and
sends to the same Pyroscope.

## Tracing across languages

A trace crosses a service boundary in three headers defined by the
[W3C Trace Context](https://www.w3.org/TR/trace-context/) and
[Baggage](https://www.w3.org/TR/baggage/) standards:

```mermaid
flowchart TB
    subgraph TP["traceparent: 00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"]
        direction LR
        V["00<br/>version"] --- T["4bf92f3577b34da6a3ce929d0e0e4736<br/>trace id, shared by every span"]
        T --- P["00f067aa0ba902b7<br/>parent span id: the caller's span"]
        P --- F["01<br/>flags: sampled"]
    end
    TS["tracestate<br/>vendor-specific data, passed through untouched"]
    BG["baggage<br/>key=value pairs that travel with the request, e.g. tenant=acme"]
```

Every Telemetry SDK installs this propagator when it is built. The **caller
injects** its span's context into the outgoing request's headers; the
**callee extracts** it and starts its span under it. Both spans then share
one trace id, and Tempo shows the request as one trace across both services,
whatever language each is written in.

Say service A (`orders`, Go) calls service B (`inventory`, Rust) over HTTP:

```go
// Service A — Go: start a client span and put its context on the request.
ctx, span := tel.Tracing.StartSpan(ctx, "inventory.reserve",
    telemetry.WithSpanKind(telemetry.SpanKindClient))
defer span.End()

headers := map[string]string{}
tel.Tracing.Inject(ctx, headers) // writes traceparent (+ tracestate, baggage)

req, _ := http.NewRequestWithContext(ctx, "POST", inventoryURL, body)
for k, v := range headers {
    req.Header.Set(k, v)
}
```

```rust
// Service B — Rust: continue the caller's trace from the request headers.
use telemetry::tracing::SpanKind;

let headers: HashMap<String, String> = request_headers_lowercased(&req);
let tracing = telemetry.tracing.as_ref().unwrap();

let parent = tracing.extract(&headers); // the Go span, as a remote parent
let mut span = tracing.start_span_with("inventory.reserve", SpanKind::Server, Some(&parent));
span.set_attribute("order.id", order_id);

// Calling a third service? Hand the trace on the same way.
let mut out = HashMap::new();
tracing.inject(&span, &mut out);
```

The reverse direction is the same calls on the other side:
`tracing.inject` in Rust and `tel.Tracing.Extract(ctx, headers)` followed by
`StartSpan(ctx, ..., WithSpanKind(SpanKindServer))` in Go. Over gRPC the
headers are the request metadata; over a message broker they are the message
headers (`SpanKindProducer` / `SpanKindConsumer`).

Things that break the chain:

- **The callee does not extract.** It starts a new trace, and Tempo shows two
  unrelated traces. Always start the server span from the extracted context.
- **Header names change case.** HTTP header names are case-insensitive, and
  the propagator expects lowercase `traceparent`. Lowercase keys when copying
  headers into a map.
- **A proxy strips unknown headers.** Allow `traceparent`, `tracestate` and
  `baggage` through gateways and load balancers.

Try it locally. One trace through four services, each written with a
different SDK:

```bash
just crosslang
# go     (orders-go)       trace cd3b5eda…  span ef132842…
# rust   (inventory-rs)    trace cd3b5eda…  span 70f84c9e…  parent ef132842…
# python (billing-py)      trace cd3b5eda…  span 0585d24b…  parent 70f84c9e…
# c++    (shipping-cpp)    trace cd3b5eda…  span 4c7cf38d…  parent 0585d24b…
# OK: one trace across every service
```

CI runs the same check on every change, so a regression in either SDK's
propagation fails the build. With the stack running
(`telemetry-core`, `docker compose up -d`), set
`TELEMETRY_DEMO_OTLP=localhost:4317` to see the single trace in Grafana Tempo.

## Writing at high volume

### The starting point: the official SDKs

Every OpenTelemetry SDK exports logs and spans the same way. Each call puts
the record in a bounded queue (2048 slots by default), and a background
exporter sends batches to the collector. When a program writes faster than the
collector accepts, the queue fills and the SDK throws the overflow away. It
does not return an error, log a warning or keep a count.

We gave each language's official SDK the same burst of 1,000,000 records,
on one thread, with its default settings:

| Official SDK | Logs delivered | Spans delivered |
| ------------ | -------------: | --------------: |
| Go | 272,896 (27%) | 418,816 (42%) |
| Rust | 110,180 (11%) | 149,536 (15%) |
| Python | 335,581 (34%) | 499,200 (50%) |
| C++ | 216,760 (22%) | 841,406 (84%) |

The calls returned quickly, and between 16% and 89% of the records never
arrived. Nothing in the program said so.

### What this SDK changes

This SDK keeps the OpenTelemetry data model and OTLP wire format and replaces
the part that loses data. Each step below changes one thing, measured in Go
with the same 1,000,000-record burst:

| Step | Logs delivered | Logs per second | Spans delivered | Spans per second |
| ---- | -------------: | --------------: | --------------: | ---------------: |
| 1. Official Go SDK alone | 272,896 | 1,684,187 | 418,816 | 1,958,812 |
| 2. This SDK, default mode | 569,955 | 552,613 | 588,575 | 1,729,006 |
| 3. Lossless mode | 1,000,000 | 324,539 | 1,000,000 | 1,065,046 |
| 4. Lossless, 14 goroutines, batch 8192 | 1,000,000 | 379,073 | 1,000,000 | 1,451,022 |
| 5. Lossless, plus 4 parallel exporters | 1,000,000 | 1,698,293 | 1,000,000 | 4,370,134 |

1. **Official SDK.** This is the baseline: fast calls, most records dropped,
   and no sign of it.
2. **Count every drop.** In default mode this SDK still drops when the queue
   is full, but it counts every record it drops and reports the total when the
   program closes telemetry. Lost data becomes a number you can alert on.
3. **Lossless mode.** The writer waits for room in the queue instead of
   dropping. All 1,000,000 records arrive. A single writer is slower because it
   now runs at the speed of the export rather than the speed of the queue.
4. **Bigger batches, more writers.** A larger queue and 8192-record batches
   mean fewer export requests, and writers on every core keep the queue full.
5. **Parallel exporters.** One exporter was the bottleneck: it sends a batch
   and waits for the reply before sending the next. Four exporters send
   batches concurrently, while the queue stays lossless.

At step 5, logs arrive in full at the same rate the official SDK reached while
it was losing 73% of them, and spans arrive at more than twice its rate. The
official numbers come from one thread, so this is not a like-for-like speed
contest. The point is that the official SDK cannot be configured to deliver
everything, while this SDK delivers everything and scales with exporters.

### What it costs per call

Per call, this SDK does more work than the official one: console output, MCAP
recording, caller lookup and drop accounting. On one thread, in default mode,
it runs at this fraction of the official SDK's call rate:

| Signal | Go | Rust | Python | C++ |
| ------ | -: | ---: | -----: | --: |
| Logs | 0.33x | 0.07x | 0.24x | 0.16x |
| Spans | 0.88x | 0.56x | 0.87x | 1.0x |
| Metrics | 0.43x | 0.25x | 0.70x | 0.04x |

Spans cost about the same as upstream. The gaps in logs and metrics are where
the next improvements go. Rust's log macro formats every message even when
console output is discarded. C++ counters are looked up by name under a lock
on every call. Go's struct-tag metrics inspect the struct on every call. The
official SDK's speed numbers count the records it dropped; this SDK's numbers
count only records that were delivered or reported.

### Configuring it

Go, Rust and Python take the same export options:

```go
tel, err := telemetry.New().
    WithService("ingest", "1.0.0").
    WithExport(telemetry.ExportOptions{
        Lossless:  true,  // wait for room instead of dropping (default: drop)
        QueueSize: 65536, // records waiting per signal (default 2048)
        BatchSize: 8192,  // records per export request (default 512)
    }).
    Build()
```

```rust
let telemetry = Telemetry::builder("ingest", "1.0.0")
    .with_export(ExportOptions { lossless: true, queue_size: 65_536, ..Default::default() })
    .build()?;
```

```python
t = Telemetry.new().with_service("ingest", "1.0.0").with_export(lossless=True).build()
```

Either way nothing is lost silently. Every drop is counted, and reported when
the program closes telemetry (an error from `Close` in Go and `close` in Rust,
a warning in Python):

```text
telemetry: 935442 of 1000000 log records dropped (export queue full);
use WithExport(ExportOptions{Lossless: true}) or a larger QueueSize to keep them
```

`tel.Stats()` gives the counts at any time (emitted, exported, dropped,
failed per signal). The same settings come from `[telemetry.export]` in
`telemetry.toml`, `TELEMETRY_TELEMETRY__EXPORT__LOSSLESS=true`, or the
standard `OTEL_BSP_*` / `OTEL_BLRP_*` variables.

To find where time goes when a run falls short, `telemetry-loadgen` writes a
million records through the SDK and profiles the run:

```bash
cd telemetry-go
go run ./cmd/telemetry-loadgen -n 1000000 -lossless \
    -cpuprofile cpu.out -memprofile mem.out -mutexprofile mutex.out -trace trace.out
go tool pprof -http=: cpu.out
```

It reports emitted vs. delivered per signal, against an in-process collector
or a real one (`-endpoint host:4317`). Results for every SDK are in
[BENCHMARKS.md](docs/benchmark.md).

## Observability stack

Telemetry ships a complete, pre-configured stack in
[`telemetry-core`](telemetry-core/README.md), powered by
industry-standard tools:

- **Loki** — log aggregation
- **Tempo** — distributed tracing
- **Prometheus** — metrics storage
- **Pyroscope** — continuous profiling
- **Grafana** — unified dashboards
- **OpenTelemetry Collector** — telemetry pipeline

```bash
cd telemetry-core
docker compose up -d
```

Grafana is then available at `http://localhost:3000` with all datasources
pre-configured.

Read more: [Observability stack](telemetry-core/README.md) ·
[Production deployment](telemetry-core/deploy/production/README.md)

## Repository layout

| Path | Description |
| ---- | ----------- |
| [`telemetry-go`](telemetry-go)     | Go SDK |
| [`telemetry-py`](telemetry-py)     | Python SDK |
| [`telemetry-rs`](telemetry-rs)     | Rust SDK (workspace: `telemetry`, `telemetry-derive`, `telemetry-examples`) |
| [`telemetry-cpp`](telemetry-cpp)   | C++ SDK (CMake and Bazel) |
| [`telemetry-core`](telemetry-core) | Observability stack and production deployment |
| [`docs`](docs)                               | Configuration and usage guides |

## Development

Each SDK is self-contained and can be built and tested independently.

```bash
# Go
cd telemetry-go && go build ./... && go test ./...

# Rust
cd telemetry-rs && cargo build --all-targets && cargo test

# Python
cd telemetry-py && pip install . && ruff check .

# C++ (Bazel)
cd telemetry-cpp && bazel build //...
```

Continuous integration builds and tests all four SDKs and validates the
observability stack. See [`.github/workflows`](.github/workflows) for the CI and
release pipelines.

## Releases

One command releases every SDK at one version:

```bash
just release 1.3.0
```

It stamps the version into every manifest (`just set-version`), commits it,
tags `v1.3.0` and pushes; the release workflow does the rest.

| SDK    | Published as | Install |
| ------ | ------------ | ------- |
| Go     | tag `telemetry-go/v1.3.0`, pushed by the workflow | `go get github.com/the-protobuf-project/telemetry/telemetry-go@v1.3.0` |
| Rust   | `.crate` files on the GitHub release, and the git tag | `telemetry-rs = { git = "https://github.com/the-protobuf-project/telemetry", tag = "v1.3.0" }` |
| Python | wheel and sdist on the GitHub release | `pip install https://github.com/the-protobuf-project/telemetry/releases/download/v1.3.0/telemetry_py-1.3.0-py3-none-any.whl` |
| C++    | source archive on the GitHub release, and the Bazel Central Registry | `bazel_dep(name = "tpp_telemetry.cpp", version = "1.3.0")`, or CMake `FetchContent` |

The Go module lives in a subdirectory, so Go resolves its versions from tags
prefixed with that directory (`telemetry-go/v1.3.0`), not from the repository's
`v1.3.0`. The release workflow creates that tag on the same commit and asks
the Go proxy for it, so `go get` and pkg.go.dev see the release at once.

The C++ module goes to the Bazel Central Registry as a pull request, made by
[publish-to-bcr](https://github.com/bazel-contrib/publish-to-bcr) from the
templates in [`.bcr/`](.bcr). It needs, once:

1. A fork of [bazelbuild/bazel-central-registry](https://github.com/bazelbuild/bazel-central-registry):
   [the-protobuf-project/bazel-central-registry](https://github.com/the-protobuf-project/bazel-central-registry)
   (the `BCR_FORK` repository variable names another).
2. A classic personal access token with the `repo` and `workflow` scopes,
   saved as the `BCR_PUBLISH_TOKEN` repository secret.

Without the secret, the release skips this step. The first version of a new
module waits for a BCR maintainer to review its pull request; until it is
merged, `archive_override` takes the release archive (the release notes show
how).

Every C++ release is attested, the way the BCR asks for it. bazel-contrib's
[release_ruleset](https://github.com/bazel-contrib/.github/blob/master/.github/workflows/release_ruleset.yaml)
runs the C++ tests, builds the source archive with
[`release_prep.sh`](.github/workflows/release_prep.sh) and signs its SLSA
build provenance (`telemetry-cpp-<version>.tar.gz.intoto.jsonl`). publish-to-bcr
then attests the entry's `MODULE.bazel` and `source.json` and adds an
`attestations.json`, which BCR presubmit verifies and the registry shows on
the version's Attestations tab. The GitHub release stays a draft until those
attestations are uploaded to it. Anyone can check the archive:

```bash
gh attestation verify telemetry-cpp-1.3.0.tar.gz --repo the-protobuf-project/telemetry
```

## Use cases

- **Microservices** — track requests across service boundaries.
- **API services** — monitor performance and errors.
- **Robotics** — record and analyze system behavior with MCAP.
- **ML pipelines** — trace data-processing workflows.
- **Production debugging** — correlate logs, traces, and metrics.

## Contributing

Contributions are welcome. Fork the repository, create a feature branch, make your
changes, and open a pull request. Please ensure the relevant SDK builds and its
tests and linters pass before submitting.

## License

Copyright &copy; 2026 The Protobuf Project.

Licensed under the [Apache License, Version 2.0](LICENSE).
