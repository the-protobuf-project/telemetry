/**
 * The Telemetry instance and its builder: one call sets up logging, metrics,
 * tracing, profiling and MCAP recording from telemetry.toml, the environment
 * and code, in that order of precedence.
 */

import {
  context,
  logs,
  type Meter,
  metrics,
  propagation,
  type TextMapPropagator,
  trace,
} from "@opentelemetry/api";
import {
  CompositePropagator,
  W3CBaggagePropagator,
  W3CTraceContextPropagator,
} from "@opentelemetry/core";
import { type Resource, resourceFromAttributes } from "@opentelemetry/resources";
import { LoggerProvider, type ReadWriteLogRecord } from "@opentelemetry/sdk-logs";
import { MeterProvider, PeriodicExportingMetricReader } from "@opentelemetry/sdk-metrics";
import {
  AlwaysOffSampler,
  type ReadableSpan,
  type SpanProcessor,
  TracerProvider,
} from "@opentelemetry/sdk-trace";
import { Counters, ExportErrors, lossReport, type Stats } from "./accounting.ts";
import { BatchedLogProcessor, BatchedSpanProcessor, Batcher } from "./batcher.ts";
import { Logger, resolveLevel } from "./logger.ts";
import { Metrics } from "./metrics.ts";
import {
  applyConfig,
  type ConfigTree,
  defaultExport,
  defaultOptions,
  type Environment,
  type ExportOptions,
  LogLevel,
  signalExport,
  type TelemetryOptions,
} from "./options.ts";
import type { Platform, Recorder } from "./platform.ts";
import { RecordingSpanProcessor, Tracing } from "./tracing.ts";

const scope = "@the-protobuf-project/telemetry";

// The OpenTelemetry API's globals are process-wide and take the first
// registration only; the first Telemetry built claims them, and gives them
// up when it closes.
let globalOwner: Telemetry | undefined;

export class Telemetry {
  readonly options: TelemetryOptions;
  readonly logger: Logger;
  readonly metrics: Metrics;
  readonly tracing: Tracing;

  readonly #platform: Platform;
  readonly #tracerProvider: TracerProvider;
  readonly #loggerProvider: LoggerProvider | undefined;
  readonly #meterProvider: MeterProvider | undefined;
  readonly #batchers: Batcher<unknown>[] = [];
  readonly #logCounters = new Counters();
  readonly #spanCounters = new Counters();
  readonly #errors = new ExportErrors();
  readonly #recorder: Recorder | undefined;
  readonly #detach: (() => void) | undefined;
  #stopProfiling: (() => Promise<void>) | undefined;
  #closing: Promise<Stats> | undefined;

