/**
 * One MCAP file with every signal, for Foxglove Studio: logs on /logs,
 * metrics on /metrics and spans on /traces, JSON-encoded under the same
 * schemas the Go, Rust, Python and C++ SDKs write, so one layout opens a
 * recording from any of them.
 */

import { closeSync, openSync, writeSync } from "node:fs";
import { McapWriter } from "@mcap/core";
import type { ServiceOptions } from "../core/options.ts";
import type { LevelName, Recorder } from "../core/platform.ts";

const time = {
  type: "object",
  properties: {
    sec: { type: "integer", minimum: 0 },
    nsec: { type: "integer", minimum: 0, maximum: 999999999 },
  },
  required: ["sec", "nsec"],
};

const schemas = {
  log: {
    name: "foxglove.Log",
    schema: {
      $schema: "http://json-schema.org/draft-07/schema#",
      title: "Log",
      description: "A log message with timestamp, level, and structured data",
      type: "object",
      properties: {
        timestamp: time,
        level: {
          type: "integer",
          description: "Log level (1=DEBUG, 2=INFO, 3=WARN, 4=ERROR, 5=FATAL)",
        },
        message: { type: "string" },
        name: { type: "string" },
        file: { type: "string" },
        line: { type: "integer", minimum: 0 },
        service_version: { type: "string" },
        service_environment: { type: "string" },
        data: { type: "object" },
      },
      required: [
        "timestamp",
        "level",
        "message",
        "name",
        "file",
        "line",
        "service_version",
        "service_environment",
      ],
    },
  },
  metric: {
    name: "mahcanirobotics.metric",
    schema: {
      $schema: "http://json-schema.org/draft-07/schema#",
      title: "mahcanirobotics.metric",
      description: "A metric value with timestamp for time-series visualization",
      type: "object",
      properties: { timestamp: time, name: { type: "string" }, value: { type: "number" } },
      required: ["timestamp", "name", "value"],
    },
  },
  trace: {
    name: "the-protobuf-project.trace",
    schema: {
      $schema: "http://json-schema.org/draft-07/schema#",
      title: "the-protobuf-project.trace",
      description: "A distributed trace span with timing and context",
      type: "object",
      properties: {
        timestamp: time,
        trace_id: { type: "string" },
        span_id: { type: "string" },
        parent_span_id: { type: "string" },
        name: { type: "string" },
        attributes: { type: "object" },
      },
      required: ["timestamp", "trace_id", "span_id", "name"],
    },
  },
};

const levels: Record<LevelName, number> = { DEBUG: 1, INFO: 2, WARN: 3, ERROR: 4, FATAL: 5 };

const encoder = new TextEncoder();

export class McapRecorder implements Recorder {
  readonly #fd: number;
  readonly #writer: McapWriter;
  readonly #service: ServiceOptions;
  readonly #warn: (message: string) => void;
  #position = 0n;
  // McapWriter is async; one chain keeps messages in the order they were written.
  #tail: Promise<unknown>;
  #channels: Promise<{ log: number; metric: number; trace: number }>;
  #sequence = 0;
  #failed = false;
  #closed = false;

  constructor(path: string, service: ServiceOptions, warn: (message: string) => void) {
    this.#service = service;
    this.#warn = warn;
    this.#fd = openSync(path, "w");
    const writer = new McapWriter({
      writable: {
        write: async (buffer: Uint8Array) => {
          this.#position += BigInt(writeSync(this.#fd, buffer));
        },
        position: () => this.#position,
      },
    });
    this.#writer = writer;
    this.#channels = (async () => {
      await writer.start({ profile: "", library: "@the-protobuf-project/telemetry" });
      const channel = async (topic: string, s: { name: string; schema: object }) =>
        writer.registerChannel({
          topic,
          messageEncoding: "json",
          schemaId: await writer.registerSchema({
            name: s.name,
            encoding: "jsonschema",
            data: encoder.encode(JSON.stringify(s.schema)),
          }),
          metadata: new Map([["service", service.name]]),
        });
      return {
        log: await channel("/logs", schemas.log),
        metric: await channel("/metrics", schemas.metric),
        trace: await channel("/traces", schemas.trace),
      };
    })();
    this.#tail = this.#channels.catch((e) => this.#fail(e));
  }

  log(e: Parameters<Recorder["log"]>[0]): void {
    this.#add("log", e.timeNanos, {
      timestamp: stamp(e.timeNanos),
      level: levels[e.level],
      message: e.message,
      name: this.#service.name,
      file: e.caller?.file ?? "",
      line: e.caller?.line ?? 0,
      service_version: this.#service.version,
      service_environment: this.#service.environment,
      data: e.data ?? {},
    });
  }

  metric(name: string, value: number, timeNanos: bigint): void {
    this.#add("metric", timeNanos, { timestamp: stamp(timeNanos), name, value });
  }

  span(e: Parameters<Recorder["span"]>[0]): void {
    this.#add("trace", e.timeNanos, {
      timestamp: stamp(e.timeNanos),
      trace_id: e.traceId,
      span_id: e.spanId,
      parent_span_id: e.parentSpanId,
      name: e.name,
      attributes: e.attributes,
    });
  }

  async close(): Promise<void> {
    if (this.#closed) return;
    this.#closed = true;
    await this.#tail;
    if (!this.#failed) {
      await this.#writer.end().catch((e) => this.#fail(e));
    }
    closeSync(this.#fd);
  }

  #add(topic: "log" | "metric" | "trace", timeNanos: bigint, message: object): void {
    if (this.#closed || this.#failed) return;
    const data = encoder.encode(JSON.stringify(message, replacer));
    const sequence = this.#sequence++;
    this.#tail = this.#tail.then(async () => {
      if (this.#failed) return;
      const channels = await this.#channels;
      await this.#writer
        .addMessage({
          channelId: channels[topic],
          sequence,
          logTime: timeNanos,
          publishTime: timeNanos,
          data,
        })
        .catch((e) => this.#fail(e));
    });
  }

  #fail(error: unknown): void {
    if (this.#failed) return;
    this.#failed = true;
    this.#warn(
      `telemetry: MCAP recording stopped: ${error instanceof Error ? error.message : error}`,
    );
  }
}

function stamp(ns: bigint): { sec: number; nsec: number } {
  return { sec: Number(ns / 1_000_000_000n), nsec: Number(ns % 1_000_000_000n) };
}

function replacer(_key: string, value: unknown): unknown {
  return typeof value === "bigint" ? value.toString() : value;
}
