# Telemetry examples (standalone crate)

Runs the same scenarios as `telemetry-go/examples/`, as a **separate package** in the workspace.

## OpenTelemetry collector (port **6009**)

Examples that export OTLP use **gRPC** to `localhost:6009` (see `DEFAULT_OTEL_COLLECTOR_OTLP_PORT` in the `telemetry` crate).

Configure your collector to listen for OTLP gRPC on **6009**, for example:

```yaml
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:6009
```

Or run a collector container with port mapping `6009:4317` and point Telemetry at `localhost:6009`.

## Run

From `telemetry-rs/`:

```bash
cargo run -p telemetry-examples --bin logging
cargo run -p telemetry-examples --bin logging_mcap
cargo run -p telemetry-examples --bin metrics    # OTLP metrics → :6009
cargo run -p telemetry-examples --bin tracing   # OTLP traces → :6009
cargo run -p telemetry-examples --bin module_levels
```

## Ergonomics

- **`telemetry_local_otel!()`** — builder preset for `localhost:6009`
- **`TelemetryBuilder::with_local_otel_collector()`** — same
- **`Telemetry` `Drop`** — calls `close()` → flush + OTLP shutdown (safe to ignore explicit `close()`)
