// Writes N log records, spans or counter increments through the TypeScript
// SDK to an OTLP collector and prints one JSON line with how long it took.
// Driven by bench/bench_all.py, which counts what reached the collector.
//
// Run with (after `npm run build`):
//   node examples/loadgen.ts <signal> <n> <host:port>
//
// The burst is one synchronous loop, as in the other SDKs' load programs.
// JavaScript runs it on the same thread as the exporters, so in the default
// mode they send nothing until it ends and the queue keeps only what fits;
// lossless mode awaits `ready()` every 1024 records, as a program that wants
// every record would.

import { Telemetry } from "@the-protobuf-project/telemetry";

const signal = process.argv[2] ?? "logs";
const n = Number(process.argv[3] ?? 1_000_000);
const endpoint = process.argv[4] ?? "127.0.0.1:4317";
const env = process.env;

// LOADGEN_MCAP=path records to MCAP; an endpoint of "none" skips OTLP, to
// measure MCAP recording alone. LOADGEN_HTTP=1 exports over OTLP/HTTP (the
// endpoint is then the HTTP port).
let b = Telemetry.builder("loadgen-ts", "1.0.0").withConsole(env.LOADGEN_CONSOLE === "1");
if (!endpoint.endsWith(":none") && endpoint !== "none") b = b.withOtlpEndpoint(endpoint);
if (env.LOADGEN_HTTP === "1") b = b.withOtlpHttp();
if (env.LOADGEN_MCAP) b = b.withMcap(env.LOADGEN_MCAP);
const lossless = env.LOADGEN_LOSSLESS === "1";
const exporters = Number(env.LOADGEN_EXPORTERS ?? 1);
if (lossless || exporters > 1) b = b.withExport({ lossless, exporters });
const tel = b.build();

const emit: Record<string, (i: number) => void> = {
  logs: (i) => tel.logger.info("load", { i }),
  spans: () => tel.tracing.startSpan("load.op").end(),
  metrics: () => tel.metrics.counter("ops", 1),
};
const one = emit[signal];
if (!one) throw new Error(`unknown signal ${signal}`);

let start = performance.now();
for (let i = 0; i < n; i++) {
  one(i);
  if (lossless && (i & 1023) === 1023) await tel.ready();
}
const emitS = (performance.now() - start) / 1000;

start = performance.now();
const stats = await tel.close();
const closeS = (performance.now() - start) / 1000;

console.log(
  JSON.stringify({
    lang: "ts",
    signal,
    n,
    workers: 1,
    emit_s: emitS,
    close_s: closeS,
    dropped_reported:
      signal === "logs" ? stats.logs.dropped : signal === "spans" ? stats.spans.dropped : 0,
  }),
);
