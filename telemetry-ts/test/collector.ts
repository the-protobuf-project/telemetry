// An in-process OTLP/HTTP collector for the tests: counts the spans, log
// records and metric data points it receives, and keeps the payloads.

import { createServer, type Server } from "node:http";
import type { AddressInfo } from "node:net";

type Json = Record<string, any>;

export interface Collector {
  endpoint: string;
  spans: Json[];
  logs: Json[];
  metrics: Json[];
  /** Milliseconds each request waits before it is answered. */
  delayMillis: number;
  /** Answer every request with this status instead of 200. */
  failWith: number | undefined;
  /** The most requests in flight at once. */
  maxInFlight: number;
  close(): Promise<void>;
}

export async function startCollector(): Promise<Collector> {
  let inFlight = 0;
  const c: Collector = {
    endpoint: "",
    spans: [],
    logs: [],
    metrics: [],
    delayMillis: 0,
    failWith: undefined,
    maxInFlight: 0,
    close: () => new Promise((resolve) => server.close(() => resolve())),
  };
  const server: Server = createServer((req, res) => {
    inFlight++;
    c.maxInFlight = Math.max(c.maxInFlight, inFlight);
    const chunks: Buffer[] = [];
    req.on("data", (chunk: Buffer) => chunks.push(chunk));
    req.on("end", () => {
      setTimeout(() => {
        inFlight--;
        if (c.failWith) {
          res.writeHead(c.failWith).end();
          return;
        }
        const body = JSON.parse(Buffer.concat(chunks).toString() || "{}") as Json;
        if (req.url === "/v1/traces") {
          for (const rs of body.resourceSpans ?? [])
            for (const ss of rs.scopeSpans ?? []) c.spans.push(...(ss.spans ?? []));
        } else if (req.url === "/v1/logs") {
          for (const rl of body.resourceLogs ?? [])
            for (const sl of rl.scopeLogs ?? []) c.logs.push(...(sl.logRecords ?? []));
        } else if (req.url === "/v1/metrics") {
          for (const rm of body.resourceMetrics ?? [])
            for (const sm of rm.scopeMetrics ?? []) c.metrics.push(...(sm.metrics ?? []));
        }
        res.writeHead(200, { "content-type": "application/json" }).end("{}");
      }, c.delayMillis);
    });
  });
  // Keep-alive sockets would hold close() open until they time out.
  server.keepAliveTimeout = 1;
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  c.endpoint = `127.0.0.1:${(server.address() as AddressInfo).port}`;
  return c;
}

/** An attribute's value from an OTLP JSON key/value list. */
export function attr(attributes: Json[] | undefined, key: string): unknown {
  const kv = attributes?.find((a) => a.key === key);
  if (!kv) return undefined;
  const v = kv.value ?? {};
  return v.stringValue ?? v.intValue ?? v.doubleValue ?? v.boolValue;
}
