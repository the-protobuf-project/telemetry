import { mkdirSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { signalExport } from "../src/core/options.ts";
import { otlpUrl } from "../src/core/platform.ts";
import {
  applyConfig,
  defaultOptions,
  Environment,
  envConfig,
  LogLevel,
  Telemetry,
} from "../src/index.node.ts";
import { findConfig, parseConfigFile } from "../src/node/config.ts";

const toml = `
[service]
name = "file-service"
version = "2.0.0"
environment = "production"

[service.labels]
robot_id = "r-1"

[telemetry.otlp]
endpoint = "otel.example.com"
auth_token = "secret"

[telemetry.export]
lossless = true
batch_size = 1024
timeout = "10s"

[logging]
level = 1

[logging.modules.vision]
level = 3

[foxglove]
enabled = true
file_path = "run.mcap"
`;

function tempDir(): string {
  return mkdtempSync(join(tmpdir(), "telemetry-ts-"));
}

describe("config files", () => {
  it("maps telemetry.toml onto the options", () => {
    const dir = tempDir();
    writeFileSync(join(dir, "telemetry.toml"), toml);
    const opts = applyConfig(defaultOptions(), parseConfigFile(join(dir, "telemetry.toml")));
    expect(opts.service).toMatchObject({
      name: "file-service",
      version: "2.0.0",
      environment: Environment.Production,
      labels: { robot_id: "r-1" },
    });
    // A non-local endpoint turns export on by itself.
    expect(opts.otlp).toMatchObject({
      enabled: true,
      endpoint: "otel.example.com",
      authToken: "secret",
    });
    // Lossless raises the queue default; the batch size given still wins.
    expect(opts.export).toMatchObject({
      lossless: true,
      queueSize: 65536,
      batchSize: 1024,
      timeoutMillis: 10_000,
    });
    expect(opts.logging.level).toBe(LogLevel.Level1);
    expect(opts.logging.modules).toEqual({ vision: LogLevel.Level3 });
    expect(opts.foxglove).toEqual({ enabled: true, filePath: "run.mcap" });
  });

  it("reads YAML and JSON with the same keys", () => {
    const dir = tempDir();
    writeFileSync(join(dir, "a.yaml"), "service:\n  name: from-yaml\n");
    writeFileSync(join(dir, "b.json"), '{"service": {"name": "from-json"}}');
    expect(applyConfig(defaultOptions(), parseConfigFile(join(dir, "a.yaml"))).service.name).toBe(
      "from-yaml",
    );
    expect(applyConfig(defaultOptions(), parseConfigFile(join(dir, "b.json"))).service.name).toBe(
      "from-json",
    );
  });

  it("finds TELEMETRY_CONFIG_PATH, then the working directory, then .config/", () => {
    const dir = tempDir();
    expect(findConfig(undefined, {}, dir)).toBeUndefined();
    const nested = join(dir, ".config");
    mkdirSync(nested);
    writeFileSync(join(nested, "telemetry.yaml"), "");
    expect(findConfig(undefined, {}, dir)).toBe(join(nested, "telemetry.yaml"));
    writeFileSync(join(dir, "telemetry.toml"), "");
    expect(findConfig(undefined, {}, dir)).toBe(join(dir, "telemetry.toml"));
    expect(findConfig(undefined, { TELEMETRY_CONFIG_PATH: "/etc/t.toml" }, dir)).toBe(
      "/etc/t.toml",
    );
    expect(findConfig("given.toml", { TELEMETRY_CONFIG_PATH: "/etc/t.toml" }, dir)).toBe(
      "given.toml",
    );
  });
});

describe("environment variables", () => {
  it("accepts single and double underscores for nesting", () => {
    const tree = envConfig({
      TELEMETRY_SERVICE_NAME: "single",
      TELEMETRY_TELEMETRY_OTLP_AUTH_TOKEN: "tok",
      TELEMETRY_TELEMETRY__OTLP__ENDPOINT: "double:4317",
      TELEMETRY_TELEMETRY__EXPORT__LOSSLESS: "true",
      TELEMETRY_LOGGING_MODULES_VISION_LEVEL: "3",
      UNRELATED: "x",
    });
    const opts = applyConfig(defaultOptions(), tree);
    expect(opts.service.name).toBe("single");
    expect(opts.otlp.authToken).toBe("tok");
    expect(opts.otlp.endpoint).toBe("double:4317");
    expect(opts.export.lossless).toBe(true);
    expect(opts.logging.modules.vision).toBe(LogLevel.Level3);
  });

  it("override the file", () => {
    const opts = defaultOptions();
    applyConfig(opts, { service: { name: "file" } });
    applyConfig(opts, envConfig({ TELEMETRY_SERVICE_NAME: "env" }));
    expect(opts.service.name).toBe("env");
  });

  it("OTEL_BSP_* and OTEL_BLRP_* fill what the config leaves at its default", () => {
    const env = { OTEL_BSP_MAX_QUEUE_SIZE: "100", OTEL_BLRP_SCHEDULE_DELAY: "250" };
    const base = defaultOptions().export;
    expect(signalExport(base, env, "spans")).toMatchObject({
      queueSize: 100,
      intervalMillis: 5000,
    });
    expect(signalExport(base, env, "logs")).toMatchObject({ queueSize: 2048, intervalMillis: 250 });
    expect(signalExport({ ...base, queueSize: 7 }, env, "spans").queueSize).toBe(7);
  });
});

describe("the builder", () => {
  it("wins over the file, and withService ignores the file's service section", () => {
    const dir = tempDir();
    writeFileSync(join(dir, "t.toml"), toml);
    const opts = Telemetry.new()
      .withConfig(join(dir, "t.toml"))
      .withService("code", "3.0.0")
      .withExport({ lossless: false, queueSize: 10 })
      .resolve();
    expect(opts.service).toMatchObject({
      name: "code",
      version: "3.0.0",
      environment: Environment.Development,
      labels: {},
    });
    expect(opts.export).toMatchObject({ lossless: false, queueSize: 10, batchSize: 512 });
    expect(opts.otlp.endpoint).toBe("otel.example.com");
  });

  it("refuses a config file that is not there", () => {
    expect(() => Telemetry.new().withConfig("/nonexistent/telemetry.toml").resolve()).toThrow(
      /not found/,
    );
  });
});

describe("OTLP endpoints", () => {
  const otlp = (endpoint: string, secure = false) => ({
    ...defaultOptions().otlp,
    endpoint,
    secure,
  });

  it("adds the protocol's port and the signal's path", () => {
    expect(otlpUrl(otlp("otel.example.com"), false)).toBe("http://otel.example.com:4317");
    expect(otlpUrl(otlp("otel.example.com"), true, "/v1/logs")).toBe(
      "http://otel.example.com:4318/v1/logs",
    );
    expect(otlpUrl(otlp("otel.example.com", true), false)).toBe("https://otel.example.com:443");
    expect(otlpUrl(otlp("localhost:6009"), true, "/v1/traces")).toBe(
      "http://localhost:6009/v1/traces",
    );
  });

  it("takes TLS and a path prefix from a URL", () => {
    expect(otlpUrl(otlp("https://otel.example.com/otlp/"), true, "/v1/traces")).toBe(
      "https://otel.example.com:443/otlp/v1/traces",
    );
  });
});
