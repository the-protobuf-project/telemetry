#!/usr/bin/env python3
"""Load-tests every SDK the same way and writes docs/benchmark.md.

Each SDK writes N log records, N spans and N counter increments over OTLP
(gRPC; HTTP for C++) to one counting collector (telemetry-go's
`telemetry-loadgen -serve`), so the table compares what each SDK *delivered*,
not only how fast it returned. Beside every SDK, the same load goes through the
upstream OpenTelemetry SDK for that language alone, for comparison.

    just bench                                # 1,000,000 per signal, all SDKs
    just bench 100000 --langs go,rust

Needs go, cargo, uv and bazel (C++) on PATH. Standard library only.
"""

import argparse
import json
import math
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
OTLP_HTTP = "127.0.0.1:43180"  # the same collector over OTLP/HTTP, for C++ and Python
COUNTS = "127.0.0.1:43200"
OFFICIAL = "official OpenTelemetry SDK alone (default batch processor)"

# The official SDKs tuned through the standard variables every one of them
# reads: a queue that holds the whole burst, so they drop nothing either, and
# the same batch size this SDK uses in lossless mode.
TUNED = {
    "OTEL_BLRP_MAX_QUEUE_SIZE": "1048576",
    "OTEL_BLRP_MAX_EXPORT_BATCH_SIZE": "8192",
    "OTEL_BSP_MAX_QUEUE_SIZE": "1048576",
    "OTEL_BSP_MAX_EXPORT_BATCH_SIZE": "8192",
}
# Exporters this SDK runs per signal in the head-to-head.
EXPORTERS = {"Go": 4, "Rust": 8, "Python": 4, "C++": 8}


def sh(cmd, cwd, **kw):
    return subprocess.run(cmd, cwd=cwd, check=True, **kw)


def build(langs, scratch):
    loadgen = scratch / "telemetry-loadgen"
    sh(["go", "build", "-o", str(loadgen), "./cmd/telemetry-loadgen"], ROOT / "telemetry-go")
    sh(["go", "build", "-o", str(scratch / "otel-baseline-go"), "./cmd/otel-baseline"], ROOT / "telemetry-go")
    if "rust" in langs:
        sh(["cargo", "build", "--release", "-q", "-p", "telemetry-examples", "--bin", "loadgen",
            "--bin", "otel_baseline"], ROOT / "telemetry-rs")
    if "python" in langs:
        sh(["uv", "sync", "-q"], ROOT / "telemetry-py")
    if "cpp" in langs:
        sh(["bazel", "build", "-c", "opt", "//examples:loadgen", "//examples:otel_baseline"], ROOT / "telemetry-cpp")
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
    """(sdk, mode, short label for charts, command builder, extra env) for each run."""
    out = []
    if "go" in langs:
        go = lambda *extra: lambda sig, n: [str(loadgen), "-json", "-signals", sig, "-n", str(n), "-endpoint", OTLP, *extra]
        out += [
            ("Go", "default (drop when full), 1 goroutine", "Go drop", go("-workers", "1"), {}),
            ("Go", "Lossless, 1 goroutine", "Go lossless", go("-workers", "1", "-lossless"), {}),
            ("Go", f"Lossless, {workers} goroutines, batch 8192", f"Go lossless x{workers}",
             go("-workers", str(workers), "-lossless", "-batch", "8192", "-queue", "65536"), {}),
            ("Go", f"Lossless, {workers} goroutines, batch 8192, 4 exporters", "Go 4 exporters",
             go("-workers", str(workers), "-lossless", "-batch", "8192", "-queue", "65536", "-exporters", "4"), {}),
        ]
    if "rust" in langs:
        rust = ROOT / "telemetry-rs" / "target" / "release" / "loadgen"
        cmd = lambda sig, n: [str(rust), sig, str(n), OTLP]
        out += [
            ("Rust", "default (drop when full)", "Rust drop", cmd, {}),
            ("Rust", "Lossless", "Rust lossless", cmd, {"LOADGEN_LOSSLESS": "1"}),
        ]
    if "python" in langs:
        cmd = lambda sig, n: ["uv", "run", "-q", "--project", str(ROOT / "telemetry-py"),
                              "python", str(ROOT / "telemetry-py" / "examples" / "loadgen.py"), sig, str(n), OTLP_HTTP]
        # Free-threaded Python (3.14t) exports over HTTP: grpcio has no build for it.
        out += [
            ("Python", "default (drop when full)", "Python drop", cmd, {"LOADGEN_HTTP": "1"}),
            ("Python", "Lossless", "Python lossless", cmd, {"LOADGEN_HTTP": "1", "LOADGEN_LOSSLESS": "1"}),
        ]
    if "cpp" in langs:
        cpp = ROOT / "telemetry-cpp" / "bazel-bin" / "examples"
        out.append(("C++", "default (drop when full)", "C++ drop",
                    lambda sig, n: [str(cpp / "loadgen"), sig, str(n), OTLP_HTTP], {}))

    # The same load through each language's upstream OpenTelemetry SDK alone.
    scratch = loadgen.parent
    official = {
        "go": ("Go", lambda sig, n: [str(scratch / "otel-baseline-go"), "-signal", sig, "-n", str(n), "-endpoint", OTLP]),
        "rust": ("Rust", lambda sig, n: [str(ROOT / "telemetry-rs" / "target" / "release" / "otel_baseline"), sig, str(n), OTLP]),
        "python": ("Python", lambda sig, n: ["uv", "run", "-q", "--project", str(ROOT / "telemetry-py"), "python",
                                             str(ROOT / "telemetry-py" / "examples" / "otel_baseline.py"), sig, str(n),
                                             OTLP_HTTP]),
        "cpp": ("C++", lambda sig, n: [str(ROOT / "telemetry-cpp" / "bazel-bin" / "examples" / "otel_baseline"),
                                       sig, str(n), OTLP_HTTP]),
    }
    for key, (lang, cmd) in official.items():
        if key in langs:
            out.append((lang, OFFICIAL, f"{lang} OTel SDK", cmd, {"OTEL_BASELINE_HTTP": "1"} if key == "python" else {}))
    return out


