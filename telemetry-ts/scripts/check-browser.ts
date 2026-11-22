// Bundles the browser entry the way a web app's bundler would and fails if
// any Node code got into it: a node: built-in, gRPC, MCAP file writing or the
// config-file parsers. Run after `npm run build`.

import { build } from "esbuild";

const nodeOnly = [
  /^node:/,
  /^(fs|path|os|http|https|net|tls|util|module|url|child_process|worker_threads|async_hooks)$/,
  /@grpc\//,
  /exporter-[a-z]+-otlp-grpc/,
  /context-async-hooks/,
  /@mcap\//,
  /smol-toml|\/yaml\//,
  /[\\/]src[\\/]node[\\/]|[\\/]dist[\\/]node[\\/]/,
];

const result = await build({
  entryPoints: [new URL("../dist/index.browser.js", import.meta.url).pathname],
  bundle: true,
  platform: "browser",
  format: "esm",
  write: false,
  minify: true,
  metafile: true,
  logLevel: "silent",
}).catch((error: { errors?: { text: string }[] }) => {
  // A Node built-in the browser cannot resolve fails the build outright.
  console.error("browser bundle does not build:");
  for (const e of error.errors ?? []) console.error(`  ${e.text}`);
  process.exit(1);
});

const inputs = Object.keys(result.metafile.inputs);
const leaked = inputs.filter((path) => nodeOnly.some((re) => re.test(path)));
const external = Object.values(result.metafile.outputs).flatMap((o) =>
  o.imports.filter((i) => i.external).map((i) => i.path),
);
const bytes = result.outputFiles.reduce((n, f) => n + f.contents.length, 0);

if (leaked.length || external.length) {
  console.error("browser bundle pulls in Node code:");
  for (const path of [...leaked, ...external]) console.error(`  ${path}`);
  process.exit(1);
}
console.log(
  `browser bundle OK: ${inputs.length} modules, ${(bytes / 1024).toFixed(0)} KiB minified`,
);
