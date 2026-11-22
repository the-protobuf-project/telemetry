/**
 * The export queue for log records and spans, in place of the OpenTelemetry
 * batch processors.
 *
 * Every record is counted in and out. A full queue drops the record and
 * counts it, or, when lossless, keeps it: JavaScript cannot make the writer
 * wait without stalling the exporters on the same thread, so a lossless
 * queue grows instead, and `ready()` lets a writer that wants backpressure
 * await room. Up to `exporters` batches are in flight at once, one per
 * exporter.
 */

import { type Context, TraceFlags } from "@opentelemetry/api";
import { type ExportResult, ExportResultCode } from "@opentelemetry/core";
import type { LogRecordProcessor, ReadWriteLogRecord } from "@opentelemetry/sdk-logs";
import type { ReadableSpan, SpanProcessor } from "@opentelemetry/sdk-trace";
import type { Counters, ExportErrors } from "./accounting.ts";
import type { ExportOptions } from "./options.ts";

/** The shape the OpenTelemetry span and log record exporters share. */
export interface Exporter<T> {
  export(items: T[], done: (result: ExportResult) => void): void;
  shutdown(): Promise<void>;
}

export class Batcher<T> {
  readonly #opts: ExportOptions;
  readonly #counters: Counters;
  readonly #errors: ExportErrors;
  readonly #exporters: Exporter<T>[];
  readonly #idle: Exporter<T>[];
  // A queue with a moving head: shifting a large array per batch is O(n).
  #queue: T[] = [];
  #head = 0;
  #inFlight = 0;
  #timer: ReturnType<typeof setTimeout> | undefined;
  #flushing = 0;
  #pumping = false;
  #again = false;
  #closed = false;
  #onIdle: Array<() => void> = [];
  #onRoom: Array<() => void> = [];

  constructor(
    exporters: Exporter<T>[],
    opts: ExportOptions,
    counters: Counters,
    errors: ExportErrors,
  ) {
    this.#exporters = exporters;
    this.#idle = [...exporters];
    this.#opts = opts;
    this.#counters = counters;
    this.#errors = errors;
  }

  get size(): number {
    return this.#queue.length - this.#head;
  }

  add(item: T): void {
    this.#counters.emitted++;
    if (this.#closed || (!this.#opts.lossless && this.size >= this.#opts.queueSize)) {
      this.#counters.dropped++;
      return;
    }
    this.#queue.push(item);
    if (this.size >= this.#opts.batchSize) this.#pump();
    else this.#schedule();
  }

  /** Resolves once the queue has room for more records. */
  ready(): Promise<void> {
    if (this.size < this.#opts.queueSize) return Promise.resolve();
    return new Promise((resolve) => this.#onRoom.push(resolve));
  }

  /** Exports everything queued and resolves when the last batch is answered. */
  async flush(): Promise<void> {
    if (this.size === 0 && this.#inFlight === 0) return;
    this.#flushing++;
    try {
      await new Promise<void>((resolve) => {
        this.#onIdle.push(resolve);
        this.#pump();
      });
    } finally {
      this.#flushing--;
    }
  }

  /** Flushes, then refuses new records and shuts the exporters down. */
  async shutdown(): Promise<void> {
    if (this.#closed) return;
    await this.flush();
    this.#closed = true;
    clearTimeout(this.#timer);
    await Promise.all(this.#exporters.map((e) => e.shutdown()));
  }

  #schedule(): void {
    if (this.#timer !== undefined) return;
    this.#timer = setTimeout(() => {
      this.#timer = undefined;
      this.#flushing++;
      this.#pump();
      this.#flushing--;
    }, this.#opts.intervalMillis);
    // Pending records alone do not keep a Node process alive; close() sends them.
    (this.#timer as { unref?: () => void }).unref?.();
  }

  /**
   * Hands batches to idle exporters: full ones always, partial ones while
   * flushing. Exporters may answer synchronously, which re-enters here; the
   * loop picks that up instead of recursing once per batch.
   */
  #pump(): void {
    if (this.#pumping) {
      this.#again = true;
      return;
    }
    this.#pumping = true;
    try {
      do {
        this.#again = false;
        while (this.#idle.length && this.size > 0) {
          if (this.size < this.#opts.batchSize && this.#flushing === 0) break;
          this.#send(this.#idle.pop() as Exporter<T>, this.#take());
        }
      } while (this.#again);
    } finally {
      this.#pumping = false;
    }
    if (this.size > 0 && this.#inFlight === 0 && this.#flushing === 0) this.#schedule();
    this.#settle();
  }

  #take(): T[] {
    const end = Math.min(this.#head + this.#opts.batchSize, this.#queue.length);
    const batch = this.#queue.slice(this.#head, end);
    this.#head = end;
    if (this.#head === this.#queue.length) {
      this.#queue = [];
      this.#head = 0;
    } else if (this.#head > 65536 && this.#head * 2 > this.#queue.length) {
      this.#queue = this.#queue.slice(this.#head);
      this.#head = 0;
    }
    return batch;
  }

  #send(exporter: Exporter<T>, batch: T[]): void {
    this.#inFlight++;
    let answered = false;
    const done = (result: ExportResult) => {
      if (answered) return;
      answered = true;
      this.#inFlight--;
      if (result.code === ExportResultCode.SUCCESS) {
        this.#counters.exported += batch.length;
      } else {
        this.#counters.failed += batch.length;
        this.#errors.record(result.error ?? "export failed");
      }
      this.#idle.push(exporter);
      this.#pump();
    };
    try {
      exporter.export(batch, done);
    } catch (error) {
      done({ code: ExportResultCode.FAILED, error: error as Error });
    }
  }

  #settle(): void {
    if (this.#onRoom.length && this.size < this.#opts.queueSize) {
      for (const resolve of this.#onRoom.splice(0)) resolve();
    }
    if (this.#onIdle.length && this.size === 0 && this.#inFlight === 0) {
      for (const resolve of this.#onIdle.splice(0)) resolve();
    }
  }
}

/** Ended, sampled spans into a Batcher. */
export class BatchedSpanProcessor implements SpanProcessor {
  readonly #batcher: Batcher<ReadableSpan>;

  constructor(batcher: Batcher<ReadableSpan>) {
    this.#batcher = batcher;
  }

  onStart(): void {}

  onEnd(span: ReadableSpan): void {
    if (span.spanContext().traceFlags & TraceFlags.SAMPLED) this.#batcher.add(span);
  }

  forceFlush(): Promise<void> {
    return this.#batcher.flush();
  }

  shutdown(): Promise<void> {
    return this.#batcher.shutdown();
  }
}

/** Emitted log records into a Batcher. */
export class BatchedLogProcessor implements LogRecordProcessor {
  readonly #batcher: Batcher<ReadWriteLogRecord>;

  constructor(batcher: Batcher<ReadWriteLogRecord>) {
    this.#batcher = batcher;
  }

  onEmit(record: ReadWriteLogRecord, _context?: Context): void {
    this.#batcher.add(record);
  }

  forceFlush(): Promise<void> {
    return this.#batcher.flush();
  }

  shutdown(): Promise<void> {
    return this.#batcher.shutdown();
  }
}