def run_one(cmd, timeout, extra_env=None):
    env = dict(os.environ, OTEL_EXPORTER_OTLP_ENABLED="true", RUST_LOG="off", **(extra_env or {}))
    p = subprocess.run(cmd, check=False, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True, timeout=timeout, env=env)
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


DATA = ROOT / "docs" / "benchmarks.json"


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--n", type=int, default=1_000_000)
    ap.add_argument("--langs", default="go,rust,python,cpp")
    ap.add_argument("--signals", default="logs,spans,metrics")
    ap.add_argument("--timeout", type=int, default=1800, help="seconds per run")
    ap.add_argument("--out", default=str(ROOT / "docs" / "BENCHMARKS.md"))
    ap.add_argument("--data", default=str(DATA), help="where the raw results are kept")
    ap.add_argument("--no-mcap", action="store_true", help="skip the MCAP recording runs")
    ap.add_argument("--rounds", type=int, default=3, help="runs per head-to-head configuration; the median is kept")
    ap.add_argument("--render", action="store_true", help="rebuild the page from --data without running anything")
    args = ap.parse_args()
    data_path = Path(args.data)

    if args.render:
        write(json.loads(data_path.read_text()), args.out)
        print(f"wrote {args.out} from {data_path}", file=sys.stderr)
        return

    langs = set(args.langs.split(","))
    workers = os.cpu_count() or 8
    scratch = Path(os.environ.get("TMPDIR", "/tmp")) / "telemetry-bench"
    scratch.mkdir(exist_ok=True)
    loadgen = build(langs, scratch)
    collector = subprocess.Popen([str(loadgen), "-serve", OTLP, "-counts", COUNTS, "-http", OTLP_HTTP],
                                 stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    results = []
    try:
        for _ in range(50):
            try:
                http("GET", "/counts")
                break
            except OSError:
                time.sleep(0.1)
        for lang, mode, label, cmd, env in variants(langs, loadgen, workers):
            for sig in args.signals.split(","):
                http("POST", "/reset")
                print(f"{lang:6} {sig:7} {mode} ...", end=" ", flush=True, file=sys.stderr)
                try:
                    r = run_one(cmd(sig, args.n), args.timeout, env)
                except (subprocess.TimeoutExpired, RuntimeError) as e:
                    print(f"failed: {e}", file=sys.stderr)
                    results.append({"lang": lang, "mode": mode, "label": label, "signal": sig, "error": str(e)[:80]})
                    continue
                got = delivered(sig)
                r.update(lang=lang, mode=mode, label=label, delivered=got)
                results.append(r)
                print(f"{got}/{args.n} delivered, {args.n / r['emit_s']:,.0f}/s", file=sys.stderr)
        versus = head_to_head(langs, loadgen, args.n, args.rounds, workers)
    finally:
        collector.terminate()

    mcap = [] if args.no_mcap else mcap_runs(langs, loadgen, args, scratch)
    micro = go_micro() if "go" in langs else []
    # Measurements made outside this script (the exporter sweep, the stack
    # test) are kept from the previous data file.
    previous = json.loads(data_path.read_text()) if data_path.exists() else {}
    data = {
        "n": args.n,
        "date": date.today().isoformat(),
        "platform": f"{platform.system()} {platform.machine()}",
        "cpus": workers,
        "python": sdk_python(),
        "results": results,
        "mcap": mcap,
        "micro": [list(m) for m in micro],
        "exporter_sweep": previous.get("exporter_sweep"),
        "head_to_head": versus,
    }
    data_path.write_text(json.dumps(data, indent=2) + "\n")
    write(data, args.out)
    print(f"wrote {args.out} and {data_path}", file=sys.stderr)


# The page


def sdk_python():
    """The Python the Python SDK runs on, "3.14.10t" when free-threaded."""
    try:
        out = subprocess.run(
            ["uv", "run", "-q", "--project", str(ROOT / "telemetry-py"), "python", "-c",
             ("import sys; g = getattr(sys, '_is_gil_enabled', lambda: True)(); "
              "print(sys.version.split()[0] + ('' if g else 't'))")],
            capture_output=True, text=True, check=True)
        return out.stdout.strip()
    except (OSError, subprocess.CalledProcessError):
        return platform.python_version()


def head_to_head(langs, loadgen, n, rounds, workers):
    """Each SDK against its tuned official SDK, lossless both, on one thread and on
    one per CPU: the median of `rounds` runs of goodput, records delivered per
    second of emitting and flushing."""
    scratch = loadgen.parent
    py = ["uv", "run", "-q", "--project", str(ROOT / "telemetry-py"), "python"]
    rs = ROOT / "telemetry-rs" / "target" / "release"
    cpp = ROOT / "telemetry-cpp" / "bazel-bin" / "examples"
    go_ours = lambda sig, w: ([str(loadgen), "-json", "-signals", sig, "-n", str(n), "-endpoint", OTLP, "-workers", str(w),
                               "-lossless", "-batch", "8192", "-queue", "65536", "-exporters", str(EXPORTERS["Go"])], {})
    go_off = lambda sig, w: ([str(scratch / "otel-baseline-go"), "-signal", sig, "-n", str(n), "-workers", str(w),
                              "-endpoint", OTLP], TUNED)
    ours_env = lambda lang, w: {"LOADGEN_LOSSLESS": "1", "LOADGEN_WORKERS": str(w), "LOADGEN_EXPORTERS": str(EXPORTERS[lang])}
    off_env = lambda w: dict(TUNED, OTEL_BASELINE_WORKERS=str(w))
    sdks = {
        "go": ("Go", go_off, go_ours),
        "rust": ("Rust", lambda sig, w: ([str(rs / "otel_baseline"), sig, str(n), OTLP], off_env(w)),
                 lambda sig, w: ([str(rs / "loadgen"), sig, str(n), OTLP], ours_env("Rust", w))),
        "python": ("Python", lambda sig, w: ([*py, str(ROOT / "telemetry-py" / "examples" / "otel_baseline.py"), sig, str(n),
                                              OTLP_HTTP], dict(off_env(w), OTEL_BASELINE_HTTP="1")),
                   lambda sig, w: ([*py, str(ROOT / "telemetry-py" / "examples" / "loadgen.py"), sig, str(n), OTLP_HTTP],
                                   dict(ours_env("Python", w), LOADGEN_HTTP="1"))),
        "cpp": ("C++", lambda sig, w: ([str(cpp / "otel_baseline"), sig, str(n), OTLP_HTTP], off_env(w)),
                lambda sig, w: ([str(cpp / "loadgen"), sig, str(n), OTLP_HTTP], ours_env("C++", w))),
    }
    out = []
    for key, (lang, official, ours) in sdks.items():
        if key not in langs:
            continue
        for w in (1, workers):
            for sig in ("logs", "spans", "metrics"):
                for sdk, cmd in (("official", official), ("ours", ours)):
                    argv, env = cmd(sig, w)
                    runs = []
                    print(f"{lang:6} {sig:5} x{w:<2} {sdk:8} ...", end=" ", flush=True, file=sys.stderr)
                    for _ in range(rounds):
                        http("POST", "/reset")
                        try:
                            r = run_one(argv, 1800, env)
                        except (subprocess.TimeoutExpired, RuntimeError) as e:
                            print(f"failed: {e}", file=sys.stderr)
                            break
                        got = delivered(sig)
                        runs.append({"delivered": got, "goodput": got / (r["emit_s"] + r["close_s"])})
                    if not runs:
                        continue
                    runs.sort(key=lambda x: x["goodput"])
                    mid = runs[len(runs) // 2]
                    out.append({"lang": lang, "signal": sig, "workers": w, "sdk": sdk,
                                "exporters": EXPORTERS[lang] if sdk == "ours" else 1,
                                "delivered": mid["delivered"], "goodput": mid["goodput"],
                                "runs": [round(x["goodput"]) for x in runs]})
                    print(f"{mid['delivered']}/{n} delivered, {mid['goodput']:,.0f}/s", file=sys.stderr)
    return out


def ok(rows):
    return [r for r in rows if "error" not in r]


def xychart(title, labels, values, y_title, kind="bar"):
    """A Mermaid xychart: one series, labelled categories."""
    cats = ", ".join(f'"{l}"' for l in labels)
    vals = ", ".join(f"{v:.6g}" for v in values)
    top = max(values) * 1.1 if values and max(values) > 0 else 1
    return [
        "```mermaid",
        "xychart-beta",
        f'    title "{title}"',
        f"    x-axis [{cats}]",
        f'    y-axis "{y_title}" 0 --> {top:.6g}',
        f"    {kind} [{vals}]",
        "```",
        "",
    ]


def charts(data):
    n = data["n"]
    rows = ok(data["results"])
    out = []
    by = lambda sig: [r for r in rows if r["signal"] == sig]

    out += from_official(data)
    out += tuned(data)
    out += ["## At a glance", ""]

    # Delivery and throughput, per signal.
    for sig, unit, scale in (("logs", "thousand / s", 1e3), ("spans", "thousand / s", 1e3)):
        rs = by(sig)
        if not rs:
            continue
        labels = [r["label"] for r in rs]
        out += [f"### {sig.capitalize()}", ""]
        out += xychart(f"{sig.capitalize()} delivered of {n:,} (%)", labels,
                       [100 * r["delivered"] / n for r in rs], "% delivered")
        out += xychart(f"{sig.capitalize()} written per second", labels,
                       [n / r["emit_s"] / scale for r in rs], unit)
    rs = by("metrics")
    if rs:
        out += ["### Metrics", "",
                "Every configuration delivers every increment: a counter exports its running total.", ""]
        out += xychart("Counter increments per second", [r["label"] for r in rs],
                       [n / r["emit_s"] / 1e6 for r in rs], "million / s")

    # Speed against delivery: the configurations worth using sit top right.
    for sig in ("logs", "spans"):
        rs = by(sig)
        if not rs:
            continue
        lo, hi = 3.0, 7.0  # log10 of 1k/s .. 10M/s
        out += [f"### {sig.capitalize()}: speed against delivery", "",
                "Right is faster (log scale, 1k/s to 10M/s); up is more of the records delivered.", "",
                "```mermaid", "quadrantChart",
                f"    title {sig.capitalize()}: speed against delivery",
                "    x-axis Slow --> Fast", "    y-axis Loses records --> Delivers all",
                "    quadrant-1 Fast and lossless", "    quadrant-2 Lossless but slow",
                "    quadrant-3 Slow and lossy", "    quadrant-4 Fast but lossy"]
        for r in rs:
            x = min(max((math.log10(n / r["emit_s"]) - lo) / (hi - lo), 0.02), 0.98)
            y = min(max(r["delivered"] / n, 0.02), 0.98)
            out.append(f"    {r['label']}: [{x:.3f}, {y:.3f}]")
        out += ["```", ""]

    # Where the records went when the queue was allowed to drop.
    lossy = [r for r in rows if r["signal"] in ("logs", "spans") and r["delivered"] < n]
    if lossy:
        out += ["### Where the records went (default mode)", "",
                "With the default drop-when-full queue, a burst faster than the collector loses what",
                "does not fit; every SDK now counts it and reports it on close.", "",
                "```mermaid", "---", "config:", "  sankey:", "    showValues: false",
                "    linkColor: source", "---", "sankey-beta", ""]
        for r in lossy:
            src = f"{r['label']} {r['signal']}"
            out.append(f"{src},Delivered,{r['delivered']}")
            out.append(f"{src},Dropped,{n - r['delivered']}")
        out += ["```", ""]
        worst = max(lossy, key=lambda r: n - r["delivered"])
        out += ["```mermaid", f'pie showData title {worst["label"]}: {n:,} {worst["signal"]}',
                f'    "Delivered" : {worst["delivered"]}',
                f'    "Dropped (queue full)" : {n - worst["delivered"]}', "```", ""]

    sweep = data.get("exporter_sweep")
    if sweep:
        xs = [str(p["exporters"]) for p in sweep["points"]]
        logs = ", ".join(f"{p['logs'] / 1e3:.0f}" for p in sweep["points"])
        spans = ", ".join(f"{p['spans'] / 1e3:.0f}" for p in sweep["points"])
        top = max(p["spans"] for p in sweep["points"]) / 1e3 * 1.1
        out += ["### Go: more exporters, more throughput", "",
                f"{sweep['note']} Upper line spans, lower line logs (thousand / s).", "",
                "```mermaid", "xychart-beta", '    title "Lossless throughput by number of exporters"',
                f"    x-axis \"exporters\" [{', '.join(xs)}]", f'    y-axis "thousand / s" 0 --> {top:.0f}',
                f"    line [{spans}]", f"    line [{logs}]", "```", ""]

    mcap = ok(data.get("mcap", []))
    if mcap:
        name = {"go": "Go", "rust": "Rust", "python": "Python"}
        recorded = [r for r in mcap if r["recorded"]]
        labels = [f"{name[r['lang']]} {r['signal']}" for r in recorded]
        out += ["### MCAP recording", ""]
        out += xychart("MCAP records written per second", labels,
                       [n / r["emit_s"] / 1e3 for r in recorded], "thousand / s")
        out += xychart("MCAP bytes per record (zstd)", labels,
                       [r["size"] / r["recorded"] for r in recorded], "bytes")

    micro = data.get("micro", [])
    if micro:
        out += ["### Go: cost of one call", ""]
        out += xychart("Go SDK nanoseconds per call", [m[0].removeprefix("Benchmark") for m in micro],
                       [float(m[1]) for m in micro], "ns / op")
    return out


def tuned(data):
    """Each SDK against its official SDK tuned to drop nothing."""
    rows = data.get("head_to_head") or []
    if not rows:
        return []
    n = data["n"]
    by = {(r["lang"], r["signal"], r["workers"], r["sdk"]): r for r in rows}
    pairs = []
    for (lang, sig, w, sdk), r in by.items():
        if sdk == "ours" and (lang, sig, w, "official") in by:
            pairs.append((lang, sig, w, by[(lang, sig, w, "official")], r))
    order = {"Go": 0, "Rust": 1, "Python": 2, "C++": 3}
    pairs.sort(key=lambda p: (p[2], order.get(p[0], 9), p[1]))
    most = max(p[2] for p in pairs)
    out = ["## Head to head: the official SDKs tuned", "",
           "Defaults are not the official SDKs' best. Here each one is tuned through the standard",
           "`OTEL_BLRP_*` and `OTEL_BSP_*` variables to a queue that holds the whole burst and batches",
           "of 8192, so it drops nothing, and this SDK runs lossless with several exporters per signal",
           (f"(Go {EXPORTERS['Go']}, Rust {EXPORTERS['Rust']}, Python {EXPORTERS['Python']}, "
            f"C++ {EXPORTERS['C++']}). Both get the same "
            f"{n:,} records from the same number of threads."),
           "**Goodput** is records that reached the collector per second, emitting and flushing included;",
           "each figure is the median of several runs.", ""]
    for w in sorted({p[2] for p in pairs}):
        ps = [p for p in pairs if p[2] == w]
        out += xychart(f"Goodput vs the tuned official SDK, {w} thread{'s' if w > 1 else ''} (x)",
                       [f"{p[0]} {p[1]}" for p in ps], [p[4]["goodput"] / p[3]["goodput"] for p in ps], "x official")
    out += ["| Language | Signal | Threads | Official, tuned | This SDK | Delivered (both) | vs official |",
            "| -------- | ------ | ------: | --------------: | -------: | ---------------: | ----------: |"]
    for lang, sig, w, off, ours in pairs:
        both = f"{off['delivered']:,} / {ours['delivered']:,}"
        out.append(f"| {lang} | {sig} | {w} | {off['goodput']:,.0f}/s | {ours['goodput']:,.0f}/s | {both} | "
                   f"**{ours['goodput'] / off['goodput']:.2f}x** |")
    out += [""]
    short = [p for p in pairs if p[2] == most]
    short = [f"{p[0]} {p[1]}" for p in short if p[4]["goodput"] < 2 * p[3]["goodput"]]
    if short:
        out += [f"Under 2x: {', '.join(short)}."]
        if any(x.startswith("C++ spans") for x in short):
            out += ["C++ spans are bounded inside opentelemetry-cpp's tracer: every span copies",
                    "reference-counted pointers that all threads share, in the official SDK and this one alike."]
        out += [""]
    return out


def from_official(data):
    """The starting point: what each upstream OpenTelemetry SDK delivers alone, then each step
    this SDK takes from there."""
    n = data["n"]
    rows = ok(data["results"])
    official = [r for r in rows if r["mode"] == OFFICIAL and r["signal"] in ("logs", "spans")]
    if not official:
        return []
    out = ["## Starting point: the official SDKs", "",
           "Each language's upstream OpenTelemetry SDK alone, with its default batch processor and",
           "OTLP exporter, given the same burst. The calls return fast, and most of the records never",
           "arrive: the batch processor's 2048-slot queue fills and drops the rest without a word.", ""]
    for sig in ("logs", "spans"):
        rs = [r for r in official if r["signal"] == sig]
        if rs:
            out += xychart(f"Official SDKs: {sig} delivered of {n:,} (%)", [r["lang"] for r in rs],
                           [100 * r["delivered"] / n for r in rs], "% delivered")

    steps = [("Go OTel SDK", "Official Go SDK alone"),
             ("Go drop", "This SDK, default: drops counted and reported"),
             ("Go lossless", "Lossless: the writer waits for room"),
             (next((r["label"] for r in rows if r["label"].startswith("Go lossless x")), ""),
              "Lossless, one goroutine per CPU, batch 8192"),
             ("Go 4 exporters", "Lossless, plus 4 exporters in parallel")]
    go = {(r["label"], r["signal"]): r for r in rows if r["lang"] == "Go"}
    steps = [(label, what) for label, what in steps if (label, "logs") in go and (label, "spans") in go]
    if len(steps) > 1:
        out += ["### Step by step, in Go", "",
                "Each step changes one thing, starting from the official SDK. The last row delivers every",
                "record at the official SDK's speed for logs and twice it for spans. The official run uses",
                "one thread, so read this as what each SDK can be made to deliver, not a like-for-like race.", "",
                "| Step | Logs delivered | Logs / s | Spans delivered | Spans / s |",
                "| ---- | -------------: | -------: | --------------: | --------: |"]
        for label, what in steps:
            lg, sp = go[(label, "logs")], go[(label, "spans")]
            out.append(f"| {what} | {lg['delivered']:,} | {n / lg['emit_s']:,.0f} | "
                       f"{sp['delivered']:,} | {n / sp['emit_s']:,.0f} |")
        out += [""]
        for sig in ("logs", "spans"):
            out += xychart(f"Go {sig}: delivered (%) at each step", [str(i) for i in range(1, len(steps) + 1)],
                           [100 * go[(label, sig)]["delivered"] / n for label, _ in steps], "% delivered")
            out += xychart(f"Go {sig}: written per second at each step", [str(i) for i in range(1, len(steps) + 1)],
                           [n / go[(label, sig)]["emit_s"] / 1e3 for label, _ in steps], "thousand / s", kind="line")
    out += versus_official(data)
    return out


def versus_official(data):
    """Each SDK against its language's upstream OpenTelemetry SDK alone."""
    n = data["n"]
    rows = ok(data["results"])
    official = {(r["lang"], r["signal"]): r for r in rows if r["mode"] == OFFICIAL}
    if not official:
        return []
    out = ["### Every language against its official SDK", "",
           "This SDK's default and lossless modes beside the official SDK, one thread each. Above 1",
           "means faster per call than the official SDK; the official SDK's speed is bought with the",
           "records it drops.", ""]
    table = ["| Language | Signal | Official: delivered | Official: rate | This SDK, drop: delivered | rate | Lossless: delivered | rate |",
             "| -------- | ------ | ------------------: | -------------: | ------------------------: | ---: | ------------------: | ---: |"]
    for sig in ("logs", "spans", "metrics"):
        labels, ratios = [], []
        for lang in ("Go", "Rust", "Python", "C++"):
            off = official.get((lang, sig))
            if not off:
                continue
            mine = [r for r in rows if r["lang"] == lang and r["signal"] == sig and r["mode"] != OFFICIAL]
            drop = next((r for r in mine if "drop" in r["label"]), None)
            lossless = next((r for r in mine if "lossless" in r["label"].lower() and "x" not in r["label"]
                             and "exporters" not in r["label"]), None)
            fmt = lambda r: (f"{r['delivered']:,}", f"{n / r['emit_s']:,.0f}/s") if r else ("—", "—")
            table.append(f"| {lang} | {sig} | {fmt(off)[0]} | {fmt(off)[1]} | {fmt(drop)[0]} | {fmt(drop)[1]} | "
                         f"{fmt(lossless)[0]} | {fmt(lossless)[1]} |")
            if drop:
                labels.append(lang)
                ratios.append(off["emit_s"] / drop["emit_s"])
        if labels:
            out += xychart(f"{sig.capitalize()}: calls per second vs the official SDK (x)", labels, ratios,
                           "x official")
    out += table + [""]
    return out


def write(data, out_path):
    n = data["n"]
    results = data["results"]
    lines = [
        "# Benchmarks",
        "",
        f"Every SDK writes **{n:,}** log records, spans and counter increments over OTLP/gRPC to one",
        "in-process counting collector, so the numbers show what each SDK **delivered**, not only how",
        "fast the calls returned. Regenerate with `just bench`; redraw this page from the saved",
        "numbers (`docs/benchmark.json`) with `just bench-render`.",
        "",
        f"Measured {data['date']} on {data['platform']}, {data['cpus']} CPUs, Python {data['python']}.",
        "Console log output is off in every load generator (Go formats it to io.Discard), so the terminal is",
        "not what is measured. **drop** is the default: a full export queue drops what does not fit,",
        "counted and reported on close. **lossless** makes the writer wait for room instead.",
        "",
    ]
    lines += charts(data)
    lines += [
        "## OTLP export",
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
        "program wait (lossless). Every SDK counts what it dropped and reports it on close.",
        "",
    ]
    mcap = data.get("mcap", [])
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
        lines.append("")
    micro = data.get("micro", [])
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
        lines += [f"| `{m[0]}` | {m[1]} | {m[2]} | {m[3]} |" for m in micro]
        lines.append("")
    Path(out_path).write_text("\n".join(lines))


if __name__ == "__main__":
    main()