  /** Use `Telemetry.new()` or `Telemetry.builder(name, version)`. */
  constructor(options: TelemetryOptions, platform: Platform, codeLevel: LogLevel = LogLevel.Unset) {
    this.options = options;
    this.#platform = platform;
    const { service, otlp } = options;
    const exporting = otlp.enabled;
    const exporters = exporting
      ? platform.exporters(otlp, options.export.timeoutMillis)
      : undefined;
    const env = platform.env();

    const resource: Resource = resourceFromAttributes({
      "service.name": service.name,
      "service.version": service.version,
      "service.environment": service.environment,
      "deployment.environment.name": service.environment,
      ...(service.description && { "service.description": service.description }),
      ...service.labels,
    });

    if (options.foxglove.enabled && options.foxglove.filePath) {
      this.#recorder = platform.recorder(options.foxglove.filePath, service);
    }

    // Tracing: spans are real whenever tracing is on, so ids propagate and
    // MCAP records them even without a collector.
    const spanProcessors: SpanProcessor[] = [];
    if (exporters && options.tracing.enabled) {
      const opts = signalExport(options.export, env, "spans");
      const batcher = new Batcher<ReadableSpan>(
        Array.from({ length: opts.exporters }, () => exporters.span()),
        opts,
        this.#spanCounters,
        this.#errors,
      );
      this.#batchers.push(batcher as Batcher<unknown>);
      spanProcessors.push(new BatchedSpanProcessor(batcher));
    }
    if (this.#recorder) spanProcessors.push(new RecordingSpanProcessor(this.#recorder));
    this.#tracerProvider = new TracerProvider({
      resource,
      spanProcessors,
      ...(!options.tracing.enabled && { sampler: new AlwaysOffSampler() }),
    });
    const propagator: TextMapPropagator = new CompositePropagator({
      propagators: [new W3CTraceContextPropagator(), new W3CBaggagePropagator()],
    });
    this.tracing = new Tracing(this.#tracerProvider.getTracer(scope, service.version), propagator);

    // Logs: OTLP only when exporting; console and MCAP regardless.
    if (exporters && options.logging.enabled) {
      const opts = signalExport(options.export, env, "logs");
      const batcher = new Batcher<ReadWriteLogRecord>(
        Array.from({ length: opts.exporters }, () => exporters.log()),
        opts,
        this.#logCounters,
        this.#errors,
      );
      this.#batchers.push(batcher as Batcher<unknown>);
      this.#loggerProvider = new LoggerProvider({
        resource,
        processors: [new BatchedLogProcessor(batcher)],
      });
    }
    this.logger = new Logger({
      service,
      level: resolveLevel(service, options.logging, codeLevel),
      console: options.logging.enabled && options.logging.console,
      reportCaller: options.logging.reportCaller,
      otel: this.#loggerProvider?.getLogger(scope, service.version),
      recorder: options.logging.enabled ? this.#recorder : undefined,
      platform,
    });

    // Metrics: aggregated in process and pushed on an interval, so there is
    // nothing to queue or drop.
    let meter: Meter | undefined;
    if (exporters && options.metrics.enabled) {
      this.#meterProvider = new MeterProvider({
        resource,
        readers: [
          new PeriodicExportingMetricReader({
            exporter: exporters.metric(),
            exportIntervalMillis: options.metrics.exportIntervalSeconds * 1000,
            exportTimeoutMillis: Math.min(
              options.export.timeoutMillis,
              options.metrics.exportIntervalSeconds * 1000,
            ),
          }),
        ],
      });
      meter = this.#meterProvider.getMeter(scope, service.version);
    }
    this.metrics = new Metrics(
      meter,
      options.metrics.enabled ? this.#recorder : undefined,
      service.labels,
    );

    if (!globalOwner) {
      globalOwner = this;
      context.setGlobalContextManager(platform.contextManager().enable());
      propagation.setGlobalPropagator(propagator);
      trace.setGlobalTracerProvider(this.#tracerProvider);
      if (this.#loggerProvider) logs.setGlobalLoggerProvider(this.#loggerProvider);
      if (this.#meterProvider) metrics.setGlobalMeterProvider(this.#meterProvider);
    }

    if (options.profiling.enabled) {
      this.#stopProfiling = platform.startProfiling(service, options.profiling);
    }
    this.#detach = platform.attach?.(() => this.flush(), options);
  }

  /** Counts of what the export pipeline did with this instance's log records and spans. */
  stats(): Stats {
    return {
      logs: this.#logCounters.stats(),
      spans: this.#spanCounters.stats(),
      exportErrors: this.#errors.count,
      lastError: this.#errors.last,
    };
  }

  /**
   * Resolves once every export queue has room. A lossless queue grows
   * rather than drop; a writer producing faster than the collector accepts
   * can await this to keep memory bounded.
   */
  async ready(): Promise<void> {
    await Promise.all(this.#batchers.map((b) => b.ready()));
  }

  /** Exports everything queued, metrics included, and waits for the answers. */
  async flush(): Promise<void> {
    await Promise.all([
      this.#tracerProvider.forceFlush(),
      this.#loggerProvider?.forceFlush(),
      this.#meterProvider?.forceFlush(),
    ]);
  }

  /**
   * Flushes and shuts everything down. Records that never reached the
   * collector are reported once, as a warning, and counted in the stats it
   * resolves. Calling it again returns the same result.
   */
  close(): Promise<Stats> {
    this.#closing ??= this.#close();
    return this.#closing;
  }

  async [Symbol.asyncDispose](): Promise<void> {
    await this.close();
  }

  async #close(): Promise<Stats> {
    this.#detach?.();
    await this.#stopProfiling?.();
    this.#stopProfiling = undefined;
    await Promise.allSettled([
      this.#tracerProvider.shutdown(),
      this.#loggerProvider?.shutdown(),
      this.#meterProvider?.shutdown(),
    ]);
    await this.#recorder?.close();
    if (globalOwner === this) {
      globalOwner = undefined;
      trace.disable();
      logs.disable();
      metrics.disable();
      propagation.disable();
      context.disable();
    }
    const stats = this.stats();
    const report = lossReport(stats.logs, stats.spans);
    if (report) this.#platform.warn(report);
    return stats;
  }
}

/**
 * Fluent configuration for a Telemetry instance. Settings made here win
 * over the config file and the environment.
 */
export class TelemetryBuilder {
  readonly #platform: Platform;
  readonly #create: (opts: TelemetryOptions, platform: Platform, level: LogLevel) => Telemetry;
  readonly #edits: Array<(opts: TelemetryOptions) => void> = [];
  readonly #trees: ConfigTree[] = [];
  #configPath: string | undefined;
  #serviceFromCode = false;
  #level: LogLevel = LogLevel.Unset;

  constructor(
    platform: Platform,
    create: (opts: TelemetryOptions, platform: Platform, level: LogLevel) => Telemetry,
  ) {
    this.#platform = platform;
    this.#create = create;
  }

  /** Reads this config file instead of discovering telemetry.toml (Node only). */
  withConfig(path: string): this {
    this.#configPath = path;
    return this;
  }

  /**
   * Applies a config tree in the shape of telemetry.toml, over the file and
   * the environment: how a browser app, which has neither, configures from data.
   */
  withConfigObject(tree: ConfigTree): this {
    this.#trees.push(tree);
    return this;
  }

  /**
   * Names the service. The service section of the config file is then
   * ignored, as in the other SDKs, so a shared file cannot rename it.
   */
  withService(name: string, version: string): this {
    this.#serviceFromCode = true;
    return this.#edit((o) => Object.assign(o.service, { name, version }));
  }

  description(description: string): this {
    return this.#edit((o) => (o.service.description = description));
  }

  environment(environment: Environment): this {
    return this.#edit((o) => (o.service.environment = environment));
  }

  /** Adds a label to every log record, metric and span. */
  withLabel(key: string, value: string): this {
    return this.#edit((o) => (o.service.labels[key] = value));
  }

  withLabels(labels: Record<string, string>): this {
    return this.#edit((o) => Object.assign(o.service.labels, labels));
  }

  /** Exports over OTLP to host:port. */
  withOtlp(host: string, port: number): this {
    return this.withOtlpEndpoint(`${host}:${port}`);
  }

  /** Exports over OTLP to `host`, `host:port` or a URL. */
  withOtlpEndpoint(endpoint: string): this {
    return this.#edit((o) => Object.assign(o.otlp, { endpoint, enabled: true }));
  }

  /** Sends `authorization: Bearer <token>` with every export. */
  withOtlpAuth(token: string): this {
    return this.#edit((o) => (o.otlp.authToken = token));
  }

  withOtlpHeaders(headers: Record<string, string>): this {
    return this.#edit((o) => Object.assign(o.otlp.headers, headers));
  }

  withOtlpSecure(secure = true): this {
    return this.#edit((o) => (o.otlp.secure = secure));
  }

  /** OTLP over HTTP instead of gRPC (the browser always uses HTTP). */
  withOtlpHttp(useHttp = true): this {
    return this.#edit((o) => (o.otlp.useHttp = useHttp));
  }

  /**
   * Tunes the export queues for logs and spans. Lossless keeps every record
   * and makes the queue larger (65536) unless `queueSize` says otherwise.
   */
  withExport(opts: Partial<ExportOptions>): this {
    return this.#edit((o) => {
      o.export = { ...defaultExport(opts.lossless ?? false), ...stripUndefined(opts) };
      o.export.exporters = Math.max(1, o.export.exporters);
    });
  }

  /** Prints each log line to the console, or not; on by default. */
  withConsole(console = true): this {
    return this.#edit((o) => (o.logging.console = console));
  }

  /** Records logs, metrics and spans to an MCAP file for Foxglove Studio (Node only). */
  withMcap(path: string): this {
    return this.#edit((o) => Object.assign(o.foxglove, { enabled: true, filePath: path }));
  }

  /** Pushes continuous CPU and heap profiles to Pyroscope (Node only; needs @pyroscope/nodejs). */
  withProfiling(serverAddress = "http://localhost:4040"): this {
    return this.#edit((o) => Object.assign(o.profiling, { enabled: true, serverAddress }));
  }

  /**
   * The service's log level in code. A `[logging.modules.<name>]` entry in
   * the config still overrides it, so operators can quiet a module.
   */
  withLogLevel(level: LogLevel): this {
    this.#level = level;
    return this;
  }

  /** Turns tracing on, and OTLP export with it. */
  withTracing(): this {
    return this.#edit((o) => {
      o.tracing.enabled = true;
      o.otlp.enabled = true;
    });
  }

  /** The options this builder would build with, without building. */
  resolve(): TelemetryOptions {
    const opts = defaultOptions();
    const fromFile = this.#platform.loadConfig(this.#configPath);
    for (const tree of [...fromFile, ...this.#trees]) {
      applyConfig(opts, tree);
    }
    if (this.#serviceFromCode) {
      const defaults = defaultOptions().service;
      opts.service = { ...defaults, labels: {} };
    }
    for (const edit of this.#edits) edit(opts);
    return opts;
  }

  build(): Telemetry {
    return this.#create(this.resolve(), this.#platform, this.#level);
  }

  #edit(edit: (opts: TelemetryOptions) => void): this {
    this.#edits.push(edit);
    return this;
  }
}

function stripUndefined<T extends object>(o: T): Partial<T> {
  return Object.fromEntries(Object.entries(o).filter(([, v]) => v !== undefined)) as Partial<T>;
}
