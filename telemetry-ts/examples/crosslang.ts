// A hop of the cross-language tracing demo (`just crosslang`): continues, in
// TypeScript, the trace an upstream service started, from the W3C headers it
// sent, and hands it on.
//
// The headers arrive on stdin as a JSON object; in a real service they are
// the incoming HTTP headers, gRPC metadata or message headers. Set
// TELEMETRY_DEMO_OTLP=localhost:4317 to export the span to a collector.
//
// Run with (after `npm run build`):
//   echo '{"traceparent": "00-…-…-01"}' | node examples/crosslang.ts

import { SpanKind, Telemetry } from "@the-protobuf-project/telemetry";

let input = "";
for await (const chunk of process.stdin) input += chunk;
const headers = JSON.parse(input) as Record<string, string>;

const tel = Telemetry.builder("notify-ts", "1.0.0")
  .withOtlpEndpoint(process.env.TELEMETRY_DEMO_OTLP ?? "127.0.0.1:4317")
  .withTracing()
  .withConsole(false)
  .build();

// The one call that joins the caller's trace.
const parent = tel.tracing.extract(headers);
tel.tracing.withSpan(
  "notify.send",
  (span) => {
    span.setAttribute("order.id", "o-42");
    const downstream = tel.tracing.inject(); // and the next hop continues it in turn
    console.log(
      JSON.stringify({
        received_traceparent: headers.traceparent,
        trace_id: span.traceId,
        span_id: span.spanId,
        downstream_traceparent: downstream.traceparent,
        baggage: tel.tracing.baggage(parent),
      }),
    );
  },
  { parent, kind: SpanKind.SERVER },
);
await tel.close();
