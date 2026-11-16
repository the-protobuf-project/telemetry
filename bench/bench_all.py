#!/usr/bin/env python3
"""Load-tests every SDK the same way and writes BENCHMARKS.md.

Each SDK writes N log records, N spans and N counter increments over OTLP/gRPC
to one counting collector (telemetry-go's `telemetry-loadgen -serve`), so the
table compares what each SDK *delivered*, not only how fast it returned.

    bench/bench_all.py                      # 1,000,000 per signal, all SDKs
    bench/bench_all.py --n 100000 --langs go,rust

Needs go, cargo and uv on PATH. Standard library only.
"""

import argparse
import json
import os
import platform
import subprocess
import sys
import time
import urllib.request
from datetime import date
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
OTLP = "127.0.0.1:43170"  # away from 4317, so a running local stack is untouched
COUNTS = "127.0.0.1:43200"


def sh(cmd, cwd, **kw):
    return subprocess.run(cmd, cwd=cwd, check=True, **kw)


def build(langs, scratch):
    loadgen = scratch / "telemetry-loadgen"
    sh(["go", "build", "-o", str(loadgen), "./cmd/telemetry-loadgen"], ROOT / "telemetry-go")
    if "rust" in langs:
        sh(["cargo", "build", "--release", "-q", "-p", "telemetry-examples", "--bin", "loadgen"], ROOT / "telemetry-rs")
    if "python" in langs:
        sh(["uv", "sync", "-q"], ROOT / "telemetry-py")
    return loadgen


def http(method, path):
    req = urllib.request.Request(f"http://{COUNTS}{path}", method=method)
    with urllib.request.urlopen(req, timeout=5) as r:
        body = r.read()
    return json.loads(body) if body else None


def delivered(signal):
    """What the collector has for signal once it stops changing."""
    last, stable = None, 0
    deadline = time.time() + 60
    while time.time() < deadline:
        c = http("GET", "/counts")
        sums = c.get("Sums") or {}
        now = {
            "logs": c["Logs"],
            "spans": c["Spans"],
            "metrics": int(max((v for k, v in sums.items() if "ops" in k), default=0)),
        }[signal]
        stable = stable + 1 if now == last else 0
        if stable >= 3:
            return now
        last = now
        time.sleep(0.5)
    return last


def variants(langs, loadgen, workers):
    """(label, mode, command builder) for each run."""
    out = []
    if "go" in langs:
        go = lambda *extra: lambda sig, n: [str(loadgen), "-json", "-signals", sig, "-n", str(n), "-endpoint", OTLP, *extra]
        out += [
            ("Go", "default (drop when full), 1 goroutine", go("-workers", "1")),
            ("Go", "Lossless, 1 goroutine", go("-workers", "1", "-lossless")),
            ("Go", f"Lossless, {workers} goroutines, batch 8192", go("-workers", str(workers), "-lossless", "-batch", "8192", "-queue", "65536")),
            ("Go", f"Lossless, {workers} goroutines, batch 8192, 4 exporters",
             go("-workers", str(workers), "-lossless", "-batch", "8192", "-queue", "65536", "-exporters", "4")),
        ]
    if "rust" in langs:
        rust = ROOT / "telemetry-rs" / "target" / "release" / "loadgen"
        out.append(("Rust", "default (SDK batch processor)", lambda sig, n: [str(rust), sig, str(n), OTLP]))
    if "python" in langs:
        out.append(("Python", "default (SDK batch processor)",
                    lambda sig, n: ["uv", "run", "-q", "--project", str(ROOT / "telemetry-py"),
                                    "python", str(ROOT / "telemetry-py" / "examples" / "loadgen.py"), sig, str(n), OTLP]))
    return out


def run_one(cmd, timeout, extra_env=None):
    env = dict(os.environ, OTEL_EXPORTER_OTLP_ENABLED="true", RUST_LOG="off", **(extra_env or {}))
    p = subprocess.run(cmd, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True, timeout=timeout, env=env)
    for line in reversed(p.stdout.splitlines()):
        if line.startswith("{") and '"lang"' in line:
            return json.loads(line)
    raise RuntimeError(f"no result from {' '.join(cmd)} (exit {p.returncode})")


def mcap_count(path):
    """Messages in an MCAP file by signal, read back with the mcap library."""
    out = subprocess.run(["uv", "run", "-q", "--project", str(ROOT / "telemetry-py"), "python",
                          str(ROOT / "bench" / "mcap_count.py"), str(path)],
                         capture_output=True, text=True, check=True).stdout
    return json.loads(out)


