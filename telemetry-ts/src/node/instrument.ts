/**
 * Node's automatic instrumentation: the HTTP and HTTPS clients and servers,
 * `fetch` (undici) and gRPC, so a trace crosses every request this process
 * makes or answers without code at the call.
 *
 * The official HTTP instrumentation patches the module a `require` returns,
 * which an ESM program never makes, so it is required here once enabled and
 * the ESM bindings resynced: `import http from "node:http"` and
 * `import { request } from "node:http"` then both see the patched functions.
 * `fetch` is traced through diagnostics_channel and needs neither. gRPC is
 * hooked on the grpc-js prototypes (./grpc.ts) for the same ESM reason.
 */

import { createRequire, syncBuiltinESMExports } from "node:module";
import type { Instrumentation } from "@opentelemetry/instrumentation";
import { HttpInstrumentation } from "@opentelemetry/instrumentation-http";
import { UndiciInstrumentation } from "@opentelemetry/instrumentation-undici";
import type { InstrumentArgs } from "../core/platform.ts";
import { instrumentGrpc } from "./grpc.ts";

const require = createRequire(import.meta.url);

/** OTLP's own paths: a collector in this process is not traced into itself. */
const otlpPath = /^\/v1\/(traces|logs|metrics)\b/;

// One of each for the process: the instrumentation library shares one
// require hook, which returns a module it has seen from its cache, so a
// second HttpInstrumentation would never get to patch node:http. Each
// Telemetry hands these its providers and enables them again; re-enabled,
// they patch the module they already hold.
let builtins: Instrumentation[] | undefined;

export function instrument({ tracerProvider, meterProvider, tracing }: InstrumentArgs): () => void {
  builtins ??= [
    new HttpInstrumentation({ ignoreIncomingRequestHook: (req) => otlpPath.test(req.url ?? "") }),
    new UndiciInstrumentation(),
  ];
  const instrumentations = [...builtins, ...tracing.instrumentations];
  for (const i of instrumentations) {
    i.setTracerProvider(tracerProvider);
    if (meterProvider) i.setMeterProvider(meterProvider);
    i.enable();
  }
  require("node:http");
  require("node:https");
  syncBuiltinESMExports();
  const ungrpc = instrumentGrpc(tracerProvider);
  return () => {
    ungrpc();
    for (const i of instrumentations) i.disable();
    syncBuiltinESMExports();
  };
}
