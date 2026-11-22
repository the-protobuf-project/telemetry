// Writes the load examples/loadgen.ts writes (n log records, spans or counter
// increments) through the upstream OpenTelemetry JS SDK alone, with its batch
// processors and OTLP gRPC exporters, and prints one JSON line with how long
// it took. bench/bench_all.py runs it beside loadgen.ts to compare this SDK
// with plain OpenTelemetry. It tunes the batch processors from the standard
// OTEL_BSP_* and OTEL_BLRP_* variables, as the bench does for every language:
// the 3.x JS processors no longer read them themselves (only sdk-node does).
// OTEL_BASELINE_HTTP=1 exports over OTLP/HTTP (the endpoint is then the HTTP port).
//
// Run with:
//   node examples/otel_baseline.ts <signal> <n> <host:port>

import { SeverityNumber } from "@opentelemetry/api";
import { OTLPLogExporter as GrpcLogs } from "@opentelemetry/exporter-logs-otlp-grpc";
import { OTLPLogExporter as HttpLogs } from "@opentelemetry/exporter-logs-otlp-http";
import { OTLPMetricExporter as GrpcMetrics } from "@opentelemetry/exporter-metrics-otlp-grpc";
import { OTLPMetricExporter as HttpMetrics } from "@opentelemetry/exporter-metrics-otlp-http";
import { OTLPTraceExporter as GrpcTraces } from "@opentelemetry/exporter-trace-otlp-grpc";
import { OTLPTraceExporter as HttpTraces } from "@opentelemetry/exporter-trace-otlp-http";
import { BatchLogRecordProcessor, LoggerProvider } from "@opentelemetry/sdk-logs";
import { MeterProvider, PeriodicExportingMetricReader } from "@opentelemetry/sdk-metrics";
import { BatchSpanProcessor, TracerProvider } from "@opentelemetry/sdk-trace";

const signal = process.argv[2] ?? "logs";
const n = Number(process.argv[3] ?? 1_000_000);
const endpoint = process.argv[4] ?? "127.0.0.1:4317";
const http = process.env.OTEL_BASELINE_HTTP === "1";
// The official processors flush every queued batch at once on shutdown; with
// the bench's queue of a million that is over a hundred requests, past the
// exporters' default limit of 30 in flight, and the rejection ends the
// process. No OTEL_* variable sets the limit, so it is raised here.
const concurrencyLimit = 1024;
const url = (path: string) => ({
  url: http ? `http://${endpoint}${path}` : `http://${endpoint}`,
  concurrencyLimit,
});

/** The batch processor options a signal's OTEL_* variables set, as the spec names them. */
function batch(prefix: "OTEL_BSP_" | "OTEL_BLRP_") {
  const read = (name: string) => {
    const v = Number(process.env[prefix + name]);
    return Number.isFinite(v) && v > 0 ? v : undefined;
  };
  return {
    maxQueueSize: read("MAX_QUEUE_SIZE"),
    maxExportBatchSize: read("MAX_EXPORT_BATCH_SIZE"),
    scheduledDelayMillis: read("SCHEDULE_DELAY"),
    exportTimeoutMillis: read("EXPORT_TIMEOUT"),
  };
}

let emit: (i: number) => void;
let provider: { shutdown(): Promise<void> };

if (signal === "logs") {
  const exporter = http ? new HttpLogs(url("/v1/logs")) : new GrpcLogs(url(""));
  const p = new LoggerProvider({
    processors: [new BatchLogRecordProcessor({ exporter, ...batch("OTEL_BLRP_") })],
  });
  const logger = p.getLogger("otel-baseline");
  emit = (i) =>
    logger.emit({
      severityNumber: SeverityNumber.INFO,
      severityText: "INFO",
      body: "load",
      attributes: { i },
    });
  provider = p;
} else if (signal === "spans") {
  const exporter = http ? new HttpTraces(url("/v1/traces")) : new GrpcTraces(url(""));
  const p = new TracerProvider({
    spanProcessors: [new BatchSpanProcessor({ exporter, ...batch("OTEL_BSP_") })],
  });
  const tracer = p.getTracer("otel-baseline");
  emit = () => tracer.startSpan("load.op").end();
  provider = p;
} else if (signal === "metrics") {
  const exporter = http ? new HttpMetrics(url("/v1/metrics")) : new GrpcMetrics(url(""));
  const p = new MeterProvider({ readers: [new PeriodicExportingMetricReader({ exporter })] });
  const counter = p.getMeter("otel-baseline").createCounter("ops");
  emit = () => counter.add(1);
  provider = p;
} else {
  throw new Error(`unknown signal ${signal}`);
}

let start = performance.now();
for (let i = 0; i < n; i++) emit(i);
const emitS = (performance.now() - start) / 1000;

start = performance.now();
await provider.shutdown();
const closeS = (performance.now() - start) / 1000;

console.log(JSON.stringify({ lang: "ts", signal, n, workers: 1, emit_s: emitS, close_s: closeS }));
