/**
 * Node: config files, AsyncLocalStorage context, OTLP over gRPC or HTTP,
 * HTTP, fetch and gRPC instrumentation, MCAP and Pyroscope.
 */

import { createRequire } from "node:module";
import { Metadata } from "@grpc/grpc-js";
import { AsyncLocalStorageContextManager } from "@opentelemetry/context-async-hooks";
import { OTLPLogExporter as GrpcLogExporter } from "@opentelemetry/exporter-logs-otlp-grpc";
import { OTLPLogExporter as HttpLogExporter } from "@opentelemetry/exporter-logs-otlp-http";
import { OTLPMetricExporter as GrpcMetricExporter } from "@opentelemetry/exporter-metrics-otlp-grpc";
import { OTLPMetricExporter as HttpMetricExporter } from "@opentelemetry/exporter-metrics-otlp-http";
import { OTLPTraceExporter as GrpcTraceExporter } from "@opentelemetry/exporter-trace-otlp-grpc";
import { OTLPTraceExporter as HttpTraceExporter } from "@opentelemetry/exporter-trace-otlp-http";
import type { OtlpOptions, ProfilingOptions, ServiceOptions } from "../core/options.ts";
import { type Exporters, otlpHeaders, otlpUrl, type Platform } from "../core/platform.ts";
import { environment, loadConfig } from "./config.ts";
import { caller, writeConsole } from "./console.ts";
import { instrument } from "./instrument.ts";
import { McapRecorder } from "./mcap.ts";

function exporters(otlp: OtlpOptions, timeoutMillis: number): Exporters {
  const headers = otlpHeaders(otlp);
  if (otlp.useHttp) {
    const at = (path: string) => ({ url: otlpUrl(otlp, true, path), headers, timeoutMillis });
    return {
      span: () => new HttpTraceExporter(at("/v1/traces")),
      log: () => new HttpLogExporter(at("/v1/logs")),
      metric: () => new HttpMetricExporter(at("/v1/metrics")),
    };
  }
  // The URL's scheme picks TLS: https for secure, http for plaintext.
  const url = otlpUrl(otlp, false);
  const metadata = () => {
    const md = new Metadata();
    for (const [k, v] of Object.entries(headers)) md.set(k, v);
    return md;
  };
  return {
    span: () => new GrpcTraceExporter({ url, metadata: metadata(), timeoutMillis }),
    log: () => new GrpcLogExporter({ url, metadata: metadata(), timeoutMillis }),
    metric: () => new GrpcMetricExporter({ url, metadata: metadata(), timeoutMillis }),
  };
}

const require = createRequire(import.meta.url);

/**
 * Starts the Pyroscope agent with the tags the other SDKs send (service,
 * version, environment), so one Grafana query covers every language.
 */
function startProfiling(service: ServiceOptions, opts: ProfilingOptions): () => Promise<void> {
  let pyroscope: {
    init(config: object): void;
    start(): void;
    stop(): Promise<void>;
  };
  try {
    pyroscope = require("@pyroscope/nodejs");
  } catch (error) {
    throw new Error(
      "telemetry: profiling is enabled but @pyroscope/nodejs is not installed: npm install @pyroscope/nodejs",
      { cause: error },
    );
  }
  pyroscope.init({
    appName: `${service.name}.${service.environment}`,
    serverAddress: opts.serverAddress,
    ...(opts.basicAuthUser && { basicAuthUser: opts.basicAuthUser }),
    ...(opts.basicAuthPassword && { basicAuthPassword: opts.basicAuthPassword }),
    ...(opts.tenantId && { tenantID: opts.tenantId }),
    tags: {
      service: service.name,
      version: service.version,
      environment: service.environment,
      ...opts.tags,
    },
  });
  pyroscope.start();
  return () => pyroscope.stop();
}

function warn(message: string): void {
  process.emitWarning(message, "TelemetryWarning");
}

export const nodePlatform: Platform = {
  name: "node",
  contextManager: () => new AsyncLocalStorageContextManager(),
  env: () => environment(),
  loadConfig,
  exporters,
  console: writeConsole,
  caller,
  recorder: (path, service) => new McapRecorder(path, service, warn),
  startProfiling,
  warn,
  instrument,
};
