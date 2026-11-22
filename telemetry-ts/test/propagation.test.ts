// Automatic propagation: every HTTP, fetch and gRPC request this process
// makes or answers carries the trace on, with no inject or extract in the
// program, against an in-process OTLP/HTTP collector.

import http, { createServer, type IncomingHttpHeaders, type Server } from "node:http";
import type { AddressInfo } from "node:net";
import * as grpc from "@grpc/grpc-js";
import { suppressTracing } from "@opentelemetry/core";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { context, SpanKind, Telemetry, type TelemetryBuilder } from "../src/index.node.ts";
import { type Collector, startCollector } from "./collector.ts";

type Json = Record<string, any>;

// What another language's SDK sends: a sampled trace this process never saw.
const upstreamTrace = "0af7651916cd43dd8448eb211c80319c";
const upstreamSpan = "b7ad6b7169203331";
const upstream = `00-${upstreamTrace}-${upstreamSpan}-01`;

let collector: Collector;
const open: Array<{ close(): unknown }> = [];

beforeEach(async () => {
  collector = await startCollector();
});

afterEach(async () => {
  for (const c of open.splice(0).reverse()) await c.close();
  await collector.close();
});

function build(b: TelemetryBuilder = Telemetry.builder("svc", "1.0.0")): Telemetry {
  const t = b.withOtlpEndpoint(collector.endpoint).withOtlpHttp().withConsole(false).build();
  open.push(t);
  return t;
}

/** An HTTP server that records the headers of each request, by path. */
async function server(
  handle: (url: string, headers: IncomingHttpHeaders) => unknown = () => {},
): Promise<{ url: string; seen: Record<string, IncomingHttpHeaders> }> {
  const seen: Record<string, IncomingHttpHeaders> = {};
  const s: Server = createServer(async (req, res) => {
    seen[req.url ?? ""] = req.headers;
    await handle(req.url ?? "", req.headers);
    res.end("ok");
  });
  s.keepAliveTimeout = 1;
  await new Promise<void>((resolve) => s.listen(0, "127.0.0.1", resolve));
  open.push({ close: () => new Promise((resolve) => s.close(resolve)) });
  return { url: `http://127.0.0.1:${(s.address() as AddressInfo).port}`, seen };
}

/** A GET through node:http, the way most Node clients make one. */
function get(url: string, headers: Record<string, string> = {}): Promise<void> {
  return new Promise((resolve, reject) => {
    http
      .get(url, { headers }, (res) => {
        res.resume();
        res.on("end", resolve);
      })
      .on("error", reject);
  });
}

/** A request this process does not trace, as from a service in another language. */
function fromElsewhere<T>(fn: () => Promise<T>): Promise<T> {
  return context.with(suppressTracing(context.active()), fn);
}

const traceparent = (h: IncomingHttpHeaders | undefined) => String(h?.traceparent ?? "");
const traceOf = (tp: string) => tp.split("-")[1];
const spansNamed = (name: string) => collector.spans.filter((s) => s.name === name);

describe("HTTP and fetch", () => {
  it("carries the active trace on every outgoing request", async () => {
    const t = build();
    const down = await server();
    let traceId = "";
    await t.tracing.withSpan("checkout", async (span) => {
      traceId = span.traceId;
      await get(`${down.url}/http`);
      await fetch(`${down.url}/fetch`).then((r) => r.text());
    });
    expect(traceOf(traceparent(down.seen["/http"]))).toBe(traceId);
    expect(traceOf(traceparent(down.seen["/fetch"]))).toBe(traceId);
  });

  it("continues another service's trace, and hands it on", async () => {
    const t = build();
    const next = await server();
    const here = await server(() => get(`${next.url}/next`));
    await fromElsewhere(() => get(`${here.url}/order`, { traceparent: upstream }));
    await t.flush();

    // upstream → this server's span → its client call → the next service.
    const serverSpan = collector.spans.find((s) => s.parentSpanId === upstreamSpan);
    expect(serverSpan?.kind).toBe(SpanKind.SERVER + 1);
    expect(serverSpan?.traceId).toBe(upstreamTrace);
    const onward = traceparent(next.seen["/next"]);
    expect(traceOf(onward)).toBe(upstreamTrace);
    const client = collector.spans.find((s) => s.spanId === onward.split("-")[2]);
    expect(client?.parentSpanId).toBe(serverSpan?.spanId);
  });

  it("passes a sampled trace on with tracing off, recording nothing", async () => {
    const t = build(
      Telemetry.builder("svc", "1.0.0").withConfigObject({ tracing: { enabled: false } }),
    );
    const next = await server();
    const here = await server(() => fetch(`${next.url}/next`).then((r) => r.text()));
    await fromElsewhere(() => get(`${here.url}/order`, { traceparent: upstream }));
    await t.flush();

    const onward = traceparent(next.seen["/next"]);
    expect(traceOf(onward)).toBe(upstreamTrace);
    expect(onward.endsWith("-01")).toBe(true);
    expect(collector.spans).toEqual([]);
  });

  it("does not trace its own exports", async () => {
    const t = build();
    t.logger.info("hello");
    await t.tracing.withSpan("op", () => {});
    await t.flush();
    await t.flush();
    expect(collector.spans.map((s) => s.name)).toEqual(["op"]);
  });

  it("leaves requests alone without auto-instrumentation", async () => {
    const t = build(Telemetry.builder("svc", "1.0.0").withAutoInstrumentation(false));
    const down = await server();
    await t.tracing.withSpan("op", () => get(`${down.url}/http`));
    expect(down.seen["/http"]?.traceparent).toBeUndefined();
  });
});

