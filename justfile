# Every task in the repository, one place: `just` lists them.
#
#   just test                 test all four SDKs
#   just crosslang            one trace through Go, Rust, Python and C++
#   just bench                every SDK, 1M records per signal, OTLP and MCAP
#   just set-version 1.3.0    stamp one version into every SDK
#
# Needs: go, cargo, uv, bazel (C++), docker (image and stack recipes).

set shell := ["bash", "-euo", "pipefail", "-c"]

image_name := env("IMAGE_NAME", "ghcr.io/the-protobuf-project/telemetry")
image_tag := env("IMAGE_TAG", "1.0.0")

# List the recipes.
default:
    @just --list --unsorted

# ── test and lint ────────────────────────────────────────────────────────

# Test every SDK.
test: test-go test-rust test-py test-cpp

# Go: vet, race-test the SDK and build the isolated example module.
test-go:
    cd telemetry-go && go vet ./... && go test -race ./...
    cd telemetry-go/examples/telemetry && go build ./...

# Rust: build every target and run the tests.
test-rust:
    cd telemetry-rs && cargo build --all-targets && cargo test

# Python: lint, check formatting, and import the package.
test-py:
    cd telemetry-py && uv run ruff check . && uv run ruff format --check . && uv run python -c "import telemetry"

# C++: build everything and run the tests.
test-cpp:
    cd telemetry-cpp && bazel build //... && bazel test //tests/...

# Lint every SDK the way CI does.
lint:
    cd telemetry-go && golangci-lint run ./...
    cd telemetry-rs && cargo fmt --check
    cd telemetry-py && uv run ruff check . && uv run ruff format --check .

# Format every SDK in place.
fmt:
    cd telemetry-go && gofmt -w .
    cd telemetry-rs && cargo fmt
    cd telemetry-py && uv run ruff format .

# ── tracing across languages ─────────────────────────────────────────────

