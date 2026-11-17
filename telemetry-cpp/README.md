# Telemetry - C++ SDK

> The Protobuf Project is not affiliated with, endorsed by, or sponsored by
> Google LLC. Protocol Buffers, FlatBuffers, and the Google AIP standards are
> Google's work; gRPC is a Cloud Native Computing Foundation project. See
> [Trademarks and attribution](https://the-protobuf-project.org/license#attribution).

Logging, metrics, distributed tracing and continuous profiling for C++ services,
on the same model as the Go, Rust and Python SDKs: build one `Telemetry` and use
the logger, metrics, tracer and profiler it holds. Records go over OTLP/HTTP to
an OpenTelemetry collector, to an MCAP file for Foxglove Studio, or both.

## Contents

- [Features](#features)
- [Requirements](#requirements)
- [Installation](#installation)
- [Quick start](#quick-start)
- [Configuration](#configuration)
- [Logging](#logging)
- [Metrics](#metrics)
- [Distributed tracing](#distributed-tracing)
- [Baggage](#baggage)
- [Profiling](#profiling)
- [Writing at high volume](#writing-at-high-volume)
- [MCAP recording](#mcap-recording)
- [Examples](#examples)
- [Development](#development)
- [Project structure](#project-structure)
- [Troubleshooting](#troubleshooting)
- [License](#license)

## Features

- **Structured logging** to the console, OTLP and MCAP, with the file and line
  of each call; a record written inside a span carries its trace and span id.
- **Metrics**: counters, histograms and gauges, safe to record from any thread.
- **Distributed tracing** with W3C `traceparent` propagation, so one trace runs
  through services written in Go, Rust, Python and C++.
- **Baggage**: W3C baggage set on a span reaches every service downstream.
- **Continuous CPU profiling** to Pyroscope, in process.
- **Lossless export**: every log record and span is counted; nothing is
  dropped without being reported, and lossless mode drops nothing at all.
- **MCAP recording** of logs, metrics and spans in one file.
- **Embedded**: builds for FreeRTOS, without the console, OTLP or profiling.

## Requirements

- C++17.
- Bazel 9.3.0 or later, or CMake 3.14 or later.
- Linux or macOS. Profiling needs one of them; FreeRTOS builds leave it out.

## Installation

### Bazel (bzlmod)

From the Bazel Central Registry, in your `MODULE.bazel`:

```starlark
bazel_dep(name = "tpp_telemetry.cpp", version = "1.3.3")
```

[`examples/bazel`](examples/bazel) is a complete module that does this; CI
builds and tests it against the registry.

Before a version reaches the registry, take it from the GitHub release (each
release's notes give the `integrity` value):

```starlark
bazel_dep(name = "tpp_telemetry.cpp", version = "1.3.0")
archive_override(
    module_name = "tpp_telemetry.cpp",
    urls = ["https://github.com/the-protobuf-project/telemetry/releases/download/v1.3.0/telemetry-cpp-1.3.0.tar.gz"],
    strip_prefix = "telemetry-cpp-1.3.0",
    integrity = "sha256-...",
)
```

and in a `BUILD.bazel`:

```starlark
cc_binary(
    name = "service",
    srcs = ["main.cpp"],
    deps = ["@tpp_telemetry.cpp//:telemetry"],
)
```

The module brings opentelemetry-cpp, spdlog, mcap, curl and nlohmann_json with
it from the Bazel Central Registry.

### CMake

With `FetchContent`, from the release archive:

```cmake
include(FetchContent)
FetchContent_Declare(
    telemetry
    URL https://github.com/the-protobuf-project/telemetry/releases/download/v1.3.0/telemetry-cpp-1.3.0.tar.gz
)
FetchContent_MakeAvailable(telemetry)

target_link_libraries(service PRIVATE telemetry::telemetry)
```

Or install it and use `find_package`:

```bash
cmake -S telemetry-cpp -B build -DCMAKE_BUILD_TYPE=Release
cmake --build build -j
cmake --install build --prefix /usr/local
```

```cmake
find_package(telemetry CONFIG REQUIRED)
target_link_libraries(service PRIVATE telemetry::telemetry)
```

CMake uses installed opentelemetry-cpp and spdlog packages when it finds them
(`find_package`) and otherwise builds them from source; opentelemetry-cpp then
needs protobuf and libcurl installed (`brew install protobuf curl`,
`apt install libprotobuf-dev protobuf-compiler libcurl4-openssl-dev`). Installed
with `cmake --install`, the package's `find_package(telemetry)` looks for the
same dependencies again.

| CMake option | Default | |
| ------------ | ------- | - |
| `TELEMETRY_USE_OTEL` | `ON` | OTLP export through opentelemetry-cpp |
| `TELEMETRY_PROFILING` | `ON` | CPU profiling to Pyroscope (needs libcurl) |
| `TELEMETRY_BUILD_EXAMPLES` | `ON` when top level | the programs in `examples/` |
| `TELEMETRY_BUILD_TESTS` | `OFF` | the Catch2 tests, run with `ctest` |
| `TELEMETRY_FREERTOS` | `OFF` | build for FreeRTOS |

## Quick start

```cpp
#include <telemetry/telemetry.hpp>

int main() {
    auto telemetry = telemetry::Telemetry::builder("orders", "1.0.0")
                         .environment(telemetry::Environment::Production)
                         .with_otlp("localhost", 4318)  // OTLP/HTTP
                         .build();

    TELEMETRY_LOG_INFO("service started");
    telemetry.metrics().counter("requests_total");

    {
        auto span = telemetry.tracer().start_span("handle_request");
        span.set_attribute("order.id", "o-42");
        auto scope = span.attach();  // logs and spans below belong to it
        TELEMETRY_LOG_INFO("charging the card");
        span.end();
    }

    telemetry.close();  // exports what is queued; reports anything dropped
}
```

### OTLP to a local collector

The stack in [`telemetry-core`](../telemetry-core/README.md) takes OTLP/HTTP on
port 6009 (Envoy in front of the collector):

```cpp
auto telemetry = telemetry::Telemetry::builder("orders", "1.0.0").with_otlp("localhost", 6009).build();
```

## Configuration

The C++ SDK is configured in code, with the builder. It does not read
`telemetry.toml` or `TELEMETRY_*` environment variables, which the Go, Rust and
Python SDKs do.

| Builder method | |
| -------------- | - |
| `builder(name, version)` | service name and version, on every record |
| `.description(text)` | service description |
| `.environment(Environment::Production)` | `Development` (default), `Staging`, `Production` |
| `.with_otlp(host, port)` | export logs, metrics and spans over OTLP/HTTP |
| `.with_export(ExportOptions{...})` | queue, batch, lossless, [exporters](#writing-at-high-volume) |
| `.with_console(false)` | no console lines; OTLP and MCAP still get them |
| `.with_mcap(path)` | record everything to an MCAP file |
| `.with_profiling(address)` | profile CPU to Pyroscope; see [Profiling](#profiling) |

## Logging

```cpp
TELEMETRY_LOG_DEBUG("cache warm");
TELEMETRY_LOG_INFO("user logged in");
TELEMETRY_LOG_WARN("retrying");
TELEMETRY_LOG_ERROR("payment declined");
TELEMETRY_LOG_FATAL("cannot continue");
```

The macros record the file and line of the call. Without them:

```cpp
telemetry.logger().info("user logged in", __FILE__, __LINE__);
```

Structured data is any type with a `to_json()` returning a JSON string:

```cpp
struct Login {
    std::string user;
    std::string to_json() const { return R"({"user":")" + user + R"("})"; }
};

TELEMETRY_LOG_INFO_DATA("user logged in", Login{"ada"});
```

`telemetry.logger().set_level(telemetry::logging::Level::Warn)` drops anything
below a level, and `add_sink` takes every record for a destination of your
own:

```cpp
telemetry.logger().add_sink([](const telemetry::logging::LogEntry& e) {
    // e.level, e.message, e.file, e.line, e.timestamp_ns, e.trace_id, e.span_id
});
```

### Distributed logging

A record written while a span is attached carries that span's trace and span
id, so Grafana links the log to its trace, across services:

```cpp
auto span = telemetry.tracer().start_span("checkout");
auto scope = span.attach();
TELEMETRY_LOG_INFO("reserving stock");  // carries checkout's trace id
```

## Metrics

```cpp
auto& m = telemetry.metrics();
m.counter("requests_total");             // add 1
m.counter("bytes_sent_total", 512);      // add 512
m.histogram("request_duration_ms", 42.5);
m.gauge("queue_depth", 17);              // set to 17
```

Recording is safe from any thread. Each name's instrument is made on first use;
after that each thread finds it in a small cache of its own, without a lock or
an allocation. A counter keeps one cell per core, so threads seldom write the
same cache line, and OpenTelemetry reads the sum when it collects (an
observable counter) instead of being called on every increment. A gauge is
exported as an observable gauge holding the last value set; histograms record
each value as it comes.

Keeping a reference to the instrument skips the lookup altogether:

```cpp
auto& requests = m.get_counter("requests_total");
requests.add();
```

A type can describe several metrics at once:

```cpp
namespace tm = telemetry::metrics;

struct Batch : tm::RecordMetrics {
    double processed = 0, duration_ms = 0;
    std::vector<tm::MetricField> metric_fields() const override {
        using T = tm::MetricType;
        return {{"batch.processed", T::Counter, "records processed", processed},
                {"batch.duration_ms", T::Histogram, "batch time", duration_ms}};
    }
};

Batch batch;
batch.processed = 128;
batch.duration_ms = 3.2;
telemetry.metrics().record(batch);
```

## Distributed tracing

```cpp
auto& tracer = telemetry.tracer();

auto span = tracer.start_span("process_order", telemetry::tracing::SpanKind::Server);
span.set_attribute("order.id", "o-42");
span.set_attribute("items", int64_t{3});
span.add_event("validated");
span.set_status(telemetry::tracing::SpanStatus::Ok);
span.end();
```

A span started with no parent continues the span attached on its thread, and
otherwise starts a new trace. `TELEMETRY_SPAN` opens one for the rest of the
scope:

```cpp
void reserve_stock(telemetry::tracing::Tracer& tracer) {
    TELEMETRY_SPAN(tracer, "reserve_stock");
    TELEMETRY_LOG_INFO("reserving");  // inside reserve_stock
}
```

### Across services

`inject` writes the W3C `traceparent` (and `baggage`) headers for the next
service; `extract` reads them from the caller, whatever language it is in:

```cpp
// Calling another service.
std::map<std::string, std::string> headers;
tracer.inject(span, headers);  // send headers with the request

// Handling a request.
auto parent = tracer.extract(request_headers);
// Continues the caller's trace.
auto span = tracer.start_span("billing.charge", parent);
```

`just crosslang`, from the repository root, sends one trace through Go, Rust,
Python and C++ and checks it arrives whole.

## Baggage

Baggage is key-value context that travels with a trace to every service it
reaches, as the W3C `baggage` header:

```cpp
auto span = tracer.start_span("checkout");
span.set_baggage("tenant", "acme");

auto child = tracer.start_span("charge", span);  // child.baggage() has tenant=acme
{
    auto scope = span.attach();
    auto reserve = tracer.start_span("reserve");  // so does this one
    tracer.inject(reserve, headers);              // and so does the next service
}
```

Baggage a caller sent arrives with `extract`, and `span.baggage()` reads it.

## Profiling

```cpp
auto telemetry = telemetry::Telemetry::builder("orders", "1.0.0")
                     .with_profiling("http://localhost:4040")
                     .build();
```

The profiler samples the stack of whichever thread is on the CPU 100 times a
second of CPU time, and every 10 seconds sends the stacks it saw to Pyroscope's
ingest API, under `<service>.<environment>` with `service`, `version` and
`environment` tags, as the Go, Rust and Python SDKs do. It runs on Linux and
macOS; one profiler runs per process. `close()` sends what is left.

Sampling is a `SIGPROF` timer: the signal handler only copies the stack into a
fixed buffer, and a background thread names the functions and sends them.
Function names come from the dynamic symbol table, so on Linux link with
`-rdynamic` to see your own functions rather than addresses:

```starlark
cc_binary(name = "service", linkopts = ["-rdynamic"], ...)
```

For finer control, `telemetry::profiling::Profiler` takes `ProfilingOptions`:
sample rate, upload interval, extra tags and basic auth for Grafana Cloud.

## Writing at high volume

Logs and spans go to the collector in batches from a bounded queue. When a
program writes faster than the collector takes them, the queue fills, and by
default what does not fit is dropped. Every drop is counted, and `close()`
reports it:

```text
telemetry: 935442 of 1000000 log records dropped (export queue full); use
with_export(ExportOptions{.lossless = true}) or a larger queue_size to keep them
```

To keep everything, and to export faster:

```cpp
telemetry::ExportOptions exp;
exp.lossless = true;      // wait for room instead of dropping (default: drop)
exp.queue_size = 65536;   // records each exporter holds (default 2048)
exp.batch_size = 8192;    // records per export request (default 512)
exp.exporters = 8;        // exporters per signal, in parallel (default 1)

auto telemetry = telemetry::Telemetry::builder("ingest", "1.0.0")
                     .with_otlp("localhost", 4318)
                     .with_export(exp)
                     .with_console(false)
                     .build();
```

`telemetry.stats()` gives what happened to each signal at any time: emitted,
exported, failed, and pending (dropped, once closed). Against the official
OpenTelemetry C++ SDK tuned to drop nothing, this delivers logs at three to
four times its rate; see [BENCHMARKS.md](../docs/benchmark.md).

## MCAP recording

```cpp
auto telemetry = telemetry::Telemetry::builder("robot", "1.0.0").with_mcap("run.mcap").build();
```

Logs, metrics and spans go to one MCAP file, which Foxglove Studio opens.
`telemetry.flush()` writes what is buffered; `close()` finishes the file.

## Examples

```bash
bazel run //examples:main          # logging, metrics and tracing to MCAP
bazel run //examples:logging       # log levels and structured data
bazel run //examples:logging_mcap  # logs recorded to MCAP
bazel run //examples:metrics       # counters, histograms, gauges
bazel run //examples:tracing       # spans over OTLP
bazel run //examples:crosslang     # one hop of the cross-language trace
bazel run -c opt //examples:loadgen -- logs 1000000 127.0.0.1:4318  # load test
```

## Development

```bash
bazel build //...
bazel test //tests/...
```

or with CMake:

```bash
cmake -S . -B build -DTELEMETRY_BUILD_TESTS=ON
cmake --build build -j
ctest --test-dir build --output-on-failure
```

From the repository root, `just test-cpp` runs the Bazel build and tests.

## Project structure

```text
telemetry-cpp/
├── MODULE.bazel          # Bazel module "tpp_telemetry.cpp" and its dependencies
├── CMakeLists.txt        # the same library for CMake
├── BUILD.bazel           # //:telemetry, the library for other modules
├── include/telemetry/    # public headers
│   ├── telemetry.hpp     # Telemetry and its builder
│   ├── config.hpp        # options, ExportOptions, Stats
│   ├── context.hpp       # the trace context of the current thread
│   ├── logging/          # Logger, the TELEMETRY_LOG_* macros
│   ├── metrics/          # Counter, Histogram, Gauge, Metrics
│   ├── tracing/          # Tracer, Span, TELEMETRY_SPAN
│   ├── profiling/        # Profiler
│   ├── otel/             # the OpenTelemetry export pipeline
│   └── mcap/             # MCAP writer
├── src/                  # their implementations
├── examples/
├── tests/                # Catch2
└── tools/                # gazelle (bazel run //tools:gazelle)
```

## Troubleshooting

### Nothing reaches the collector

`with_otlp` takes the OTLP/HTTP port (4318, or 6009 through the stack's Envoy),
not the gRPC port 4317. `telemetry.stats()` shows whether records failed to
export (the collector refused or was unreachable) or were dropped.

### Profiles show addresses, not function names

Link with `-rdynamic` on Linux, and keep symbols (do not strip the binary).

### "profiling is already running in this process"

The sampling timer is process-wide, so only the first `Profiler` samples; a
second one does nothing.

## License

Apache 2.0. See [LICENSE](../LICENSE).