describe("gRPC", () => {
  const id = (b: Buffer) => b;
  const service = {
    Echo: {
      path: "/demo.Echo/Echo",
      requestStream: false,
      responseStream: false,
      requestSerialize: id,
      requestDeserialize: id,
      responseSerialize: id,
      responseDeserialize: id,
    },
    Watch: {
      path: "/demo.Echo/Watch",
      requestStream: false,
      responseStream: true,
      requestSerialize: id,
      requestDeserialize: id,
      responseSerialize: id,
      responseDeserialize: id,
    },
  } as unknown as grpc.ServiceDefinition;

  async function echoServer() {
    const seen: Record<string, string> = {};
    const s = new grpc.Server();
    s.addService(service, {
      Echo: (call: grpc.ServerUnaryCall<Buffer, Buffer>, cb: grpc.sendUnaryData<Buffer>) => {
        seen.Echo = String(call.metadata.get("traceparent")[0] ?? "");
        cb(null, call.request);
      },
      Watch: (call: grpc.ServerWritableStream<Buffer, Buffer>) => {
        seen.Watch = String(call.metadata.get("traceparent")[0] ?? "");
        call.write(Buffer.from("1"));
        call.end();
      },
    });
    const port = await new Promise<number>((resolve, reject) =>
      s.bindAsync("127.0.0.1:0", grpc.ServerCredentials.createInsecure(), (e, p) =>
        e ? reject(e) : resolve(p),
      ),
    );
    const Client = grpc.makeGenericClientConstructor(service, "Echo");
    const client = new Client(`127.0.0.1:${port}`, grpc.credentials.createInsecure()) as Json;
    open.push({
      close: () => {
        client.close();
        s.forceShutdown();
      },
    });
    const echo = (md?: grpc.Metadata) =>
      new Promise<void>((resolve, reject) =>
        md
          ? client.Echo(Buffer.from("hi"), md, (e: Error | null) => (e ? reject(e) : resolve()))
          : client.Echo(Buffer.from("hi"), (e: Error | null) => (e ? reject(e) : resolve())),
      );
    const watch = () =>
      new Promise<void>((resolve, reject) => {
        const call = client.Watch(Buffer.from("hi"));
        call.on("data", () => {});
        call.on("end", resolve);
        call.on("error", reject);
      });
    return { seen, echo, watch };
  }

  it("traces client and server calls as one trace", async () => {
    const t = build();
    const { seen, echo, watch } = await echoServer();
    let traceId = "";
    await t.tracing.withSpan("checkout", async (span) => {
      traceId = span.traceId;
      await echo();
      await watch();
    });
    await t.flush();

    expect(traceOf(seen.Echo ?? "")).toBe(traceId);
    expect(traceOf(seen.Watch ?? "")).toBe(traceId);
    for (const name of ["demo.Echo/Echo", "demo.Echo/Watch"]) {
      const client = spansNamed(name).find((s) => s.kind === SpanKind.CLIENT + 1);
      const server = spansNamed(name).find((s) => s.kind === SpanKind.SERVER + 1);
      expect(client?.traceId).toBe(traceId);
      expect(server?.parentSpanId).toBe(client?.spanId);
    }
  });

  it("continues another service's trace from the metadata", async () => {
    const t = build();
    const { echo } = await echoServer();
    const md = new grpc.Metadata();
    md.set("traceparent", upstream);
    await fromElsewhere(() => echo(md));
    await t.flush();

    const server = spansNamed("demo.Echo/Echo").find((s) => s.kind === SpanKind.SERVER + 1);
    expect(server?.traceId).toBe(upstreamTrace);
    expect(server?.parentSpanId).toBe(upstreamSpan);
  });
});