# One trace through four services, one per SDK, handed on in W3C headers:
# orders (Go) → inventory (Rust) → billing (Python) → shipping (C++). Fails if
# any hop starts a new trace. The Python and C++ hops run when uv / bazel are
# installed.
[doc('One trace through Go, Rust, Python and C++; fails if a hop starts a new trace')]
crosslang:
    #!/usr/bin/env bash
    set -euo pipefail
    field() { sed -E "s/.*\"$2\":\"([^\"]*)\".*/\1/" <<<"$1"; }

    go_out="$(cd telemetry-go && go run ./examples/crosslang 2>/dev/null)"
    trace="$(sed -E 's/.*"traceparent":"00-([0-9a-f]{32})-.*/\1/' <<<"$go_out")"
    go_span="$(sed -E 's/.*"traceparent":"00-[0-9a-f]{32}-([0-9a-f]{16})-.*/\1/' <<<"$go_out")"
    printf '%-24s trace %s  span %s\n' "go     (orders-go)" "$trace" "$go_span"
    next="$(sed -E 's/.*"traceparent":"([^"]*)".*/\1/' <<<"$go_out")"
    broken=0

    hop() { # name, result json
      local name="$1" out="$2" got parent
      got="$(field "$out" trace_id)"
      parent="$(sed -E 's/^00-[0-9a-f]{32}-([0-9a-f]{16})-.*/\1/' <<<"$(field "$out" received_traceparent)")"
      printf '%-24s trace %s  span %s  parent %s\n' "$name" "$got" "$(field "$out" span_id)" "$parent"
      [[ "$got" == "$trace" ]] || broken=1
      next="$(field "$out" downstream_traceparent)"
    }

    rust_out="$(cd telemetry-rs && printf '{"traceparent":"%s","baggage":"tenant=acme"}' "$next" \
      | cargo run -q -p telemetry-examples --bin crosslang 2>/dev/null | grep '"received_traceparent"' | tail -1)"
    hop "rust   (inventory-rs)" "$rust_out"

    if command -v uv >/dev/null; then
      py_out="$(cd telemetry-py && printf '{"traceparent":"%s"}' "$next" \
        | uv run -q python examples/crosslang.py 2>/dev/null | grep '"received_traceparent"' | tail -1)"
      hop "python (billing-py)" "$py_out"
    fi

    if command -v bazel >/dev/null; then
      cpp_out="$(cd telemetry-cpp && bazel build --noshow_progress --ui_event_filters=-info //examples:crosslang >/dev/null 2>&1 \
        && printf '{"traceparent":"%s"}' "$next" | bazel-bin/examples/crosslang 2>/dev/null | grep '"received_traceparent"' | tail -1)"
      hop "c++    (shipping-cpp)" "$cpp_out"
    fi

    if [[ -n "$trace" && "$broken" == 0 ]]; then
      echo "OK: one trace across every service"
    else
      echo "BROKEN: a hop started a new trace" >&2
      exit 1
    fi

# ── load and benchmarks ──────────────────────────────────────────────────

# Every SDK writes n records per signal over OTLP and to MCAP; writes docs/BENCHMARKS.md.
bench n="1000000" *args:
    python3 bench/bench_all.py --n {{ n }} {{ args }}

# Redraw docs/BENCHMARKS.md, charts included, from docs/benchmarks.json without running anything.
bench-render:
    python3 bench/bench_all.py --render

# Load-test the single-image stack end to end (builds the image first).
stack-bench n="1000000" cpus="8" memory="6g": image-dev
    python3 bench/stack_bench.py --n {{ n }} --cpus {{ cpus }} --memory {{ memory }}

# Write records through the Go SDK and report what arrived, with profiling, e.g.
# `just loadgen -n 1000000 -lossless -exporters 4 -cpuprofile cpu.out`.
[doc('Write records through the Go SDK and report what arrived, with profiling flags')]
loadgen *args:
    cd telemetry-go && go run ./cmd/telemetry-loadgen {{ args }}

# Run a standalone counting OTLP collector for other SDKs' load tests.
[doc("Run a standalone counting OTLP collector for other SDKs' load tests")]
collector addr="127.0.0.1:4317" counts="127.0.0.1:4320":
    cd telemetry-go && go run ./cmd/telemetry-loadgen -serve {{ addr }} -counts {{ counts }}

# Count an MCAP file's messages by signal.
mcap-count file:
    uv run -q --project telemetry-py python bench/mcap_count.py {{ file }}

# ── the single-image stack ───────────────────────────────────────────────

# Build the stack image for local use as telemetry-stack:dev.
image-dev:
    docker build -t telemetry-stack:dev docker

# Build the stack image: `local` builds opentelemetry-stack:<tag>; `amd64` /
# `arm64` build and push <image>:<tag>-<arch>; `manifest` joins the two pushed
# architectures into one multi-arch tag. IMAGE_NAME / IMAGE_TAG override the
# defaults; `docker login ghcr.io` before pushing.
[doc('Build the stack image: local, amd64, arm64 (push), or manifest')]
image arch="":
    #!/usr/bin/env bash
    set -euo pipefail
    case "{{ arch }}" in
      local)
        echo "Building opentelemetry-stack:{{ image_tag }} locally"
        docker build -f docker/Dockerfile -t "opentelemetry-stack:{{ image_tag }}" docker
        echo "Done! Built opentelemetry-stack:{{ image_tag }}" ;;
      amd64|arm64)
        tag="{{ image_tag }}-{{ arch }}"
        echo "Building {{ image_name }}:${tag} for linux/{{ arch }}"
        docker buildx build --platform "linux/{{ arch }}" -f docker/Dockerfile -t "{{ image_name }}:${tag}" --push docker
        echo "Done! Pushed {{ image_name }}:${tag}" ;;
      manifest)
        echo "Creating multi-arch manifest for {{ image_name }}:{{ image_tag }}"
        docker buildx imagetools create -t "{{ image_name }}:{{ image_tag }}" \
          "{{ image_name }}:{{ image_tag }}-amd64" "{{ image_name }}:{{ image_tag }}-arm64"
        echo "Done! Multi-arch manifest pushed." ;;
      *)
        echo "Usage: just image <amd64|arm64|manifest|local>"
        echo ""
        echo "  amd64            Build and push for linux/amd64"
        echo "  arm64            Build and push for linux/arm64"
        echo "  manifest         Combine pushed arch tags into a multi-arch image index"
        echo "  local            Build locally for testing"
        echo ""
        echo "Environment variables:"
        echo "  IMAGE_NAME       Override image name (default: ghcr.io/the-protobuf-project/telemetry)"
        echo "  IMAGE_TAG        Override version tag (default: 1.0.0)"
        echo ""
        echo "Note: 'docker login ghcr.io' before pushing (amd64/arm64/manifest)."
        exit 1 ;;
    esac

