/**
 * Accounting for every log record and span handed to OTLP export.
 *
 * The OpenTelemetry batch processors drop what overflows their queue (2048
 * by default) with no more than a debug message, so a burst can lose most of
 * its records unnoticed. Here every record is counted when the program emits
 * it, and again when it is exported, fails or is dropped; `Telemetry.close()`
 * reports whatever never reached the collector.
 */

/** What happened to one signal's records. */
export interface SignalStats {
  /** Handed to the pipeline by the program. */
  emitted: number;
  /** Accepted by the collector. */
  exported: number;
  /** Thrown away because the queue was full. */
  dropped: number;
  /** In an export request that failed. */
  failed: number;
  /** Queued or in flight: not exported, dropped or failed yet. */
  pending: number;
}

/** What the export pipeline did with the program's log records and spans. */
export interface Stats {
  logs: SignalStats;
  spans: SignalStats;
  /** Failed export requests, every signal. */
  exportErrors: number;
  /** The latest of them. */
  lastError: string;
}

/** One signal's counters, shared by its processor and exporters. */
export class Counters {
  emitted = 0;
  exported = 0;
  dropped = 0;
  failed = 0;

  stats(): SignalStats {
    const pending = this.emitted - this.exported - this.dropped - this.failed;
    return {
      emitted: this.emitted,
      exported: this.exported,
      dropped: this.dropped,
      failed: this.failed,
      pending,
    };
  }
}

/** The errors from every failed export request, across signals. */
export class ExportErrors {
  count = 0;
  last = "";

  record(error: unknown): void {
    this.count++;
    this.last = error instanceof Error ? error.message : String(error);
  }
}

/** Describes the records that never reached the collector, or undefined. */
export function lossReport(logs: SignalStats, spans: SignalStats): string | undefined {
  const parts: string[] = [];
  for (const [name, s] of [
    ["log records", logs],
    ["spans", spans],
  ] as const) {
    // Whatever was still pending at close never left the process either.
    const dropped = s.dropped + s.pending;
    if (dropped) parts.push(`${dropped} of ${s.emitted} ${name} dropped (export queue full)`);
    if (s.failed) parts.push(`${s.failed} of ${s.emitted} ${name} failed to export`);
  }
  if (!parts.length) return undefined;
  let report = `telemetry: ${parts.join("; ")}`;
  if (logs.dropped + logs.pending + spans.dropped + spans.pending) {
    report += "; use withExport({ lossless: true }) or a larger queueSize to keep them";
  }
  return report;
}
