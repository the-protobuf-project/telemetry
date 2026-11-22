// End to end against an in-process OTLP/HTTP collector.

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  context,
  Environment,
  LogLevel,
  SpanKind,
  SpanStatusCode,
  Telemetry,
  type TelemetryBuilder,
} from "../src/index.node.ts";
import { attr, type Collector, startCollector } from "./collector.ts";

type Json = Record<string, any>;

let collector: Collector;
const open: Telemetry[] = [];

beforeEach(async () => {
  collector = await startCollector();
});

afterEach(async () => {
  await Promise.all(open.splice(0).map((t) => t.close()));
  await collector.close();
  vi.restoreAllMocks();
});

function build(b: TelemetryBuilder = Telemetry.builder("svc", "1.0.0")): Telemetry {
  const t = b.withOtlpEndpoint(collector.endpoint).withOtlpHttp().withConsole(false).build();
  open.push(t);
  return t;
}

describe("tracing", () => {
  it("exports spans with their parent, attributes and status", async () => {
    const t = build();
    await t.tracing.withSpan("parent", async (p) => {
      p.setAttribute("order.id", "o-1");
      await t.tracing.withSpan("child", () => {});
    });
    expect(() =>
      t.tracing.withSpan("fails", () => {
        throw new Error("boom");
      }),
    ).toThrow("boom");
    await t.flush();

    const byName = Object.fromEntries(collector.spans.map((s) => [s.name, s]));
    expect(Object.keys(byName).sort()).toEqual(["child", "fails", "parent"]);
    expect(byName.child.traceId).toBe(byName.parent.traceId);
    expect(byName.child.parentSpanId).toBe(byName.parent.spanId);
    expect(attr(byName.parent.attributes, "order.id")).toBe("o-1");
    expect(byName.fails.status.code).toBe(SpanStatusCode.ERROR);
    expect(byName.fails.events[0].name).toBe("exception");
  });

  it("continues a caller's trace from its headers, baggage included", () => {
    const t = build();
    const traceparent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01";
    const parent = t.tracing.extract({ TraceParent: traceparent, baggage: "tenant=acme" });
    const span = t.tracing.startSpan("handle", { parent, kind: SpanKind.SERVER });
    expect(span.traceId).toBe("4bf92f3577b34da6a3ce929d0e0e4736");
    expect(span.parentSpanId).toBe("00f067aa0ba902b7");
    expect(t.tracing.baggage(parent)).toEqual({ tenant: "acme" });

    const out = t.tracing.inject({}, span);
    expect(out.traceparent).toBe(`00-${span.traceId}-${span.spanId}-01`);
    expect(out.baggage).toBe("tenant=acme");
    span.end();
  });

  it("carries baggage it sets", () => {
    const t = build();
    const ctx = t.tracing.setBaggage("user", "u-7");
    const headers = context.with(ctx, () => t.tracing.withSpan("s", () => t.tracing.inject()));
    expect(headers.baggage).toBe("user=u-7");
    expect(headers.traceparent).toMatch(/^00-[0-9a-f]{32}-[0-9a-f]{16}-01$/);
  });

  it("keeps the active span across await", async () => {
    const t = build();
    await t.tracing.withSpan("outer", async (span) => {
      await new Promise((r) => setTimeout(r, 5));
      expect(t.tracing.currentSpanId()).toBe(span.spanId);
    });
    expect(t.tracing.currentSpanId()).toBeUndefined();
  });
});

