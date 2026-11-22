"""One trace through five services, one per SDK, over real HTTP (`just crosslang`).

    orders (Go) → inventory (Rust) → billing (Python) → shipping (C++) → notify (TypeScript)

Each service is the SDK's crosslang example: it listens on a free port,
prints {"listening": <port>}, and answers GET /hop with its view of the trace
followed by the views of the services after it (CROSSLANG_NEXT). None of
them injects or extracts by hand: the trace crosses each hop through the
SDK's own propagation, as a real service's would.

The Go service starts the trace and calls the first. The run fails unless
every service's span is in that trace, each received a traceparent from it,
and each made a span of its own. A service whose toolchain (uv, bazel or
npm) is missing is left out and the chain links around it.

Run from the repository root: python3 tests/crosslang.py
"""

import json
import os
import shutil
import signal
import subprocess
import sys
import tempfile
import threading
from dataclasses import dataclass
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
TIMEOUT = 120  # seconds for a service to start listening


@dataclass
class Service:
    name: str
    language: str
    cwd: str
    needs: str  # the command its toolchain provides
    build: list[str] | None
    run: list[str]


# Last first: each is started with the URL of the one after it.
SERVICES = [
    Service(
        "notify-ts",
        "TypeScript",
        "telemetry-ts",
        "npm",
        ["sh", "-c", "[ -d node_modules ] || npm ci --silent; npm run -s build"],
        ["node", "examples/crosslang.ts"],
    ),
    Service(
        "shipping-cpp",
        "C++",
        "telemetry-cpp",
        "bazel",
        [
            "bazel",
            "build",
            "--noshow_progress",
            "--ui_event_filters=-info",
            "//examples:crosslang",
        ],
        ["bazel-bin/examples/crosslang"],
    ),
    Service(
        "billing-py",
        "Python",
        "telemetry-py",
        "uv",
        ["uv", "sync", "-q"],
        ["uv", "run", "-q", "python", "examples/crosslang.py"],
    ),
    Service(
        "inventory-rs",
        "Rust",
        "telemetry-rs",
        "cargo",
        ["cargo", "build", "-q", "-p", "telemetry-examples", "--bin", "crosslang"],
        ["target/debug/crosslang"],
    ),
]


def build(service: Service, log: Path) -> None:
    if service.build is None:
        return
    with log.open("ab") as out:
        done = subprocess.run(
            service.build, cwd=ROOT / service.cwd, stdout=out, stderr=out, check=False
        )
    if done.returncode != 0:
        sys.exit(f"crosslang: building {service.name} failed; see {log}")


def start(
    service: Service, next_url: str | None, log: Path
) -> tuple[subprocess.Popen, str]:
    """Starts a service and returns it with its URL, once it is listening."""
    env = dict(os.environ)
    env.pop("CROSSLANG_NEXT", None)
    if next_url:
        env["CROSSLANG_NEXT"] = next_url
    proc = subprocess.Popen(
        service.run,
        cwd=ROOT / service.cwd,
        env=env,
        stdout=subprocess.PIPE,
        stderr=log.open("ab"),
        text=True,
        start_new_session=True,  # its own process group, to stop uv's child too
    )
    first: list[str] = []
    reader = threading.Thread(target=lambda: first.append(proc.stdout.readline()))
    reader.start()
    reader.join(TIMEOUT)
    line = first[0] if first else ""
    try:
        port = json.loads(line)["listening"]
    except (ValueError, KeyError, TypeError):
        stop(proc)
        sys.exit(f"crosslang: {service.name} did not start (got {line!r}); see {log}")
    return proc, f"http://127.0.0.1:{port}"


def stop(proc: subprocess.Popen) -> None:
    try:
        os.killpg(proc.pid, signal.SIGTERM)
        proc.wait(5)
    except (ProcessLookupError, subprocess.TimeoutExpired):
        try:
            os.killpg(proc.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass


def traceparent_parts(tp: str | None) -> tuple[str, str]:
    parts = (tp or "").split("-")
    return (parts[1], parts[2]) if len(parts) == 4 else ("", "")


def check(result: dict) -> bool:
    """Prints each hop and says whether the trace arrived whole."""
    trace = result["trace_id"]
    print(f"{'go     (orders-go)':<24} trace {trace}  span {result['span_id']}")
    whole = True
    seen = {result["span_id"]}
    for hop in result["hops"]:
        got_trace, parent = traceparent_parts(hop.get("received_traceparent"))
        problems = []
        if hop.get("trace_id") != trace:
            problems.append("started a new trace")
        if got_trace != trace:
            problems.append("received no traceparent from this trace")
        if hop.get("span_id") in seen or hop.get("span_id") == parent:
            problems.append("made no span of its own")
        seen.add(hop.get("span_id"))
        print(
            f"{hop['service']:<24} trace {hop.get('trace_id')}  span {hop.get('span_id')}"
            f"  parent {parent}"
            + (f"  BROKEN: {', '.join(problems)}" if problems else "")
        )
        whole = whole and not problems
    return whole


def main() -> int:
    logs = Path(tempfile.mkdtemp(prefix="crosslang-"))
    services = [s for s in SERVICES if shutil.which(s.needs)]
    for skipped in (s for s in SERVICES if s not in services):
        print(f"crosslang: no {skipped.needs}; leaving out {skipped.name}")
    if not shutil.which("go"):
        sys.exit("crosslang: the Go service starts the trace and needs go")

    orders = logs / "orders-go"
    for service in services:
        build(service, logs / f"{service.name}.log")
    subprocess.run(
        ["go", "build", "-o", str(orders), "./examples/crosslang"],
        cwd=ROOT / "telemetry-go",
        check=True,
    )

    running: list[subprocess.Popen] = []
    try:
        next_url = None
        for service in services:
            proc, next_url = start(service, next_url, logs / f"{service.name}.log")
            running.append(proc)
        if next_url is None:
            sys.exit("crosslang: no service to call")
        env = dict(os.environ, CROSSLANG_NEXT=next_url)
        done = subprocess.run(
            [str(orders)],
            env=env,
            capture_output=True,
            text=True,
            timeout=60,
            check=False,
        )
        if done.returncode != 0:
            print(done.stderr, file=sys.stderr)
            sys.exit(f"crosslang: the orders service failed; logs in {logs}")
        result = json.loads(done.stdout)
    finally:
        for proc in reversed(running):
            stop(proc)

    expected = len(services)
    if check(result) and len(result["hops"]) == expected:
        print(f"OK: one trace across {expected + 1} services, over HTTP")
        return 0
    print(f"BROKEN: the trace did not arrive whole; logs in {logs}", file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
