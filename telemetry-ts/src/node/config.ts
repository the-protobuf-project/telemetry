/**
 * Config from disk and the environment, as the other SDKs find it:
 * `TELEMETRY_CONFIG_PATH`, else telemetry.{toml,yaml,yml,json} in the working
 * directory, else in .config/; then `.env`, then `TELEMETRY_*` variables.
 */

import { existsSync, readFileSync } from "node:fs";
import { extname, join } from "node:path";
import { parseEnv } from "node:util";
import { parse as parseToml } from "smol-toml";
import { parse as parseYaml } from "yaml";
import { type ConfigTree, envConfig } from "../core/options.ts";

const extensions = ["toml", "yaml", "yml", "json"];

/** The config file to read: the one asked for, or the first one found. */
export function findConfig(
  explicit: string | undefined,
  env: Record<string, string | undefined>,
  cwd = process.cwd(),
): string | undefined {
  if (explicit) return explicit;
  if (env.TELEMETRY_CONFIG_PATH) return env.TELEMETRY_CONFIG_PATH;
  for (const dir of [cwd, join(cwd, ".config")]) {
    for (const ext of extensions) {
      const path = join(dir, `telemetry.${ext}`);
      if (existsSync(path)) return path;
    }
  }
  return undefined;
}

export function parseConfigFile(path: string): ConfigTree {
  const text = readFileSync(path, "utf8");
  const ext = extname(path).slice(1).toLowerCase();
  const tree: unknown =
    ext === "json"
      ? JSON.parse(text)
      : ext === "yaml" || ext === "yml"
        ? parseYaml(text)
        : parseToml(text);
  if (typeof tree !== "object" || tree === null || Array.isArray(tree)) {
    throw new Error(`telemetry: ${path} does not hold a table of settings`);
  }
  return tree as ConfigTree;
}

/**
 * The process environment over `.env` in the working directory: a variable
 * already set wins, as dotenv loaders do.
 */
export function environment(cwd = process.cwd()): Record<string, string | undefined> {
  const path = join(cwd, ".env");
  const dotenv = existsSync(path) ? parseEnv(readFileSync(path, "utf8")) : {};
  return { ...dotenv, ...process.env };
}

/** Config trees, lowest precedence first: the file, then the environment. */
export function loadConfig(explicit: string | undefined): ConfigTree[] {
  const env = environment();
  const path = findConfig(explicit, env);
  if (explicit && !existsSync(explicit)) {
    throw new Error(`telemetry: config file ${explicit} not found`);
  }
  return [...(path ? [parseConfigFile(path)] : []), envConfig(env)];
}
