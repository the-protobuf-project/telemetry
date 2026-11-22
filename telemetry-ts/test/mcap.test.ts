import { mkdtempSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { McapStreamReader } from "@mcap/core";
import { expect, it } from "vitest";
import { Telemetry } from "../src/index.node.ts";

it("records logs, metrics and spans to one MCAP file without a collector", async () => {
  const path = join(mkdtempSync(join(tmpdir(), "telemetry-ts-")), "run.mcap");
  const t = Telemetry.builder("recorder", "1.2.3").withConsole(false).withMcap(path).build();
  t.tracing.withSpan("step", (span) => {
    span.setAttribute("k", "v");
    t.logger.info("hello", { n: 1 });
  });
  t.metrics.counter("ticks", 3);
  await t.close();

  const reader = new McapStreamReader();
  reader.append(readFileSync(path));
  const schemas = new Map<number, string>();
  const topics = new Map<number, string>();
  const messages: Record<string, any[]> = {};
  for (let record = reader.nextRecord(); record; record = reader.nextRecord()) {
    if (record.type === "Schema") schemas.set(record.id, record.name);
    if (record.type === "Channel") topics.set(record.id, record.topic);
    if (record.type === "Message") {
      const topic = topics.get(record.channelId) as string;
      const list = messages[topic] ?? [];
      list.push(JSON.parse(new TextDecoder().decode(record.data)));
      messages[topic] = list;
    }
  }
  expect(reader.done()).toBe(true);
  expect([...schemas.values()].sort()).toEqual([
    "foxglove.Log",
    "mahcanirobotics.metric",
    "the-protobuf-project.trace",
  ]);
  expect(messages["/logs"]).toMatchObject([
    {
      level: 2,
      message: "hello",
      name: "recorder",
      service_version: "1.2.3",
      file: "test/mcap.test.ts",
      data: { n: 1 },
    },
  ]);
  expect(messages["/metrics"]).toMatchObject([{ name: "ticks", value: 3 }]);
  expect(messages["/traces"]).toMatchObject([
    { name: "step", parent_span_id: "", attributes: { k: "v" } },
  ]);
  expect(messages["/traces"]?.[0].trace_id).toMatch(/^[0-9a-f]{32}$/);
});