describe("logging", () => {
  it("exports records with the active span's ids, labels and caller", async () => {
    const t = build(Telemetry.builder("svc", "1.0.0").withLabel("robot_id", "r-1"));
    let traceId = "";
    t.tracing.withSpan("work", (span) => {
      traceId = span.traceId;
      t.logger.info("inside", { attempt: 2 });
    });
    t.logger.warn("outside");
    await t.flush();

    const [inside, outside] = collector.logs as [Json, Json];
    expect(inside.body.stringValue).toBe("inside");
    expect(inside.severityText).toBe("INFO");
    expect(inside.traceId).toBe(traceId);
    expect(attr(inside.attributes, "attempt")).toBe(2);
    expect(attr(inside.attributes, "robot_id")).toBe("r-1");
    expect(attr(inside.attributes, "code.file.path")).toMatch(/telemetry\.test\.ts$/);
    expect(outside.traceId ?? "").toBe("");
  });

  it("filters by the service's level, with the config's module entry winning", async () => {
    const quiet = build(
      Telemetry.builder("nats-module", "0.2.0")
        .withLogLevel(LogLevel.Level3)
        .withConfigObject({ logging: { modules: { "nats-module": { level: 1 } } } }),
    );
    expect(quiet.logger.level).toBe(LogLevel.Level1);
    quiet.logger.debug("hidden");
    quiet.logger.info("hidden");
    quiet.logger.warn("hidden");
    quiet.logger.error("shown");
    await quiet.flush();
    expect(collector.logs.map((l) => l.body.stringValue)).toEqual(["shown"]);
  });

  it("defaults to debug in development and info in production", () => {
    const dev = build(Telemetry.builder("a", "1").environment(Environment.Development));
    const prod = build(Telemetry.builder("b", "1").environment(Environment.Production));
    expect(dev.logger.level).toBe(LogLevel.Level3);
    expect(prod.logger.level).toBe(LogLevel.Level2);
  });

  it("prints to stderr in the shared format", () => {
    const write = vi.spyOn(process.stderr, "write").mockReturnValue(true);
    const t = Telemetry.builder("printer", "1.0.0").build();
    open.push(t);
    t.logger.info("hello", { n: 1 });
    const line = String(write.mock.calls[0]?.[0]);
    expect(line).toMatch(
      /^\S+T\d\d:\d\d:\d\d[+-]\d\d:\d\d INFO: <test\/telemetry\.test\.ts:\d+> printer: hello\n {2}data=\n {2}│ \{/,
    );
  });
});

describe("metrics", () => {
  it("exports counters, histograms and gauges with labels", async () => {
    const t = build(Telemetry.builder("svc", "1.0.0").withLabel("fleet", "alpha"));
    t.metrics.counter("requests_total");
    t.metrics.counter("requests_total", 2, { route: "/a" });
    t.metrics.histogram("latency_ms", 12.5, undefined, [10, 20]);
    t.metrics.gauge("queue_depth", 7);
    await t.flush();

    const names = collector.metrics.map((m) => m.name).sort();
    expect(names).toEqual(["latency_ms", "queue_depth", "requests_total"]);
    const counter = collector.metrics.find((m) => m.name === "requests_total") as Json;
    const points = counter.sum.dataPoints;
    expect(points.map((p: any) => p.asDouble ?? Number(p.asInt)).sort()).toEqual([1, 2]);
    expect(points.every((p: any) => attr(p.attributes, "fleet") === "alpha")).toBe(true);
    const histogram = collector.metrics.find((m) => m.name === "latency_ms") as Json;
    expect(histogram.histogram.dataPoints[0].explicitBounds).toEqual([10, 20]);
  });
});

describe("export accounting", () => {
  it("delivers every record when lossless, across parallel exporters", async () => {
    collector.delayMillis = 5;
    const t = build(
      Telemetry.builder("load", "1.0.0").withExport({
        lossless: true,
        queueSize: 256,
        batchSize: 128,
        exporters: 4,
      }),
    );
    for (let i = 0; i < 5000; i++) t.tracing.startSpan("s").end();
    const stats = await t.close();
    expect(stats.spans).toEqual({
      emitted: 5000,
      exported: 5000,
      dropped: 0,
      failed: 0,
      pending: 0,
    });
    expect(collector.spans.length).toBe(5000);
    expect(collector.maxInFlight).toBeGreaterThan(1);
  });

  it("counts drops and reports them on close", async () => {
    collector.delayMillis = 20;
    const warn = vi.spyOn(process, "emitWarning").mockImplementation(() => {});
    const t = build(
      Telemetry.builder("burst", "1.0.0").withExport({ queueSize: 100, batchSize: 50 }),
    );
    for (let i = 0; i < 2000; i++) t.logger.info("x");
    const stats = await t.close();
    expect(stats.logs.emitted).toBe(2000);
    expect(stats.logs.dropped).toBeGreaterThan(0);
    expect(stats.logs.exported + stats.logs.dropped).toBe(2000);
    expect(collector.logs.length).toBe(stats.logs.exported);
    expect(warn).toHaveBeenCalledWith(
      expect.stringMatching(/of 2000 log records dropped \(export queue full\)/),
      "TelemetryWarning",
    );
  });

  it("counts failed exports", async () => {
    collector.failWith = 400;
    vi.spyOn(process, "emitWarning").mockImplementation(() => {});
    const t = build();
    t.tracing.startSpan("lost").end();
    const stats = await t.close();
    expect(stats.spans).toMatchObject({ emitted: 1, failed: 1 });
    expect(stats.exportErrors).toBe(1);
  });

  it("close is idempotent and Symbol.asyncDispose closes", async () => {
    const t = build();
    expect(await t.close()).toBe(await t.close());
    {
      await using u = build();
      u.tracing.startSpan("disposed").end();
    }
    expect(collector.spans.map((s) => s.name)).toContain("disposed");
  });
});
