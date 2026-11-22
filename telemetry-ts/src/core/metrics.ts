/**
 * Counters, histograms and gauges by name, exported over OTLP and recorded
 * to MCAP. Instruments are made on first use and kept, so a hot path pays
 * for one map lookup per call.
 */

import type { Attributes, Counter, Gauge, Histogram, Meter } from "@opentelemetry/api";
import type { Recorder } from "./platform.ts";

export type Labels = Record<string, string | number | boolean>;

export class Metrics {
  readonly #meter: Meter | undefined;
  readonly #recorder: Recorder | undefined;
  readonly #labels: Labels;
  readonly #counters = new Map<string, Counter>();
  readonly #histograms = new Map<string, Histogram>();
  readonly #gauges = new Map<string, Gauge>();

  constructor(meter: Meter | undefined, recorder: Recorder | undefined, labels: Labels) {
    this.#meter = meter;
    this.#recorder = recorder;
    this.#labels = labels;
  }

  /** Adds `value` (default 1) to a monotonic counter. */
  counter(name: string, value = 1, labels?: Labels): void {
    if (this.#meter) {
      let c = this.#counters.get(name);
      if (!c) {
        c = this.#meter.createCounter(name);
        this.#counters.set(name, c);
      }
      c.add(value, this.#attributes(labels));
    }
    this.#record(name, value);
  }

  /**
   * Records `value` in a histogram. `buckets` sets its explicit bucket
   * boundaries; they are fixed by the first call for each name.
   */
  histogram(name: string, value: number, labels?: Labels, buckets?: number[]): void {
    if (this.#meter) {
      let h = this.#histograms.get(name);
      if (!h) {
        const advice = buckets ? { explicitBucketBoundaries: buckets } : undefined;
        h = this.#meter.createHistogram(name, { advice });
        this.#histograms.set(name, h);
      }
      h.record(value, this.#attributes(labels));
    }
    this.#record(name, value);
  }

  /** Sets a gauge to `value`. */
  gauge(name: string, value: number, labels?: Labels): void {
    if (this.#meter) {
      let g = this.#gauges.get(name);
      if (!g) {
        g = this.#meter.createGauge(name);
        this.#gauges.set(name, g);
      }
      g.record(value, this.#attributes(labels));
    }
    this.#record(name, value);
  }

  #attributes(labels: Labels | undefined): Attributes {
    return labels ? { ...this.#labels, ...labels } : this.#labels;
  }

  #record(name: string, value: number): void {
    this.#recorder?.metric(name, value, BigInt(Date.now()) * 1_000_000n);
  }
}