def mcap_runs(langs, loadgen, args, scratch):
    """Each SDK records N records per signal to an MCAP file with OTLP off;
    the file is then read back, so "recorded" is what Foxglove would see."""
    topic = {"logs": "logs", "spans": "traces", "metrics": "metrics"}
    rows = []
    for lang in [l for l in ("go", "rust", "python") if l in langs]:
        for sig in args.signals.split(","):
            path = scratch / f"{lang}-{sig}.mcap"
            path.unlink(missing_ok=True)
            env = {"LOADGEN_MCAP": str(path)}
            if lang == "go":
                cmd = [str(loadgen), "-json", "-signals", sig, "-n", str(args.n), "-workers", "1",
                       "-otlp=false", "-mcap", str(path)]
            elif lang == "rust":
                cmd = [str(ROOT / "telemetry-rs" / "target" / "release" / "loadgen"), sig, str(args.n), "none"]
            else:
                cmd = ["uv", "run", "-q", "--project", str(ROOT / "telemetry-py"), "python",
                       str(ROOT / "telemetry-py" / "examples" / "loadgen.py"), sig, str(args.n), ":none"]
            print(f"{lang:6} {sig:7} MCAP ...", end=" ", flush=True, file=sys.stderr)
            try:
                r = run_one(cmd, args.timeout, env)
                got = mcap_count(path)[topic[sig]] if path.exists() else 0
                r.update(lang=lang, signal=sig, recorded=got, size=path.stat().st_size if path.exists() else 0)
                print(f"{got}/{args.n} recorded, {args.n / r['emit_s']:,.0f}/s", file=sys.stderr)
            except (subprocess.TimeoutExpired, RuntimeError, subprocess.CalledProcessError) as e:
                print(f"failed: {e}", file=sys.stderr)
                r = {"lang": lang, "signal": sig, "error": str(e)[:60]}
            rows.append(r)
            path.unlink(missing_ok=True)
    return rows


