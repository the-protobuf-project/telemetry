/**
 * The browser: no config file or environment (configure in code, or with
 * `withConfigObject`), OTLP over HTTP only, context that follows synchronous
 * code (use `logger.withContext(span.context)` across an `await`), `fetch`
 * and XHR traced, and pending records flushed when the page is hidden. MCAP
 * recording and profiling need Node.
 */

import { OTLPLogExporter } from "@opentelemetry/exporter-logs-otlp-http";
import { OTLPMetricExporter } from "@opentelemetry/exporter-metrics-otlp-http";
import { OTLPTraceExporter } from "@opentelemetry/exporter-trace-otlp-http";
import { registerInstrumentations } from "@opentelemetry/instrumentation";
import { FetchInstrumentation } from "@opentelemetry/instrumentation-fetch";
import { XMLHttpRequestInstrumentation } from "@opentelemetry/instrumentation-xml-http-request";
import { StackContextManager } from "@opentelemetry/sdk-trace";
import type { OtlpOptions } from "../core/options.ts";
import { otlpHeaders, otlpUrl, type Platform } from "../core/platform.ts";

const methods = {
  DEBUG: "debug",
  INFO: "info",
  WARN: "warn",
  ERROR: "error",
  FATAL: "error",
} as const;

/** The collector's HTTP endpoint: a browser exports to its port 4318, never 4317. */
function httpOtlp(otlp: OtlpOptions): OtlpOptions {
  return { ...otlp, useHttp: true, endpoint: otlp.endpoint.replace(/:4317(?=\/|$)/, ":4318") };
}

/** A URL prefix as the instrumentations match it: a pattern anchored at the start. */
function prefix(url: string): RegExp {
  return new RegExp(`^${url.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}`);
}

function nodeOnly(feature: string): never {
  throw new Error(`telemetry: ${feature} needs Node; it is not available in the browser`);
}

export const browserPlatform: Platform = {
  name: "browser",
  contextManager: () => new StackContextManager(),
  env: () => ({}),
  loadConfig: (path) => (path ? nodeOnly(`reading ${path}`) : []),
  exporters(otlp, timeoutMillis) {
    // A browser cannot speak gRPC, so export is always OTLP over HTTP, and
    // the collector's gRPC port (4317) becomes its HTTP one (4318).
    const http = httpOtlp(otlp);
    const headers = otlpHeaders(otlp);
    const at = (path: string) => ({ url: otlpUrl(http, true, path), headers, timeoutMillis });
    return {
      span: () => new OTLPTraceExporter(at("/v1/traces")),
      log: () => new OTLPLogExporter(at("/v1/logs")),
      metric: () => new OTLPMetricExporter(at("/v1/metrics")),
    };
  },
  console(r) {
    const prefix = `${r.level} ${r.service.name}:`;
    const where = r.caller ? ` <${r.caller.file}:${r.caller.line}>` : "";
    const args: unknown[] = [`${prefix}${where} ${r.message}`];
    if (r.data && Object.keys(r.data).length) args.push(r.data);
    console[methods[r.level]](...args);
  },
  caller: () => undefined,
  recorder: () => nodeOnly("MCAP recording"),
  startProfiling: () => nodeOnly("profiling"),
  warn: (message) => console.warn(message),
  instrument({ tracerProvider, meterProvider, tracing, otlp }) {
    // Same-origin requests carry the trace; another origin only when listed,
    // since it must allow the headers. Exports to the collector are not traced.
    const config = {
      propagateTraceHeaderCorsUrls: tracing.propagateTo.map(prefix),
      ignoreUrls: otlp ? [prefix(otlpUrl(httpOtlp(otlp), true))] : [],
    };
    return registerInstrumentations({
      tracerProvider,
      ...(meterProvider && { meterProvider }),
      instrumentations: [
        new FetchInstrumentation(config),
        new XMLHttpRequestInstrumentation(config),
        ...tracing.instrumentations,
      ],
    });
  },
  attach(flush) {
    if (typeof document === "undefined" || typeof addEventListener === "undefined") {
      return () => {};
    }
    // The last chance to send what is queued before a tab is closed or frozen.
    const onHide = () => {
      if (document.visibilityState === "hidden") void flush();
    };
    const onPageHide = () => void flush();
    document.addEventListener("visibilitychange", onHide);
    addEventListener("pagehide", onPageHide);
    return () => {
      document.removeEventListener("visibilitychange", onHide);
      removeEventListener("pagehide", onPageHide);
    };
  },
};
