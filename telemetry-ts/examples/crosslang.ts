// The notify service of the cross-language tracing demo (`just crosslang`):
// it renders the order confirmation and sends it, the last step of the
// journey. A plain node:http server, which Telemetry has instrumented, so each
// request continues the trace the caller sent, and calls to a next service
// with fetch would carry it on. Neither side has an inject or extract of its
// own.
//
// Each step is a span of its own under the request's, logs under it (so the
// logs carry its trace and span id) and counts what it did, so the journey
// shows in Grafana as one trace with its logs and metrics.
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
import { setTimeout as sleep } from "node:timers/promises";
import { Telemetry } from "@the-protobuf-project/telemetry";

const tel = Telemetry.builder("notify-ts", "1.0.0")
  .withOtlpEndpoint(process.env.TELEMETRY_DEMO_OTLP ?? "127.0.0.1:4317")
  .withTracing()
  .withConsole(false)
  .build();
const next = process.env.CROSSLANG_NEXT;

const server = createServer(async (req, res) => {
  // The instrumentation's server span, which continues the caller's trace.
  const query = new URL(req.url ?? "/", "http://notify").searchParams;
  const order = query.get("order") ?? "";

  await tel.tracing.withSpan("notify.render", async (span) => {
    span.setAttribute("order.id", order);
    await sleep(7);
    tel.logger.info(`confirmation rendered for ${order}`);
  });
  await tel.tracing.withSpan("notify.send", async (span) => {
    span.setAttribute("order.id", order);
    span.setAttribute("notify.channel", "email");
    await sleep(11);
    tel.metrics.counter("notifications_sent_total", 1, { channel: "email" });
    tel.logger.info(`confirmation sent for ${order}`, { channel: "email" });
  });

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
    const downstream = await fetch(`${next}/hop?${query}`);
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
