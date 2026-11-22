import { type ExportResult, ExportResultCode } from "@opentelemetry/core";
import { describe, expect, it } from "vitest";
import { Counters, ExportErrors, lossReport } from "../src/core/accounting.ts";
import { Batcher, type Exporter } from "../src/core/batcher.ts";
import { defaultExport, type ExportOptions } from "../src/core/options.ts";

/** An exporter that answers after `delayMillis` (synchronously when 0). */
class FakeExporter implements Exporter<number> {
  received: number[] = [];
  batches = 0;
  inFlight = 0;
  maxInFlight = 0;
  readonly delayMillis: number;
  readonly result: ExportResult;
  readonly shared: { inFlight: number; max: number } | undefined;
  constructor(
    delayMillis = 0,
    result: ExportResult = { code: ExportResultCode.SUCCESS },
    shared?: { inFlight: number; max: number },
  ) {
    this.delayMillis = delayMillis;
    this.result = result;
    this.shared = shared;
  }
  export(items: number[], done: (r: ExportResult) => void): void {
    this.batches++;
    this.received.push(...items);
    if (this.shared) this.shared.max = Math.max(this.shared.max, ++this.shared.inFlight);
    const answer = () => {
      if (this.shared) this.shared.inFlight--;
      done(this.result);
    };
    if (this.delayMillis) setTimeout(answer, this.delayMillis);
    else answer();
  }
  shutdown(): Promise<void> {
    return Promise.resolve();
  }
}

function batcher(exporters: FakeExporter[], opts: Partial<ExportOptions> = {}) {
  const counters = new Counters();
  const errors = new ExportErrors();
  const b = new Batcher<number>(
    exporters,
    { ...defaultExport(opts.lossless ?? false), intervalMillis: 1000, ...opts },
    counters,
    errors,
  );
  return { b, counters, errors };
}

describe("Batcher", () => {
  it("exports full batches at once and the rest on flush", async () => {
    const e = new FakeExporter();
    const { b, counters } = batcher([e], { batchSize: 10 });
    for (let i = 0; i < 25; i++) b.add(i);
    expect(e.received.length).toBe(20);
    await b.flush();
    expect(e.received).toEqual(Array.from({ length: 25 }, (_, i) => i));
    expect(counters.stats()).toEqual({
      emitted: 25,
      exported: 25,
      dropped: 0,
      failed: 0,
      pending: 0,
    });
  });

  it("exports a partial batch after the interval", async () => {
    const e = new FakeExporter();
    const { b } = batcher([e], { batchSize: 10, intervalMillis: 20 });
    b.add(1);
    expect(e.received).toEqual([]);
    await new Promise((r) => setTimeout(r, 60));
    expect(e.received).toEqual([1]);
  });

  it("drops and counts what overflows a full queue", async () => {
    const e = new FakeExporter(20);
    const { b, counters } = batcher([e], { queueSize: 100, batchSize: 50 });
    for (let i = 0; i < 1000; i++) b.add(i);
    await b.flush();
    const s = counters.stats();
    expect(s.emitted).toBe(1000);
    expect(s.dropped).toBeGreaterThan(0);
    expect(s.exported + s.dropped).toBe(1000);
    expect(s.pending).toBe(0);
    expect(lossReport(s, new Counters().stats())).toMatch(
      new RegExp(`${s.dropped} of 1000 log records dropped`),
    );
  });

  it("keeps everything when lossless, and ready() waits for room", async () => {
    const e = new FakeExporter(5);
    const { b, counters } = batcher([e], { lossless: true, queueSize: 100, batchSize: 50 });
    for (let i = 0; i < 1000; i++) b.add(i);
    expect(b.size).toBeGreaterThan(100);
    let ready = false;
    const waiting = b.ready().then(() => (ready = true));
    expect(ready).toBe(false);
    await waiting;
    expect(b.size).toBeLessThan(100);
    await b.flush();
    expect(counters.stats()).toMatchObject({ emitted: 1000, exported: 1000, dropped: 0 });
  });

  it("keeps one batch in flight per exporter", async () => {
    const shared = { inFlight: 0, max: 0 };
    const exporters = Array.from({ length: 4 }, () => new FakeExporter(10, undefined, shared));
    const { b, counters } = batcher(exporters, { lossless: true, batchSize: 10 });
    for (let i = 0; i < 200; i++) b.add(i);
    await b.flush();
    expect(shared.max).toBe(4);
    expect(exporters.every((e) => e.batches > 0)).toBe(true);
    expect(counters.exported).toBe(200);
  });

  it("counts failed exports and keeps the last error", async () => {
    const e = new FakeExporter(0, { code: ExportResultCode.FAILED, error: new Error("refused") });
    const { b, counters, errors } = batcher([e], { batchSize: 5 });
    for (let i = 0; i < 12; i++) b.add(i);
    await b.flush();
    expect(counters.stats()).toMatchObject({ exported: 0, failed: 12 });
    expect(errors).toMatchObject({ count: 3, last: "refused" });
  });

  it("survives a synchronous exporter over many batches without deep recursion", async () => {
    const e = new FakeExporter();
    const { b, counters } = batcher([e], { lossless: true, batchSize: 1 });
    for (let i = 0; i < 100_000; i++) b.add(i);
    await b.flush();
    expect(counters.exported).toBe(100_000);
  });

  it("drops what arrives after shutdown", async () => {
    const { b, counters } = batcher([new FakeExporter()]);
    await b.shutdown();
    b.add(1);
    expect(counters.stats()).toMatchObject({ emitted: 1, dropped: 1 });
  });
});
