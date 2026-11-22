# Telemetry - TypeScript SDK

Logs, metrics, traces and profiles for Node.js and the browser, over
OpenTelemetry, with MCAP recording for Foxglove Studio. Same `telemetry.toml`
and the same API as the Go, Rust, Python and C++ SDKs, so a trace started in
any of them continues here.

## Contents

- [Installation](#installation)
- [Quick start](#quick-start)
- [Node and the browser](#node-and-the-browser)
- [Configuration](#configuration)
- [Logging](#logging)
- [Tracing](#tracing)
- [Metrics](#metrics)
- [Writing at high volume](#writing-at-high-volume)
- [MCAP recording](#mcap-recording)
- [Profiling](#profiling)
- [Development](#development)

## Installation

Each release attaches the npm package to its GitHub release:

```bash
npm install https://github.com/the-protobuf-project/telemetry/releases/download/v<version>/the-protobuf-project-telemetry-<version>.tgz
```

**Requirements:** Node.js 22.15 or later (the OpenTelemetry JS packages'
floor), or any browser with `fetch`. The package is ESM only.

## Quick start

```ts
import { Telemetry } from "@the-protobuf-project/telemetry";

// Reads telemetry.toml, then .env and TELEMETRY_* variables, then this code.
const tel = Telemetry.builder("orders", "1.0.0")
  .withOtlpEndpoint("localhost:4317")
  .build();

tel.logger.info("service started", { port: 8080 });
tel.metrics.counter("requests_total", 1, { route: "/orders" });

await tel.tracing.withSpan("orders.create", async (span) => {
  span.setAttribute("order.id", "o-42");
  tel.logger.info("order created"); // carries the span's trace id
});

await tel.close(); // flushes, and reports anything that never arrived
```

`await using tel = Telemetry.builder(...).build()` closes it at the end of the
block.

## Node and the browser

One package, two entry points, picked by your runtime or bundler through the
`node` and `browser` export conditions. Import `@the-protobuf-project/telemetry/node`
or `/browser` to choose one explicitly.

| | Node | Browser |
| --- | --- | --- |
| Config | `telemetry.toml` / `.yaml` / `.json`, `.env`, `TELEMETRY_*` | code and `withConfigObject` |
| OTLP | gRPC (default) or HTTP | HTTP; port 4317 becomes 4318 |
| Active span follows `await` | yes (AsyncLocalStorage) | synchronous code only; use `logger.withContext(span.context)` |
| Console | stderr, colored on a terminal | `console.*` |
| MCAP recording | yes | no |
| Profiling | Pyroscope (`@pyroscope/nodejs`) | no |
| Flush | on `close()` | on `close()`, and when the page is hidden |

The browser bundle contains no Node code; CI checks it on every change. In
the browser the collector must allow your origin (CORS), and an auth token
in front-end code is public, so authenticate at a gateway instead.

## Configuration

The keys are the same as every SDK's; see the
[Configuration Guide](../docs/configuration.md).

```toml
[service]
name = "orders"
version = "1.0.0"
environment = "production"   # development | staging | production | jetson

[telemetry.otlp]
endpoint = "otel.example.com" # port added: 4317 for gRPC, 4318 for HTTP
auth_token = "token"
use_http = false

[telemetry.export]
lossless = true
exporters = 4

[logging]
level = 2                     # 1 errors, 2 info, 3 debug

[logging.modules.orders]
level = 3                     # this service, whatever the code says
```

Precedence, lowest to highest: defaults, the file, `.env` and `TELEMETRY_*`
variables, the builder. Nested keys are written with single or double
underscores: `TELEMETRY_TELEMETRY_OTLP_ENDPOINT` and
`TELEMETRY_TELEMETRY__OTLP__ENDPOINT` both work. `withService(name, version)`
ignores the file's `[service]` section, as the other SDKs do.

The builder methods: `withConfig(path)`, `withConfigObject(tree)`,
`withService`, `description`, `environment`, `withLabel(s)`, `withOtlp(host,
port)`, `withOtlpEndpoint`, `withOtlpAuth`, `withOtlpHeaders`,
`withOtlpSecure`, `withOtlpHttp`, `withExport`, `withConsole`, `withMcap`,
`withProfiling`, `withLogLevel`, `withTracing`. `resolve()` returns the
options without building.

## Logging

`debug`, `info`, `warn`, `error` and `fatal` take a message and optional data.
Records go to the console, OTLP and MCAP, with the caller's file and line.
`fatal` does not end the process.

A service's level is its `[logging.modules.<name>]` entry if there is one,
else `withLogLevel`, else `logging.level`, else the environment's default
(debug in development and on Jetson, info elsewhere).

## Tracing

```ts
import { SpanKind } from "@the-protobuf-project/telemetry";

// Server: continue the caller's trace from its headers.
const parent = tel.tracing.extract(req.headers);
await tel.tracing.withSpan("inventory.reserve", async () => {
  // Client: hand the trace on.
  await fetch(url, { headers: tel.tracing.inject() });
}, { parent, kind: SpanKind.SERVER });
```

`inject` writes W3C `traceparent`, `tracestate` and `baggage`, which the
other SDKs read, and `extract` reads them in any letter case.
`setBaggage(key, value)` returns a context carrying it, and `baggage(ctx)`
reads it. `startSpan` returns a span you end yourself. `withSpan` ends it when
the function returns or its promise settles, and records a throw as the
span's error.

## Metrics

```ts
tel.metrics.counter("jobs_total");                          // +1
tel.metrics.histogram("job_seconds", 1.7, { queue: "a" }, [0.5, 1, 5]);
tel.metrics.gauge("queue_depth", 12);
```

Service labels go on every point. Metrics are aggregated in the process and
pushed every `export_interval_seconds` (10).

## Writing at high volume

The OpenTelemetry batch processors drop what overflows their queue without
saying so. Here every log record and span is counted, and `close()` warns
about any that never arrived:

```text
TelemetryWarning: telemetry: 935442 of 1000000 log records dropped (export queue full); use withExport({ lossless: true }) or a larger queueSize to keep them
```

```ts
const tel = Telemetry.builder("ingest", "1.0.0")
  .withExport({ lossless: true, exporters: 4 }) // queue 65536, batches of 8192
  .build();

for (const row of rows) {
  tel.logger.info("row", row);
  if (tel.stats().logs.pending > 50_000) await tel.ready();
}
```

JavaScript cannot make the writer wait for room without stalling the
exporters, which run on the same thread. So a lossless queue grows instead of
dropping, and `await tel.ready()` waits until every queue has room again.
`exporters` is how many batches each signal has in flight at once.
`tel.stats()` gives emitted, exported, dropped, failed and pending per
signal. `OTEL_BSP_*` and `OTEL_BLRP_*` fill in queue settings the config
leaves at their defaults.

## MCAP recording

```ts
Telemetry.builder("robot", "1.0.0").withMcap("run.mcap").build();
```

Logs on `/logs`, metrics on `/metrics` and spans on `/traces`, under the same
JSON schemas the other SDKs write. MCAP works without a collector.

## Profiling

```bash
npm install @pyroscope/nodejs
```

```ts
Telemetry.builder("orders", "1.0.0").withProfiling("http://localhost:4040").build();
```

CPU and heap profiles go to Pyroscope, tagged `service`, `version` and
`environment` like the other SDKs' profiles.

## Development

```bash
npm ci
npm test               # vitest, against an in-process OTLP collector
npm run typecheck
npm run lint           # Biome
npm run build && npm run check:browser
```

`just crosslang` from the repository root passes one trace through Go, Rust,
Python, C++ and this SDK (`examples/crosslang.ts`).