def go_micro():
    """go test benchmarks: per-call cost with and without export."""
    p = subprocess.run(["go", "test", "-run", "^$", "-bench", ".", "-benchtime", "1s", "-cpu", "1,8", "."],
                       cwd=ROOT / "telemetry-go", stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True, check=True)
    rows = []
    for line in p.stdout.splitlines():
        if line.startswith("Benchmark"):
            f = line.split()
            rows.append((f[0], f[2], f[4] if len(f) > 4 else "", f[6] if len(f) > 6 else ""))
    return rows


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--n", type=int, default=1_000_000)
    ap.add_argument("--langs", default="go,rust,python")
    ap.add_argument("--signals", default="logs,spans,metrics")
    ap.add_argument("--timeout", type=int, default=1800, help="seconds per run")
    ap.add_argument("--out", default=str(ROOT / "BENCHMARKS.md"))
    ap.add_argument("--no-mcap", action="store_true", help="skip the MCAP recording runs")
    args = ap.parse_args()
    langs = set(args.langs.split(","))
    workers = os.cpu_count() or 8

    scratch = Path(os.environ.get("TMPDIR", "/tmp")) / "telemetry-bench"
    scratch.mkdir(exist_ok=True)
    loadgen = build(langs, scratch)
    collector = subprocess.Popen([str(loadgen), "-serve", OTLP, "-counts", COUNTS],
                                 stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    results = []
    try:
        for _ in range(50):
            try:
                http("GET", "/counts")
                break
            except OSError:
                time.sleep(0.1)
        for lang, mode, cmd in variants(langs, loadgen, workers):
            for sig in args.signals.split(","):
                http("POST", "/reset")
                print(f"{lang:6} {sig:7} {mode} ...", end=" ", flush=True, file=sys.stderr)
                try:
                    r = run_one(cmd(sig, args.n), args.timeout)
                except (subprocess.TimeoutExpired, RuntimeError) as e:
                    print(f"failed: {e}", file=sys.stderr)
                    results.append({"lang": lang, "mode": mode, "signal": sig, "error": str(e)[:80]})
                    continue
                got = delivered(sig)
                r.update(lang=lang, mode=mode, delivered=got)
                results.append(r)
                print(f"{got}/{args.n} delivered, {args.n / r['emit_s']:,.0f}/s", file=sys.stderr)
    finally:
        collector.terminate()

    mcap = [] if args.no_mcap else mcap_runs(langs, loadgen, args, scratch)
    micro = go_micro() if "go" in langs else []
    write(args, results, micro, workers, mcap)
    (scratch / "results.json").write_text(json.dumps(results, indent=2))
    print(f"wrote {args.out}", file=sys.stderr)


def write(args, results, micro, workers, mcap=()):
    n = args.n
    lines = [
        "# Benchmarks",
        "",
        f"Every SDK writes **{n:,}** log records, spans and counter increments over OTLP/gRPC to one",
        "in-process counting collector, so the table shows what each SDK **delivered**, not only how",
        "fast the calls returned. Regenerate with `bench/bench_all.py`.",
        "",
        f"Measured {date.today().isoformat()} on {platform.system()} {platform.machine()}, {workers} CPUs,",
        f"Python {platform.python_version()}. Console log output is discarded (Go) or sent to /dev/null",
        "(Rust, Python), so the terminal is not what is measured.",
        "",
        "| SDK | Mode | Signal | Delivered | Lost | Emit rate | Per call | Flush on close |",
        "| --- | ---- | ------ | --------: | ---: | --------: | -------: | -------------: |",
    ]
    for r in results:
        if "error" in r:
            lines.append(f"| {r['lang']} | {r['mode']} | {r['signal']} | — | — | — | — | {r['error']} |")
            continue
        lost = 100 * (n - r["delivered"]) / n
        lines.append(
            f"| {r['lang']} | {r['mode']} | {r['signal']} | {r['delivered']:,} | {lost:.1f}% | "
            f"{n / r['emit_s']:,.0f}/s | {1e9 * r['emit_s'] / n:,.0f} ns | {r['close_s']:.2f} s |"
        )
    lines += [
        "",
        "**Lost** is what never reached the collector. Metrics are aggregated in-process (a counter",
        "exports its running total), so they cannot lose increments; logs and spans are exported one by",
        "one through a bounded queue, and a queue that fills either drops (default) or makes the",
        "program wait (`ExportOptions.Lossless`, Go). Go reports every drop in `Telemetry.Stats()` and",
        "from `Close()`; the Rust and Python SDKs still drop silently when their queue (2048 by",
        "default; `OTEL_BSP_MAX_QUEUE_SIZE` / `OTEL_BLRP_MAX_QUEUE_SIZE`) overflows.",
        "",
    ]
    if mcap:
        lines += [
            "## MCAP recording",
            "",
            f"The same {n:,} records per signal, recorded to an MCAP file for Foxglove with OTLP off,",
            "then read back with the `mcap` library: **Recorded** is what the file holds.",
            "",
            "| SDK | Signal | Recorded | Write rate | Per call | File size | Bytes / record |",
            "| --- | ------ | -------: | ---------: | -------: | --------: | -------------: |",
        ]
        for r in mcap:
            lang = {"go": "Go", "rust": "Rust", "python": "Python"}[r["lang"]]
            if "error" in r:
                lines.append(f"| {lang} | {r['signal']} | — | — | — | — | {r['error']} |")
                continue
            per = r["size"] / r["recorded"] if r["recorded"] else 0
            lines.append(
                f"| {lang} | {r['signal']} | {r['recorded']:,} | {n / r['emit_s']:,.0f}/s | "
                f"{1e9 * r['emit_s'] / n:,.0f} ns | {r['size'] / 1048576:,.1f} MiB | {per:,.0f} |"
            )
        lines += [
            "",
            "A row with 0 recorded is a missing feature, not a loss: the Rust SDK records logs and",
            "metrics to MCAP but has no MCAP path for spans yet.",
            "",
        ]
    if micro:
        lines += [
            "## Go: cost per call",
            "",
            "`go test -bench . ./telemetry-go` — `export=false` is the SDK's own overhead with nothing",
            "exported; `export=true` exports to an in-process collector. `-8` runs 8 goroutines in parallel.",
            "",
            "| Benchmark | ns/op | B/op | allocs/op |",
            "| --------- | ----: | ---: | --------: |",
        ]
        lines += [f"| `{name}` | {ns} | {b} | {a} |" for name, ns, b, a in micro]
        lines.append("")
    Path(args.out).write_text("\n".join(lines))


if __name__ == "__main__":
    main()
