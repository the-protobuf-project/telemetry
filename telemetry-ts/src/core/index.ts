// What both entry points export besides Telemetry itself.

export { context, propagation, SpanStatusCode, trace } from "@opentelemetry/api";
export type { SignalStats, Stats } from "./accounting.ts";
export { Logger } from "./logger.ts";
export { type Labels, Metrics } from "./metrics.ts";
export {
  applyConfig,
  type ConfigTree,
  defaultOptions,
  Environment,
  type ExportOptions,
  envConfig,
  type FoxgloveOptions,
  type LoggingOptions,
  LogLevel,
  type MetricsOptions,
  type OtlpOptions,
  type ProfilingOptions,
  type ServiceOptions,
  type TelemetryOptions,
  type TracingOptions,
} from "./options.ts";
export { Span, SpanKind, type SpanOptions, Tracing } from "./tracing.ts";
