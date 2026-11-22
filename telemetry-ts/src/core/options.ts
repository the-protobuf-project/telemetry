/**
 * Configuration for the Telemetry SDK: the same keys, defaults and precedence
 * as the Go, Rust, Python and C++ SDKs, so one telemetry.toml drives them all.
 *
 * Precedence, lowest to highest: defaults, the config file, `.env` and
 * `TELEMETRY_*` environment variables, then the builder.
 */

import type { Instrumentation } from "@opentelemetry/instrumentation";

/** Deployment environment; sets the default log level. */
export const Environment = {
  Development: "development",
  Staging: "staging",
  Production: "production",
  Jetson: "jetson",
} as const;
export type Environment = (typeof Environment)[keyof typeof Environment];

/**
 * Verbosity of a service (a "module"), as in the other SDKs: 1 is errors
 * only, 2 adds info and warnings, 3 adds debug. Unset falls back to the
 * environment: debug in development and on Jetson, info elsewhere.
 */
export const LogLevel = {
  Unset: 0,
  Level1: 1,
  Level2: 2,
  Level3: 3,
} as const;
export type LogLevel = (typeof LogLevel)[keyof typeof LogLevel];

export interface ServiceOptions {
  name: string;
  version: string;
  description: string;
  environment: Environment;
  /** Added to every log record, metric and span. */
  labels: Record<string, string>;
}

/**
 * How log records and spans queue for OTLP export.
 *
 * By default a full queue drops what does not fit, so telemetry never holds
 * the program up; every drop is counted (`Telemetry.stats()`) and reported by
 * `Telemetry.close()`. With `lossless` the queue grows instead and nothing is
 * dropped; `await telemetry.ready()` waits until every queue has room again.
 */
export interface ExportOptions {
  lossless: boolean;
  /** Records each signal may hold awaiting export. */
  queueSize: number;
  /** Records sent per export request. */
  batchSize: number;
  /** Export requests each signal may have in flight at once. */
  exporters: number;
  /** Longest one export request may take, in milliseconds. */
  timeoutMillis: number;
  /** Longest a record waits for its batch to fill, in milliseconds; 0 picks 1s for logs, 5s for spans. */
  intervalMillis: number;
}

export interface OtlpOptions {
  enabled: boolean;
  /** `host`, `host:port` or a URL; the port defaults to 4317 for gRPC and 4318 for HTTP. */
  endpoint: string;
  /** Sent as `authorization: Bearer <token>`. */
  authToken: string;
  secure: boolean;
  /** OTLP over HTTP instead of gRPC; always on in the browser. */
  useHttp: boolean;
  headers: Record<string, string>;
}

export interface LoggingOptions {
  enabled: boolean;
  /** Global level; overrides the environment's default. */
  level: LogLevel;
  /** Per-service overrides, keyed by service name; they win over everything else. */
  modules: Record<string, LogLevel>;
  /** Print each record to the console (stderr in Node). Off, records still go to OTLP and MCAP. */
  console: boolean;
  /** Include the caller's file and line. */
  reportCaller: boolean;
}

export interface MetricsOptions {
  enabled: boolean;
  exportIntervalSeconds: number;
}

export interface TracingOptions {
  enabled: boolean;
  /**
   * Hooks the HTTP, fetch and gRPC clients and servers, so every request
   * carries the trace on (W3C `traceparent`) and every incoming one
   * continues it: one trace end to end, across services and languages.
   */
  autoInstrument: boolean;
  /**
   * Browser only: the other origins (URL prefixes) that `fetch` and XHR
   * send the trace headers to. Same-origin requests always carry them;
   * another origin must allow the headers (CORS), so it is opt-in.
   */
  propagateTo: string[];
  /** Instrumentations the program adds to the automatic ones (set in code). */
  instrumentations: Instrumentation[];
}

export interface FoxgloveOptions {
  enabled: boolean;
  /** Where the MCAP recording is written (Node only). */
  filePath: string;
}

export interface ProfilingOptions {
  enabled: boolean;
  serverAddress: string;
  basicAuthUser: string;
  basicAuthPassword: string;
  tenantId: string;
  tags: Record<string, string>;
}

