/**
 * Telemetry for Node.js: logs, metrics, traces and profiles over
 * OpenTelemetry, with MCAP recording, configured from telemetry.toml.
 *
 *   const tel = Telemetry.builder("orders", "1.0.0").build();
 *   tel.logger.info("started");
 *   await tel.close();
 */

import { LogLevel, type TelemetryOptions } from "./core/options.ts";
import type { Platform } from "./core/platform.ts";
import { Telemetry as Base, TelemetryBuilder } from "./core/telemetry.ts";
import { nodePlatform } from "./node/platform.ts";

export * from "./core/index.ts";
export { TelemetryBuilder };

export class Telemetry extends Base {
  /** A builder that reads telemetry.toml, then `.env` and `TELEMETRY_*` variables, then its own settings. */
  static new(): TelemetryBuilder {
    return new TelemetryBuilder(
      nodePlatform,
      (opts: TelemetryOptions, platform: Platform, level: LogLevel = LogLevel.Unset) =>
        new Telemetry(opts, platform, level),
    );
  }

  /** `Telemetry.new().withService(name, version)`. */
  static builder(name: string, version: string): TelemetryBuilder {
    return Telemetry.new().withService(name, version);
  }
}
