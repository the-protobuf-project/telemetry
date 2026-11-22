/**
 * What differs between Node and the browser, behind one interface: where
 * config comes from, how context follows async work, which OTLP transports
 * exist, where console output and warnings go, and the Node-only MCAP
 * recording and profiling. The core never imports a platform module itself,
 * so the browser bundle carries no Node code.
 */

import type { ContextManager, MeterProvider, TracerProvider } from "@opentelemetry/api";
import type { LogRecordExporter } from "@opentelemetry/sdk-logs";
import type { PushMetricExporter } from "@opentelemetry/sdk-metrics";
import type { SpanExporter } from "@opentelemetry/sdk-trace";
import type {
  ConfigTree,
  OtlpOptions,
  ProfilingOptions,
  ServiceOptions,
  TelemetryOptions,
  TracingOptions,
} from "./options.ts";

export type LevelName = "DEBUG" | "INFO" | "WARN" | "ERROR" | "FATAL";

/** One log line for the console. */
export interface ConsoleRecord {
  time: Date;
  level: LevelName;
  service: ServiceOptions;
  message: string;
  data: Record<string, unknown> | undefined;
  caller: Caller | undefined;
}

export interface Caller {
  file: string;
  line: number;
}

/** A recording of every signal, for offline analysis (MCAP in Node). */
export interface Recorder {
  log(entry: {
    timeNanos: bigint;
    level: LevelName;
    message: string;
    data: Record<string, unknown> | undefined;
    caller: Caller | undefined;
  }): void;
  metric(name: string, value: number, timeNanos: bigint): void;
  span(entry: {
    timeNanos: bigint;
    traceId: string;
    spanId: string;
    parentSpanId: string;
    name: string;
    attributes: Record<string, unknown>;
  }): void;
  close(): Promise<void>;
}

/** The OTLP exporters for one endpoint; each call makes a new exporter. */
export interface Exporters {
  span(): SpanExporter;
  log(): LogRecordExporter;
  metric(): PushMetricExporter;
}

/** What automatic instrumentation needs to trace and measure requests. */
export interface InstrumentArgs {
  tracerProvider: TracerProvider;
  meterProvider: MeterProvider | undefined;
  tracing: TracingOptions;
  /** The OTLP endpoint, when exporting: its own requests are never traced. */
  otlp: OtlpOptions | undefined;
}

export interface Platform {
  readonly name: "node" | "browser";
  contextManager(): ContextManager;
  /** The environment variables, or nothing where there are none. */
  env(): Record<string, string | undefined>;
  /** Config trees to apply over the defaults, lowest precedence first. */
  loadConfig(path: string | undefined): ConfigTree[];
  exporters(otlp: OtlpOptions, timeoutMillis: number): Exporters;
  console(record: ConsoleRecord): void;
  /** The program's file and line that called the logger. */
  caller(): Caller | undefined;
  recorder(path: string, service: ServiceOptions): Recorder;
  /** Starts continuous profiling; resolves the function that stops it. */
  startProfiling(service: ServiceOptions, opts: ProfilingOptions): () => Promise<void>;
  warn(message: string): void;
  /**
   * Hooks the runtime's HTTP, fetch and gRPC clients and servers so a
   * trace crosses every request; returns the unhook.
   */
  instrument(args: InstrumentArgs): () => void;
  /** Hooks into the runtime's lifecycle, e.g. flushing when a page is hidden; returns the unhook. */
  attach?(flush: () => Promise<void>, opts: TelemetryOptions): () => void;
}

/**
 * The URL for one signal at an OTLP endpoint. The endpoint may be `host`,
 * `host:port` or a full URL; a missing port is 4317 for gRPC and 4318 for
 * HTTP (443 with TLS), and HTTP adds the signal's `/v1/...` path.
 */
export function otlpUrl(otlp: OtlpOptions, http: boolean, path?: string): string {
  let endpoint = otlp.endpoint.trim();
  let secure = otlp.secure;
  const scheme = /^(https?):\/\//i.exec(endpoint);
  if (scheme) {
    secure = scheme[1]?.toLowerCase() === "https";
    endpoint = endpoint.slice(scheme[0].length);
  }
  const slash = endpoint.indexOf("/");
  let base = slash < 0 ? endpoint : endpoint.slice(0, slash);
  const prefix = slash < 0 ? "" : endpoint.slice(slash).replace(/\/+$/, "");
  if (!/:\d+$/.test(base) || (base.startsWith("[") && !/\]:\d+$/.test(base))) {
    base += `:${secure ? 443 : http ? 4318 : 4317}`;
  }
  return `${secure ? "https" : "http"}://${base}${http ? `${prefix}${path ?? ""}` : ""}`;
}

/** The OTLP request headers: the extras, and the bearer token. */
export function otlpHeaders(otlp: OtlpOptions): Record<string, string> {
  const headers = { ...otlp.headers };
  if (otlp.authToken) headers.authorization = `Bearer ${otlp.authToken}`;
  return headers;
}