export interface TelemetryOptions {
  service: ServiceOptions;
  otlp: OtlpOptions;
  export: ExportOptions;
  logging: LoggingOptions;
  metrics: MetricsOptions;
  tracing: TracingOptions;
  foxglove: FoxgloveOptions;
  profiling: ProfilingOptions;
}

export function defaultOptions(): TelemetryOptions {
  return {
    service: {
      name: "unnamed-service",
      version: "1.0.0",
      description: "",
      environment: Environment.Development,
      labels: {},
    },
    otlp: {
      enabled: false,
      endpoint: "localhost:4317",
      authToken: "",
      secure: false,
      useHttp: false,
      headers: {},
    },
    export: defaultExport(false),
    logging: {
      enabled: true,
      level: LogLevel.Unset,
      modules: {},
      console: true,
      reportCaller: true,
    },
    metrics: { enabled: true, exportIntervalSeconds: 10 },
    tracing: { enabled: true, autoInstrument: true, propagateTo: [], instrumentations: [] },
    foxglove: { enabled: false, filePath: "" },
    profiling: {
      enabled: false,
      serverAddress: "http://localhost:4040",
      basicAuthUser: "",
      basicAuthPassword: "",
      tenantId: "",
      tags: {},
    },
  };
}

/** The export defaults; lossless queues are larger, as in the other SDKs. */
export function defaultExport(lossless: boolean): ExportOptions {
  return {
    lossless,
    queueSize: lossless ? 65536 : 2048,
    batchSize: lossless ? 8192 : 512,
    exporters: 1,
    timeoutMillis: 30_000,
    intervalMillis: 0,
  };
}

/** A raw config tree: parsed TOML, YAML or JSON, or environment variables. */
export type ConfigTree = { [key: string]: unknown };

const environments = new Set<string>(Object.values(Environment));

/**
 * Applies a config tree (the shape of telemetry.toml) onto `opts`. Keys it
 * does not know are ignored, so one file can carry settings for every SDK.
 */
export function applyConfig(opts: TelemetryOptions, tree: ConfigTree): TelemetryOptions {
  const service = section(tree, "service");
  setString(service, "name", (v) => (opts.service.name = v));
  setString(service, "version", (v) => (opts.service.version = v));
  setString(service, "description", (v) => (opts.service.description = v));
  setString(service, "environment", (v) => {
    const env = v.toLowerCase();
    if (environments.has(env)) opts.service.environment = env as Environment;
  });
  Object.assign(opts.service.labels, stringMap(service.labels));

  const telemetry = section(tree, "telemetry");
  const master = bool(telemetry.enabled);
  if (master === false) {
    opts.logging.enabled = false;
    opts.metrics.enabled = false;
    opts.tracing.enabled = false;
  }

  const otlp = section(telemetry, "otlp");
  setString(otlp, "endpoint", (v) => (opts.otlp.endpoint = v));
  setString(otlp, "auth_token", (v) => (opts.otlp.authToken = v));
  setBool(otlp, "secure", (v) => (opts.otlp.secure = v));
  setBool(otlp, "use_http", (v) => (opts.otlp.useHttp = v));
  Object.assign(opts.otlp.headers, stringMap(otlp.headers));
  const enabled = bool(otlp.enabled);
  if (enabled !== undefined) {
    opts.otlp.enabled = enabled;
  } else if (typeof otlp.endpoint === "string" && otlp.endpoint !== "") {
    // An endpoint other than the local default turns export on by itself.
    opts.otlp.enabled = !/localhost|127\.0\.0\.1/.test(otlp.endpoint);
  }

  const exp = section(telemetry, "export");
  const lossless = bool(exp.lossless);
  if (lossless !== undefined && lossless !== opts.export.lossless) {
    // Lossless changes the queue defaults; sizes given alongside still win.
    opts.export = { ...defaultExport(lossless), exporters: opts.export.exporters };
  }
  setNumber(exp, "queue_size", (v) => (opts.export.queueSize = v));
  setNumber(exp, "batch_size", (v) => (opts.export.batchSize = v));
  setNumber(exp, "exporters", (v) => (opts.export.exporters = Math.max(1, v)));
  setDuration(exp, "timeout", (v) => (opts.export.timeoutMillis = v));
  setDuration(exp, "interval", (v) => (opts.export.intervalMillis = v));

  const metrics = section(telemetry, "metrics");
  setBool(metrics, "enabled", (v) => (opts.metrics.enabled = v));
  setNumber(metrics, "export_interval_seconds", (v) => (opts.metrics.exportIntervalSeconds = v));

  const logging = section(tree, "logging");
  setNumber(logging, "level", (v) => (opts.logging.level = logLevel(v)));
  setBool(logging, "console", (v) => (opts.logging.console = v));
  setBool(section(logging, "log"), "report_caller", (v) => (opts.logging.reportCaller = v));
  for (const [name, module] of Object.entries(section(logging, "modules"))) {
    if (isTree(module)) {
      setNumber(module, "level", (v) => (opts.logging.modules[name] = logLevel(v)));
    }
  }

  const tracing = section(tree, "tracing");
  setBool(tracing, "enabled", (v) => (opts.tracing.enabled = v));
  setBool(tracing, "auto_instrument", (v) => (opts.tracing.autoInstrument = v));
  const propagateTo = strings(tracing.propagate_to);
  if (propagateTo) opts.tracing.propagateTo = propagateTo;

  const foxglove = section(tree, "foxglove");
  setBool(foxglove, "enabled", (v) => (opts.foxglove.enabled = v));
  setString(foxglove, "file_path", (v) => (opts.foxglove.filePath = v));

  const profiling = section(tree, "profiling");
  setBool(profiling, "enabled", (v) => (opts.profiling.enabled = v));
  setString(profiling, "server_address", (v) => (opts.profiling.serverAddress = v));
  setString(profiling, "basic_auth_user", (v) => (opts.profiling.basicAuthUser = v));
  setString(profiling, "basic_auth_password", (v) => (opts.profiling.basicAuthPassword = v));
  setString(profiling, "tenant_id", (v) => (opts.profiling.tenantId = v));
  Object.assign(opts.profiling.tags, stringMap(profiling.tags));

  return opts;
}

