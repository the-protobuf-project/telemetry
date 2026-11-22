/**
 * Console output in the format every SDK prints, on stderr, in color when
 * stderr is a terminal and NO_COLOR is unset:
 *
 *   2026-11-22T10:15:04+05:30 INFO: <src/server.ts:42> orders: started
 */

import { relative, sep } from "node:path";
import { fileURLToPath } from "node:url";
import type { Caller, ConsoleRecord, LevelName } from "../core/platform.ts";

const gray = "\x1b[90m";
const cyan = "\x1b[36m";
const reset = "\x1b[0m";
const colors: Record<LevelName, string> = {
  DEBUG: "\x1b[34m",
  INFO: "\x1b[32m",
  WARN: "\x1b[33m",
  ERROR: "\x1b[31m",
  FATAL: "\x1b[31m",
};

let color: boolean | undefined;

export function writeConsole(r: ConsoleRecord): void {
  color ??= Boolean(process.stderr.isTTY) && !process.env.NO_COLOR;
  const paint = (c: string, s: string) => (color ? `${c}${s}${reset}` : s);
  const where = r.caller ? ` ${paint(gray, `<${r.caller.file}:${r.caller.line}>`)}` : "";
  let line = `${paint(gray, timestamp(r.time))} ${paint(colors[r.level], r.level)}:${where} ${paint(cyan, r.service.name)}: ${r.message}`;
  if (r.data && Object.keys(r.data).length) {
    const json = JSON.stringify(r.data, replacer, 2) ?? "";
    line += `\n  data=\n  │ ${json.split("\n").join("\n  │ ")}`;
  }
  process.stderr.write(`${line}\n`);
}

/** ISO 8601 in local time with its offset, as the other SDKs print it. */
function timestamp(t: Date): string {
  const pad = (n: number) => String(Math.abs(n)).padStart(2, "0");
  const offset = -t.getTimezoneOffset();
  const tz = `${offset >= 0 ? "+" : "-"}${pad(Math.trunc(offset / 60))}:${pad(offset % 60)}`;
  return (
    `${t.getFullYear()}-${pad(t.getMonth() + 1)}-${pad(t.getDate())}` +
    `T${pad(t.getHours())}:${pad(t.getMinutes())}:${pad(t.getSeconds())}${tz}`
  );
}

function replacer(_key: string, value: unknown): unknown {
  if (typeof value === "bigint") return value.toString();
  if (value instanceof Error) return { name: value.name, message: value.message };
  return value;
}

// This package's own files: frames in them are the SDK, not the caller.
const ownDir = fileURLToPath(new URL("..", import.meta.url));
// What each frame's file name is shown as, or "" for the SDK's own frames.
const shown = new Map<string, string>();

/**
 * The first stack frame outside this package: the line that called the
 * logger. Its path is shown relative to the working directory when inside it.
 *
 * V8's structured stack trace, a few frames deep: formatting the whole stack
 * as text and parsing it back was 96% of a log call.
 */
export function caller(): Caller | undefined {
  const limit = Error.stackTraceLimit;
  const prepare = Error.prepareStackTrace;
  Error.stackTraceLimit = 8;
  Error.prepareStackTrace = (_error, sites) => sites;
  const holder: { stack?: NodeJS.CallSite[] | string } = {};
  try {
    Error.captureStackTrace(holder, caller);
  } finally {
    Error.prepareStackTrace = prepare;
    Error.stackTraceLimit = limit;
  }
  const stack = holder.stack;
  // A tool that installs its own stack formatting (source maps under a test
  // runner) can hand back text instead of call sites; read the text then.
  if (!Array.isArray(stack) || typeof stack[0]?.getFileName !== "function") {
    return typeof stack === "string" ? fromText(stack) : undefined;
  }
  for (const site of stack) {
    const name = site.getFileName();
    if (!name) continue;
    const file = display(name);
    if (file) return { file, line: site.getLineNumber() ?? 0 };
  }
  return undefined;
}

function fromText(stack: string): Caller | undefined {
  for (const frame of stack.split("\n")) {
    const match = /\(?((?:file:\/\/)?[^\s()]+?):(\d+):\d+\)?$/.exec(frame);
    if (!match?.[1]) continue;
    const file = display(match[1]);
    if (file) return { file, line: Number(match[2]) };
  }
  return undefined;
}

function display(name: string): string {
  let file = shown.get(name);
  if (file === undefined) {
    const path = name.startsWith("file://") ? fileURLToPath(name) : name;
    if (path.startsWith(ownDir) || path.startsWith("node:")) {
      file = "";
    } else {
      const rel = relative(process.cwd(), path);
      file = rel.startsWith(`..${sep}`) || rel === ".." ? path : rel;
    }
    shown.set(name, file);
  }
  return file;
}
