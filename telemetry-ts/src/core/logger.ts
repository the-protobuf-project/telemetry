/**
 * Structured logging to the console, OTLP and MCAP at once, with the trace
 * and span id of the active span on every record sent over OTLP.
 */

import {
  type Context,
  context,
  type Logger as OtelLogger,
  SeverityNumber,
} from "@opentelemetry/api";
import {
  Environment,
  LogLevel as Levels,
  type LoggingOptions,
  type LogLevel,
  type ServiceOptions,
} from "./options.ts";
import type { LevelName, Platform, Recorder } from "./platform.ts";

const severity: Record<LevelName, SeverityNumber> = {
  DEBUG: SeverityNumber.DEBUG,
  INFO: SeverityNumber.INFO,
  WARN: SeverityNumber.WARN,
  ERROR: SeverityNumber.ERROR,
  FATAL: SeverityNumber.FATAL,
};

// The lowest module level that shows each record: 1 errors, 2 info, 3 debug.
const needs: Record<LevelName, LogLevel> = {
  DEBUG: Levels.Level3,
  INFO: Levels.Level2,
  WARN: Levels.Level2,
  ERROR: Levels.Level1,
  FATAL: Levels.Level1,
};

/**
 * The level a service logs at: its `[logging.modules.<name>]` entry wins,
 * then the level set in code, then the global `logging.level`, then the
 * environment (debug in development and on Jetson, info elsewhere).
 */
export function resolveLevel(
  service: ServiceOptions,
  logging: LoggingOptions,
  code: LogLevel,
): LogLevel {
  const module = logging.modules[service.name];
  if (module) return module;
  if (code) return code;
  if (logging.level) return logging.level;
  const verbose =
    service.environment === Environment.Development || service.environment === Environment.Jetson;
  return verbose ? Levels.Level3 : Levels.Level2;
}

export interface LoggerSinks {
  service: ServiceOptions;
  level: LogLevel;
  console: boolean;
  reportCaller: boolean;
  otel: OtelLogger | undefined;
  recorder: Recorder | undefined;
  platform: Platform;
}

export class Logger {
  readonly #sinks: LoggerSinks;
  readonly #context: Context | undefined;

  constructor(sinks: LoggerSinks, ctx?: Context) {
    this.#sinks = sinks;
    this.#context = ctx;
  }

  /** The service's resolved level: 1 errors only, 2 adds info and warnings, 3 adds debug. */
  get level(): LogLevel {
    return this.#sinks.level;
  }

  /**
   * A logger whose records belong to the span in `ctx`, wherever they are
   * written: for code that runs outside the span's async context, as in the
   * browser after an `await`.
   */
  withContext(ctx: Context): Logger {
    return new Logger(this.#sinks, ctx);
  }

  debug(message: string, data?: Record<string, unknown>): void {
    this.#emit("DEBUG", message, data);
  }

  info(message: string, data?: Record<string, unknown>): void {
    this.#emit("INFO", message, data);
  }

  warn(message: string, data?: Record<string, unknown>): void {
    this.#emit("WARN", message, data);
  }

  error(message: string, data?: Record<string, unknown>): void {
    this.#emit("ERROR", message, data);
  }

  /** Logs at FATAL. Unlike Go's Fatal it does not end the process: that is the program's call. */
  fatal(message: string, data?: Record<string, unknown>): void {
    this.#emit("FATAL", message, data);
  }

  #emit(level: LevelName, message: string, data: Record<string, unknown> | undefined): void {
    const s = this.#sinks;
    if (needs[level] > s.level) return;
    const time = new Date();
    const caller = s.reportCaller ? s.platform.caller() : undefined;

    if (s.console) {
      s.platform.console({ time, level, service: s.service, message, data, caller });
    }
    if (s.otel) {
      const attributes: Record<string, unknown> = { ...s.service.labels, ...data };
      if (caller) {
        attributes["code.file.path"] = caller.file;
        attributes["code.line.number"] = caller.line;
      }
      s.otel.emit({
        timestamp: time,
        severityNumber: severity[level],
        severityText: level,
        body: message,
        attributes: attributes as never,
        context: this.#context ?? context.active(),
      });
    }
    s.recorder?.log({
      timeNanos: BigInt(time.getTime()) * 1_000_000n,
      level,
      message,
      data,
      caller,
    });
  }
}