/**
 * Every leaf key the SDK reads, dotted. Environment variables are matched
 * against these, so `TELEMETRY_TELEMETRY_OTLP_AUTH_TOKEN` (single
 * underscores, as Go, Rust and C++ spell it) is unambiguous.
 */
const knownKeys = [
  "service.name",
  "service.version",
  "service.description",
  "service.environment",
  "telemetry.enabled",
  "telemetry.otlp.enabled",
  "telemetry.otlp.endpoint",
  "telemetry.otlp.auth_token",
  "telemetry.otlp.secure",
  "telemetry.otlp.use_http",
  "telemetry.export.lossless",
  "telemetry.export.queue_size",
  "telemetry.export.batch_size",
  "telemetry.export.exporters",
  "telemetry.export.timeout",
  "telemetry.export.interval",
  "telemetry.metrics.enabled",
  "telemetry.metrics.export_interval_seconds",
  "logging.level",
  "logging.console",
  "logging.log.report_caller",
  "tracing.enabled",
  "tracing.auto_instrument",
  "tracing.propagate_to",
  "foxglove.enabled",
  "foxglove.file_path",
  "profiling.enabled",
  "profiling.server_address",
  "profiling.basic_auth_user",
  "profiling.basic_auth_password",
  "profiling.tenant_id",
];

const envPrefix = "TELEMETRY_";

/**
 * Builds a config tree from `TELEMETRY_*` variables. Nesting may be written
 * with single underscores (Go, Rust, C++) or double ones (Python):
 * `TELEMETRY_SERVICE_NAME` and `TELEMETRY_SERVICE__NAME` both set
 * `service.name`. Per-service levels are `TELEMETRY_LOGGING_MODULES_<NAME>_LEVEL`.
 */
export function envConfig(env: Record<string, string | undefined>): ConfigTree {
  const tree: ConfigTree = {};
  for (const key of knownKeys) {
    const parts = key.toUpperCase().split(".");
    const value =
      env[envPrefix + parts.join("__")] ?? env[envPrefix + parts.join("_")] ?? undefined;
    if (value !== undefined) setPath(tree, key.split("."), value);
  }
  const module = /^TELEMETRY_LOGGING__?MODULES__?(.+?)__?LEVEL$/;
  for (const [name, value] of Object.entries(env)) {
    const match = module.exec(name);
    if (match?.[1] && value !== undefined) {
      setPath(tree, ["logging", "modules", match[1].toLowerCase(), "level"], value);
    }
  }
  return tree;
}

