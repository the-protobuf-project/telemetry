/**
 * Distributed tracing: spans, and the W3C `traceparent`, `tracestate` and
 * `baggage` headers that carry a trace from one service to the next in any
 * of the SDKs' languages.
 */

import {
  type Span as ApiSpan,
  type Attributes,
  type AttributeValue,
  type Context,
  context,
  type Link,
  propagation,
  type SpanContext,
  SpanKind,
  SpanStatusCode,
  type TextMapPropagator,
  type TimeInput,
  type Tracer,
  trace,
} from "@opentelemetry/api";
import type { ReadableSpan, SpanProcessor } from "@opentelemetry/sdk-trace";
import type { Recorder } from "./platform.ts";

export { SpanKind };

export interface SpanOptions {
  attributes?: Attributes;
  /** The span to start under: a remote caller's, from `extract`. Defaults to the active context. */
  parent?: Context;
  kind?: SpanKind;
  links?: Link[];
  startTime?: TimeInput;
}

/** A started span; end it with `end()`, or let `withSpan` do it. */
export class Span {
  /** The OpenTelemetry span underneath. */
  readonly raw: ApiSpan;
  /** The context with this span active: the parent for child spans, and what `inject` sends. */
  readonly context: Context;
  readonly traceId: string;
  readonly spanId: string;
  /** The parent's span id, or "" for a root span. */
  readonly parentSpanId: string;

  constructor(raw: ApiSpan, parent: Context) {
    this.raw = raw;
    this.context = trace.setSpan(parent, raw);
    const sc = raw.spanContext();
    this.traceId = sc.traceId;
    this.spanId = sc.spanId;
    this.parentSpanId = trace.getSpanContext(parent)?.spanId ?? "";
  }

  setAttribute(key: string, value: AttributeValue): this {
    this.raw.setAttribute(key, value);
    return this;
  }

  setAttributes(attributes: Attributes): this {
    this.raw.setAttributes(attributes);
    return this;
  }

  addEvent(name: string, attributes?: Attributes): this {
    this.raw.addEvent(name, attributes);
    return this;
  }

  /** Records the exception as an event and marks the span failed. */
  recordException(error: unknown): this {
    const e = error instanceof Error ? error : new Error(String(error));
    this.raw.recordException(e);
    this.raw.setStatus({ code: SpanStatusCode.ERROR, message: e.message });
    return this;
  }

  setStatus(code: SpanStatusCode, message?: string): this {
    this.raw.setStatus({ code, ...(message === undefined ? {} : { message }) });
    return this;
  }

  end(endTime?: TimeInput): void {
    this.raw.end(endTime);
  }
}

export class Tracing {
  readonly #tracer: Tracer;
  readonly #propagator: TextMapPropagator;

  constructor(tracer: Tracer, propagator: TextMapPropagator) {
    this.#tracer = tracer;
    this.#propagator = propagator;
  }

  /**
   * Starts a span under `opts.parent`, or under the active span. It is not
   * made active: run code in it with `withSpan`, or `context.with(span.context, fn)`.
   */
  startSpan(name: string, opts: SpanOptions = {}): Span {
    const parent = opts.parent ?? context.active();
    const raw = this.#tracer.startSpan(
      name,
      {
        kind: opts.kind ?? SpanKind.INTERNAL,
        ...(opts.attributes && { attributes: opts.attributes }),
        ...(opts.links && { links: opts.links }),
        ...(opts.startTime !== undefined && { startTime: opts.startTime }),
      },
      parent,
    );
    return new Span(raw, parent);
  }

  /**
   * Runs `fn` in a new, active span and ends it when `fn` returns or its
   * promise settles. A throw or rejection is recorded on the span and passed
   * on. Log records and child spans made inside belong to it.
   */
  withSpan<T>(name: string, fn: (span: Span) => T, opts: SpanOptions = {}): T {
    const span = this.startSpan(name, opts);
    const fail = (error: unknown) => {
      span.recordException(error);
      span.end();
      throw error;
    };
    let result: T;
    try {
      result = context.with(span.context, fn, undefined, span);
    } catch (error) {
      return fail(error);
    }
    if (result instanceof Promise) {
      return result.then((value) => {
        span.end();
        return value;
      }, fail) as T;
    }
    span.end();
    return result;
  }

  /**
   * Writes the trace context (and baggage) of `ctx`, by default the active
   * one, into `carrier` for the next service: HTTP headers, gRPC metadata or
   * message headers. Returns the carrier.
   */
  inject(
    carrier: Record<string, string> = {},
    ctx: Context | Span = context.active(),
  ): Record<string, string> {
    this.#propagator.inject(ctx instanceof Span ? ctx.context : ctx, carrier, {
      set: (c, k, v) => {
        c[k] = v;
      },
    });
    return carrier;
  }

  /**
   * Reads the trace context a caller sent, to start spans under it with
   * `{ parent }`. Header names are matched case-insensitively.
   */
  extract(headers: Record<string, string | string[] | undefined>): Context {
    const lower: Record<string, string> = {};
    for (const [k, v] of Object.entries(headers)) {
      if (v !== undefined) lower[k.toLowerCase()] = Array.isArray(v) ? v.join(",") : v;
    }
    return this.#propagator.extract(context.active(), lower, {
      get: (c, k) => c[k],
      keys: (c) => Object.keys(c),
    });
  }

  /**
   * Returns `ctx` (default: the active context) with `key=value` added to
   * its W3C baggage, which travels with the trace to every service
   * downstream. Run code in it with `context.with`, or pass it to `inject`.
   */
  setBaggage(key: string, value: string, ctx: Context = context.active()): Context {
    const bag = propagation.getBaggage(ctx) ?? propagation.createBaggage();
    return propagation.setBaggage(ctx, bag.setEntry(key, { value }));
  }

  /** The W3C baggage in `ctx`, by default the active context. */
  baggage(ctx: Context = context.active()): Record<string, string> {
    const bag = propagation.getBaggage(ctx);
    return Object.fromEntries((bag?.getAllEntries() ?? []).map(([k, e]) => [k, e.value]));
  }

  /** The active span's trace id, or undefined outside a span. */
  currentTraceId(): string | undefined {
    return valid(trace.getSpanContext(context.active()))?.traceId;
  }

  /** The active span's id, or undefined outside a span. */
  currentSpanId(): string | undefined {
    return valid(trace.getSpanContext(context.active()))?.spanId;
  }
}

function valid(sc: SpanContext | undefined): SpanContext | undefined {
  return sc && trace.isSpanContextValid(sc) ? sc : undefined;
}

/** Writes each ended span to the recording. */
export class RecordingSpanProcessor implements SpanProcessor {
  readonly #recorder: Recorder;

  constructor(recorder: Recorder) {
    this.#recorder = recorder;
  }

  onStart(): void {}

  onEnd(span: ReadableSpan): void {
    const [sec, nsec] = span.startTime;
    this.#recorder.span({
      timeNanos: BigInt(sec) * 1_000_000_000n + BigInt(nsec),
      traceId: span.spanContext().traceId,
      spanId: span.spanContext().spanId,
      parentSpanId: span.parentSpanContext?.spanId ?? "",
      name: span.name,
      attributes: { ...span.attributes },
    });
  }

  forceFlush(): Promise<void> {
    return Promise.resolve();
  }

  shutdown(): Promise<void> {
    return Promise.resolve();
  }
}