# Run the stack locally on :6009.
stack-up:
    cd telemetry-core && docker compose up -d

# Stop the local stack.
stack-down:
    cd telemetry-core && docker compose down

# ── releases ─────────────────────────────────────────────────────────────

# Stamp one release version into every SDK (Rust, Python, C++); Go's version
# is the telemetry-go/vX.Y.Z tag the release workflow pushes.
[doc('Stamp one release version into every SDK')]
set-version version:
    #!/usr/bin/env bash
    set -euo pipefail
    version="{{ version }}"
    version="${version#v}"
    if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]; then
      echo "usage: just set-version X.Y.Z[-prerelease]   (got '{{ version }}')" >&2
      exit 2
    fi
    # Replace the first match of a line-anchored regex, failing if nothing
    # matched so a renamed field cannot silently skip a language.
    set_line() {
      local file="$1" pattern="$2" replacement="$3"
      grep -Eq "$pattern" "$file" || { echo "set-version: no line matching '$pattern' in $file" >&2; exit 1; }
      perl -0pi -e "s/$pattern/$replacement/m" "$file"
    }
    set_line telemetry-rs/Cargo.toml '^version = "[^"]*"' "version = \"$version\""
    set_line telemetry-rs/Cargo.toml '^(telemetry-derive = \{[^}]*version = )"[^"]*"' "\${1}\"$version\""
    set_line telemetry-py/pyproject.toml '^version = "[^"]*"' "version = \"$version\""
    set_line telemetry-cpp/MODULE.bazel '^(    version = )"[^"]*"' "\${1}\"$version\""
    set_line telemetry-cpp/CMakeLists.txt '^(project\(telemetry-cpp VERSION )[0-9.]+' "\${1}${version%%-*}"
    echo "set all SDKs to $version"

# Release every SDK at one version: stamps it, commits, tags and pushes; the
# release workflow publishes.
[doc('Stamp, commit, tag and push a release of every SDK')]
release version: (set-version version)
    git commit -am "release: v{{ trim_start_match(version, 'v') }}"
    git tag "v{{ trim_start_match(version, 'v') }}"
    git push origin HEAD "v{{ trim_start_match(version, 'v') }}"

# ── production deployment (telemetry-core/deploy/production) ─────────────
# These drive real AWS/EKS/Terraform deployments; the recipes run the
# scripts, which stay the source of truth (the nix shell documents them too).

# Deploy the stack locally with Docker Compose (production config).
deploy-docker *args:
    cd telemetry-core/deploy/production && ./scripts/deploy-docker.sh {{ args }}

# Deploy to AWS EC2.
deploy-ec2 *args:
    cd telemetry-core/deploy/production && ./scripts/deploy-ec2.sh {{ args }}

# Deploy to AWS EKS.
deploy-eks *args:
    cd telemetry-core/deploy/production && ./scripts/deploy-eks.sh {{ args }}

# Update an EC2 deployment.
redeploy-ec2 *args:
    cd telemetry-core/deploy/production && ./scripts/redeploy-ec2.sh {{ args }}

# Destroy the EC2 infrastructure.
destroy-ec2 *args:
    cd telemetry-core/deploy/production && ./scripts/destroy-ec2.sh {{ args }}

# Generate an OTLP endpoint token.
setup-otel-endpoint *args:
    cd telemetry-core/deploy/production && ./scripts/setup-otel-endpoint.sh {{ args }}