/**
 * The OpenTelemetry batch variables, as the other SDKs read them: OTEL_BSP_*
 * for spans and OTEL_BLRP_* for log records, under what the config sets.
 */
export function signalExport(
  base: ExportOptions,
  env: Record<string, string | undefined>,
  signal: "spans" | "logs",
): ExportOptions {
  const prefix = signal === "spans" ? "OTEL_BSP_" : "OTEL_BLRP_";
  const read = (name: string) => {
    const n = Number(env[prefix + name]);
    return Number.isFinite(n) && n > 0 ? n : undefined;
  };
  const defaults = defaultExport(base.lossless);
  const pick = (configured: number, fallback: number, name: string) =>
    configured !== fallback ? configured : (read(name) ?? configured);
  return {
    ...base,
    queueSize: pick(base.queueSize, defaults.queueSize, "MAX_QUEUE_SIZE"),
    batchSize: pick(base.batchSize, defaults.batchSize, "MAX_EXPORT_BATCH_SIZE"),
    timeoutMillis: pick(base.timeoutMillis, defaults.timeoutMillis, "EXPORT_TIMEOUT"),
    intervalMillis:
      base.intervalMillis || read("SCHEDULE_DELAY") || (signal === "logs" ? 1000 : 5000),
  };
}

function section(tree: ConfigTree, key: string): ConfigTree {
  const v = tree[key];
  return isTree(v) ? v : {};
}

function isTree(v: unknown): v is ConfigTree {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function setPath(tree: ConfigTree, path: string[], value: unknown): void {
  let node = tree;
  for (const part of path.slice(0, -1)) {
    let next = node[part];
    if (!isTree(next)) {
      next = {};
      node[part] = next;
    }
    node = next as ConfigTree;
  }
  node[path[path.length - 1] as string] = value;
}

function bool(v: unknown): boolean | undefined {
  if (typeof v === "boolean") return v;
  if (typeof v === "string") {
    const s = v.trim().toLowerCase();
    if (["true", "1", "yes", "on"].includes(s)) return true;
    if (["false", "0", "no", "off"].includes(s)) return false;
  }
  return undefined;
}

function setBool(tree: ConfigTree, key: string, set: (v: boolean) => void): void {
  const v = bool(tree[key]);
  if (v !== undefined) set(v);
}

function setString(tree: ConfigTree, key: string, set: (v: string) => void): void {
  const v = tree[key];
  if (typeof v === "string") set(v);
  else if (typeof v === "number" || typeof v === "boolean") set(String(v));
}

function setNumber(tree: ConfigTree, key: string, set: (v: number) => void): void {
  const v = typeof tree[key] === "string" ? Number(tree[key]) : tree[key];
  if (typeof v === "number" && Number.isFinite(v)) set(v);
}

/** A duration in milliseconds: a number of seconds, or a string like "500ms", "30s" or "1m". */
function setDuration(tree: ConfigTree, key: string, set: (v: number) => void): void {
  const v = tree[key];
  if (typeof v === "number" && Number.isFinite(v)) {
    set(v * 1000);
    return;
  }
  if (typeof v !== "string") return;
  const match = /^\s*(\d+(?:\.\d+)?)\s*(ms|s|m)?\s*$/.exec(v);
  if (!match) return;
  const n = Number(match[1]);
  set(match[2] === "ms" ? n : match[2] === "m" ? n * 60_000 : n * 1000);
}

function stringMap(v: unknown): Record<string, string> {
  if (!isTree(v)) return {};
  return Object.fromEntries(Object.entries(v).map(([k, x]) => [k, String(x)]));
}

/** A list of strings, or one comma-separated string as an environment variable gives it. */
function strings(v: unknown): string[] | undefined {
  const items = Array.isArray(v) ? v : typeof v === "string" ? v.split(",") : undefined;
  return items?.map((x) => String(x).trim()).filter((x) => x !== "");
}

function logLevel(n: number): LogLevel {
  return n >= 1 && n <= 3 ? (n as LogLevel) : LogLevel.Unset;
}
