// The notify service of the cross-language tracing demo (`just crosslang`):
// a plain node:http server, which Telemetry has instrumented, so each request
// continues the trace the caller sent, and calls to the next service with
// fetch carry it on. Neither side has an inject or extract of its own.
//
// It listens on a free port and prints {"listening": <port>}; GET /hop
// answers with this hop's view of the trace, followed by the hops after it
// (CROSSLANG_NEXT, a URL, when there is one). Set
// TELEMETRY_DEMO_OTLP=localhost:4317 to export the spans to a collector.
//
// Run with (after `npm run build`):
//   node examples/crosslang.ts

import { createServer } from "node:http";
import type { AddressInfo } from "node:net";
import { Telemetry } from "@the-protobuf-project/telemetry";

const tel = Telemetry.builder("notify-ts", "1.0.0")
  .withOtlpEndpoint(process.env.TELEMETRY_DEMO_OTLP ?? "127.0.0.1:4317")
  .withTracing()
  .withConsole(false)
  .build();
const next = process.env.CROSSLANG_NEXT;

const server = createServer(async (req, res) => {
  // The instrumentation's server span, which continues the caller's trace.
  const hops: unknown[] = [
    {
      service: "notify-ts",
      received_traceparent: req.headers.traceparent ?? null,
      trace_id: tel.tracing.currentTraceId(),
      span_id: tel.tracing.currentSpanId(),
    },
  ];
  if (next) {
    // fetch is instrumented: the request carries this server span on.
    const downstream = await fetch(`${next}/hop`);
    hops.push(...((await downstream.json()) as unknown[]));
  }
  res.writeHead(200, { "content-type": "application/json" }).end(JSON.stringify(hops));
});

server.listen(0, "127.0.0.1", () => {
  console.log(JSON.stringify({ listening: (server.address() as AddressInfo).port }));
});

for (const signal of ["SIGINT", "SIGTERM"] as const) {
  process.once(signal, () => {
    server.close();
    void tel.close().then(() => process.exit(0));
  });
}
