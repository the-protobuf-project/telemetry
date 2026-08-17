# Telemetry Observability Framework - AI Agent Guidelines

## Project Overview

**Telemetry** is a unified observability framework providing multi-language SDKs
(Go, Rust, Python) for OpenTelemetry-based logging, metrics, tracing, and
profiling. Built by Machani Robotics for production robotics systems with
MCAP recording for offline analysis.

## Architecture Patterns

### 1. Multi-Language SDK Structure

- **Primary SDK**: Go (`/go/`) - Complete implementation with all telemetry features
- **Secondary SDKs**: Rust (`/rust/`) and Python (`/python/`) - Following same patterns
- **Shared Stack**: OpenTelemetry observability stack (`/telemetry/`)

### 2. Core Components Architecture

```text
telemetry.New() → Telemetry struct with:
├── Logger (*logging.Logger)           # Structured logging with trace correlation
├── Metrics (*metrics.Metrics)         # OTel metrics
├── Tracing (*tracing.Tracing)         # Distributed tracing spans
├── Profiler (*profiling.Profiler)     # Pyroscope continuous profiling
├── telemetry (*telemetry.Telemetry)   # Unified OpenTelemetry client
└── unifiedMcap (*foxglove.UnifiedMcapWriter) # MCAP recording for Foxglove
```

### 3. Configuration System

- **ServiceOptions**: Service identity (name, version, environment)
- **TelemetryOptions**: Feature toggles and endpoint configuration
- **Environment constants**: `Development`, `Staging`, `Production`, `Jetson`

## Development Workflows

### Building & Testing

```bash
# Go SDK development
cd go/
go mod tidy
go run examples/logging/main.go

# Full observability stack
cd telemetry/
docker compose up -d
# Access Grafana: http://localhost:3000
```

### Key Dependencies

- **OpenTelemetry**: Core telemetry (logs, metrics, traces)
- **Pyroscope**: Continuous profiling
- **MCAP/Foxglove**: Robotics data recording and visualization
- **Charmbracelet/log**: Enhanced logging interface

## Code Patterns

### 1. SDK Initialization Pattern

```go
// Standard service setup
serviceOpts := options.ServiceOptions{
    Name: "service-name",
    Environment: options.Development,
}
p, err := telemetry.New(ctx, serviceOpts, options.TelemetryOptions{
    Telemetry: options.DefaultTelemetry(),
})
defer p.Close(ctx)
```

### 2. Structured Logging with Attributes

Use struct tags `telemetry:"attribute:key.name"` for automatic OpenTelemetry
attribute extraction:

```go
type ChatMessage struct {
    UserID   string `json:"user_id" telemetry:"attribute:user.id"`
    RoomID   string `json:"room_id" telemetry:"attribute:room.id"`
    Language string `json:"language" telemetry:"attribute:message.language"`
}
```

### 3. Unified Telemetry Access

- `p.telemetry.GetLogger()` - OpenTelemetry logger
- `p.telemetry.GetMetrics()` - OpenTelemetry meter
- `p.telemetry.GetTracer()` - OpenTelemetry tracer

## Critical Integration Points

### 1. MCAP Recording (Robotics Focus)

- Single unified writer for all telemetry data
- Foxglove Studio integration for offline analysis
- Schema registry for multiple data types

### 2. OTLP Configuration

Default endpoints:

- **gRPC**: `localhost:4317`
- **HTTP**: `localhost:4318`
- Production: Configure `options.OpenTelemetryOptions.OTLP`

### 3. Multi-Environment Support

- `Jetson` environment for embedded robotics systems
- Environment-specific telemetry routing and sampling

## File Organization Rules

### Go SDK Structure (`/go/`)

- `telemetry.go`: Main SDK interface
- `options/`: Configuration structs and defaults
- `internal/`: Implementation packages (logging, metrics, tracing, telemetry,
  profiling, foxglove)
- `examples/`: Runnable examples for each telemetry type

### Internal Package Boundaries

- Never import `internal/` packages directly in user code
- Use public interfaces through main `Telemetry` struct
- Cross-cutting concerns handled in `internal/telemetry/`

## Environment-Specific Conventions

### Local Development

- Use `options.Development` environment
- Enable all telemetry features for testing
- Run observability stack via Docker Compose

### Production Robotics

- `options.Jetson` for embedded systems
- MCAP recording for mission-critical data capture
- Reduced telemetry overhead with selective sampling

## Common Gotchas

1. **Shutdown Order**: Always stop Profiler → MCAP → Telemetry to ensure data flush
2. **MCAP Path**: Must specify absolute path in `FoxgloveOptions.McapPath`
3. **Attribute Extraction**: Struct tags only work with supported field types
4. **Context Propagation**: Pass context through telemetry calls for trace correlation

## Adding New Features

When extending Telemetry:

1. Add options in `options/` package first
2. Implement in appropriate `internal/` package
3. Expose through main `Telemetry` struct
4. Add example in `examples/`
5. Update unified telemetry integration if needed

Focus on maintaining the unified telemetry approach while supporting the
diverse needs of robotics applications.
