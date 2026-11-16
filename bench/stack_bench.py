#!/usr/bin/env python3
"""Load-tests the single-image stack (docker/) end to end and reports what it
takes: the Go SDK writes N records per signal through Envoy → OTel Collector →
Loki / Tempo / Prometheus, while the container's CPU and memory are sampled
and the collector's own counters say what it accepted, refused, sent and
failed.

    just stack-bench                 # builds the image, then 1M per signal on 8 CPUs / 6 GB
    just stack-bench 1000000 16 12g  # n, cpus, memory

Standard library only; needs docker and go.
"""

import argparse
import json
import socket
import subprocess
import threading
import time
import urllib.parse
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
NAME = "telemetry-stack-bench"


def free_port():
    s = socket.socket()
    s.bind(("127.0.0.1", 0))
    port = s.getsockname()[1]
    s.close()
    return port


def get(url, timeout=5):
    with urllib.request.urlopen(url, timeout=timeout) as r:
        return r.read().decode()


def prom(port, expr):
    """One PromQL value; patient, since the stack is under load when asked."""
    q = urllib.parse.urlencode({"query": expr})
    for attempt in range(10):
        try:
            data = json.loads(get(f"http://127.0.0.1:{port}/metrics/api/v1/query?{q}", timeout=30))
            return sum(float(r["value"][1]) for r in data["data"]["result"])
        except OSError:
            time.sleep(3)
    raise SystemExit(f"Prometheus did not answer: {expr}")


def wait_ready(port, deadline=180):
    end = time.time() + deadline
    while time.time() < end:
        try:
            out = subprocess.run(
                ["docker", "exec", NAME, "sh", "-c",
                 "curl -s localhost:3101/ready; curl -s localhost:3201/ready; curl -s localhost:4040/ready"],
                capture_output=True, text=True).stdout
            if out.count("ready") >= 3 and "not ready" not in out.lower():
                return
        except OSError:
            pass
        time.sleep(2)
    raise SystemExit("stack did not become ready")


class Sampler(threading.Thread):
    """Peak CPU (%) and memory (MiB) of the container while it runs."""

    def __init__(self):
        super().__init__(daemon=True)
        self.cpu = self.mem = 0.0
        self.stop = threading.Event()

    def run(self):
        while not self.stop.is_set():
            out = subprocess.run(["docker", "stats", "--no-stream", "--format", "{{json .}}", NAME],
                                 capture_output=True, text=True).stdout.strip()
            if out:
                s = json.loads(out)
                self.cpu = max(self.cpu, float(s["CPUPerc"].rstrip("%")))
                used = s["MemUsage"].split("/")[0].strip()
                num, unit = float(used[:-3]), used[-3:]
                self.mem = max(self.mem, num * {"KiB": 1 / 1024, "MiB": 1, "GiB": 1024}.get(unit, 1))


def counters(port, signal):
    kind = {"logs": "log_records", "spans": "spans", "metrics": "metric_points"}[signal]
    return {
        "accepted": prom(port, f"sum(otelcol_receiver_accepted_{kind}_total)"),
        "refused": prom(port, f"sum(otelcol_receiver_refused_{kind}_total)"),
        "sent": prom(port, f"sum(otelcol_exporter_sent_{kind}_total)"),
        "failed": prom(port, f"sum(otelcol_exporter_send_failed_{kind}_total)"),
    }


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--image", default="telemetry-stack:dev")
    ap.add_argument("--cpus", default="8")
    ap.add_argument("--memory", default="6g")
    ap.add_argument("--n", type=int, default=1_000_000)
    ap.add_argument("--exporters", type=int, default=4)
    ap.add_argument("--signals", default="logs,spans,metrics")
    args = ap.parse_args()

    loadgen = Path("/tmp") / "telemetry-loadgen-stack"
    subprocess.run(["go", "build", "-o", str(loadgen), "./cmd/telemetry-loadgen"], cwd=ROOT / "telemetry-go", check=True)

    port = free_port()
    subprocess.run(["docker", "rm", "-f", NAME], capture_output=True)
    subprocess.run(["docker", "run", "-d", "--name", NAME, "--cpus", args.cpus, "--memory", args.memory,
                    "-p", f"127.0.0.1:{port}:6009", args.image], check=True, capture_output=True)
    results = []
    try:
        wait_ready(port)
        time.sleep(15)  # Prometheus' first scrape of the collector
        for sig in args.signals.split(","):
            before = counters(port, sig)
            sampler = Sampler()
            sampler.start()
            t0 = time.time()
            out = subprocess.run(
                [str(loadgen), "-json", "-signals", sig, "-n", str(args.n), "-endpoint", f"127.0.0.1:{port}",
                 "-lossless", "-batch", "8192", "-queue", "65536", "-exporters", str(args.exporters),
                 "-shutdown", "300s"],
                capture_output=True, text=True, check=True).stdout
            r = json.loads(out.strip().splitlines()[-1])
            # Let the collector drain and Prometheus scrape the result.
            last = None
            for _ in range(60):
                time.sleep(5)
                now = counters(port, sig)
                if now == last:
                    break
                last = now
            sampler.stop.set()
            delta = {k: last[k] - before[k] for k in last}
            r.update(signal=sig, wall_s=time.time() - t0, peak_cpu_pct=sampler.cpu, peak_mem_mib=sampler.mem, **delta)
            results.append(r)
            print(json.dumps(r))
    finally:
        subprocess.run(["docker", "rm", "-f", NAME], capture_output=True)

    print()
    print(f"| Signal | Records | Emit rate | Collector accepted | Sent to backend | Refused | Failed | Peak CPU ({args.cpus} cores) | Peak memory |")
    print("| ------ | ------: | --------: | -----------------: | --------------: | ------: | -----: | ---------: | ----------: |")
    for r in results:
        print(f"| {r['signal']} | {args.n:,} | {args.n / r['emit_s']:,.0f}/s | {r['accepted']:,.0f} | {r['sent']:,.0f} | "
              f"{r['refused']:,.0f} | {r['failed']:,.0f} | {r['peak_cpu_pct'] / 100:.1f} cores | {r['peak_mem_mib']:,.0f} MiB |")


if __name__ == "__main__":
    main()
