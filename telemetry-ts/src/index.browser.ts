/**
 * Telemetry for the browser: logs, metrics and traces over OTLP/HTTP, with
 * the same API as Node. Configure in code or with `withConfigObject`.
 *
 *   const tel = Telemetry.builder("web", "1.0.0").withOtlpEndpoint("https://otel.example.com").build();
 *   const headers = tel.tracing.withSpan("checkout", () => tel.tracing.inject());
 */

import { browserPlatform } from "./browser/platform.ts";
import { LogLevel, type TelemetryOptions } from "./core/options.ts";
import type { Platform } from "./core/platform.ts";
import { Telemetry as Base, TelemetryBuilder } from "./core/telemetry.ts";

export * from "./core/index.ts";
export { TelemetryBuilder };

export class Telemetry extends Base {
  /** A builder configured from code and `withConfigObject` only: a browser has no config file or environment. */
  static new(): TelemetryBuilder {
    return new TelemetryBuilder(
      browserPlatform,
      (opts: TelemetryOptions, platform: Platform, level: LogLevel = LogLevel.Unset) =>
        new Telemetry(opts, platform, level),
    );
  }

  /** `Telemetry.new().withService(name, version)`. */
  static builder(name: string, version: string): TelemetryBuilder {
    return Telemetry.new().withService(name, version);
  }
}
