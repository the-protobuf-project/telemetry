/**
 * gRPC tracing for grpc-js: every call a client makes gets a CLIENT span and
 * carries the trace in its metadata, and every handler a server registers
 * runs in a SERVER span that continues the caller's.
 *
 * The official instrumentation wraps the client constructors, which an ESM
 * program holds unpatched copies of, and generated clients keep the request
 * methods they were made with. What every call does look up when it is made
 * is grpc-js's interceptor chain, so the client side wraps that: each call
 * gets one more interceptor, outermost. Servers are hooked where every
 * handler is registered.
 *
 * Both this package's grpc-js and the application's are hooked when they
 * differ. Register services after building Telemetry: a handler registered
 * before is not wrapped.
 */

import { createRequire } from "node:module";
import { dirname, join } from "node:path";
import {
  context,
  propagation,
  ROOT_CONTEXT,
  type Span,
  SpanKind,
  SpanStatusCode,
  type TextMapGetter,
  type TextMapSetter,
  type TracerProvider,
  trace,
} from "@opentelemetry/api";
import { isTracingSuppressed } from "@opentelemetry/core";

interface GrpcMetadata {
  get(key: string): Array<string | Buffer>;
  set(key: string, value: string): void;
  getMap(): Record<string, unknown>;
  clone(): GrpcMetadata;
}

interface Status {
  code: number;
  details?: string;
}

/** The part of an interceptor's requester used here (grpc-js's `Requester`). */
interface Requester {
  start(
    metadata: GrpcMetadata,
    listener: unknown,
    next: (
      metadata: GrpcMetadata,
      listener: { onReceiveStatus(status: Status, next: (status: Status) => void): void },
    ) => void,
  ): void;
}

interface GrpcModule {
  Server: { prototype: Record<string, unknown> };
  InterceptingCall: new (next: unknown, requester: Requester) => unknown;
}

type Fn = (this: unknown, ...args: unknown[]) => unknown;

const OK = 0;
const CANCELLED = 1;
const UNKNOWN = 2;

/** OTLP's own services: exports, and a collector in this process, are not traced. */
const otlpService = /^\/?opentelemetry\.proto\.collector\./;

const setter: TextMapSetter<GrpcMetadata> = {
  set: (md, key, value) => md.set(key, value),
};

const getter: TextMapGetter<GrpcMetadata> = {
  keys: (md) => Object.keys(md.getMap()),
  get: (md, key) => {
    const values = md.get(key).map(String);
    return values.length > 1 ? values : values[0];
  },
};

/** `/pkg.Service/Method` as the semantic conventions name and describe it. */
function describe(path: string) {
  const [service = "", method = ""] = path.replace(/^\//, "").split("/");
  return {
    name: `${service}/${method}`,
    attributes: { "rpc.system": "grpc", "rpc.service": service, "rpc.method": method },
  };
}

function finish(span: Span, code: number, message?: string): void {
  span.setAttribute("rpc.grpc.status_code", code);
  if (code !== OK) span.setStatus({ code: SpanStatusCode.ERROR, ...(message && { message }) });
  span.end();
}

interface Copy {
  grpc: GrpcModule;
  /** grpc-js's client-interceptors module, which builds each call's chain. */
  interceptors: Record<string, unknown>;
}

/** The grpc-js copies to hook: this package's, and the application's if it has its own. */
function copies(): Copy[] {
  const found = new Map<string, Copy>();
  for (const from of [import.meta.url, join(process.cwd(), "noop.js")]) {
    try {
      const require = createRequire(from);
      const main = require.resolve("@grpc/grpc-js");
      if (found.has(main)) continue;
      found.set(main, {
        grpc: require(main),
        interceptors: require(join(dirname(main), "client-interceptors.js")),
      });
    } catch {
      // No grpc-js resolvable from there.
    }
  }
  return [...found.values()];
}

export function instrumentGrpc(provider: TracerProvider): () => void {
  const tracer = provider.getTracer("@the-protobuf-project/telemetry/grpc");
  const restore: Array<() => void> = [];
  const patch = (target: Record<string, unknown>, key: string, wrap: (original: Fn) => Fn) => {
    const original = target[key];
    if (typeof original !== "function") return;
    target[key] = wrap(original as Fn);
    restore.push(() => {
      target[key] = original;
    });
  };

  for (const { grpc, interceptors } of copies()) {
    // getInterceptingCall(interceptorArgs, methodDefinition, callOptions, channel),
    // called as each call is made, in the caller's context.
    patch(
      interceptors,
      "getInterceptingCall",
      (original) =>
        function (this: unknown, ...args: unknown[]) {
          const call = original.apply(this, args);
          const path = String((args[1] as { path?: unknown } | undefined)?.path ?? "");
          const active = context.active();
          if (otlpService.test(path) || isTracingSuppressed(active)) return call;
          const { name, attributes } = describe(path);
          const span = tracer.startSpan(name, { kind: SpanKind.CLIENT, attributes }, active);
          const ctx = trace.setSpan(active, span);
          return new grpc.InterceptingCall(call, {
            start(metadata, _listener, next) {
              // The caller's metadata is copied, not changed.
              const md = metadata.clone();
              propagation.inject(ctx, md, setter);
              next(md, {
                onReceiveStatus(status, nextStatus) {
                  finish(span, status.code, status.details);
                  nextStatus(status);
                },
              });
            },
          });
        },
    );

    patch(
      grpc.Server.prototype,
      "register",
      (register) =>
        function (this: unknown, name: unknown, handler: unknown, ...rest: unknown[]) {
          const path = String(name);
          if (otlpService.test(path) || typeof handler !== "function") {
            return register.call(this, name, handler, ...rest);
          }
          const type = rest[2];
          const { name: spanName, attributes } = describe(path);
          const wrapped = function (this: unknown, call: unknown, callback?: Fn) {
            const c = call as {
              metadata: GrpcMetadata;
              once(
                event: string,
                listener: (e?: { code?: number; message?: string }) => void,
              ): void;
            };
            const parent = propagation.extract(ROOT_CONTEXT, c.metadata, getter);
            const span = tracer.startSpan(spanName, { kind: SpanKind.SERVER, attributes }, parent);
            const ctx = trace.setSpan(parent, span);
            let ended = false;
            const end = (code: number, message?: string) => {
              if (ended) return;
              ended = true;
              finish(span, code, message);
            };
            c.once("cancelled", () => end(CANCELLED, "cancelled"));
            if (type === "unary" || type === "clientStream") {
              const done = function (this: unknown, ...result: unknown[]) {
                const err = result[0] as { code?: number; message?: string } | null | undefined;
                end(err ? (err.code ?? UNKNOWN) : OK, err?.message);
                return callback?.apply(this, result);
              };
              return context.with(ctx, () => (handler as Fn).call(this, call, done));
            }
            c.once("finish", () => end(OK));
            c.once("error", (e) => end(e?.code ?? UNKNOWN, e?.message));
            return context.with(ctx, () => (handler as Fn).call(this, call));
          };
          return register.call(this, name, wrapped, ...rest);
        },
    );
  }
  return () => {
    for (const undo of restore.reverse()) undo();
  };
}
