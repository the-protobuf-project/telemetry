#!/usr/bin/env bash
# Runs the cross-language tracing demo: one trace through four services, each
# written with a different Telemetry SDK, handed on in W3C headers:
#
#   orders (Go) → inventory (Rust) → billing (Python) → shipping (C++)
#
# Each hop reads the previous hop's traceparent, starts its span under it and
# sends its own on. Exits non-zero if any hop starts a new trace. Python and
# C++ hops run when uv / bazel are installed; Go and Rust always do.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"

field() { sed -E "s/.*\"$2\":\"([^\"]*)\".*/\1/" <<<"$1"; }

go_out="$(cd "$root/telemetry-go" && go run ./examples/crosslang 2>/dev/null)"
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

rust_out="$(cd "$root/telemetry-rs" && printf '{"traceparent":"%s","baggage":"tenant=acme"}' "$next" \
  | cargo run -q -p telemetry-examples --bin crosslang 2>/dev/null | grep '"received_traceparent"' | tail -1)"
hop "rust   (inventory-rs)" "$rust_out"

if command -v uv >/dev/null; then
  py_out="$(cd "$root/telemetry-py" && printf '{"traceparent":"%s"}' "$next" \
    | uv run -q python examples/crosslang.py 2>/dev/null | grep '"received_traceparent"' | tail -1)"
  hop "python (billing-py)" "$py_out"
fi

if command -v bazel >/dev/null; then
  cpp_out="$(cd "$root/telemetry-cpp" && bazel build --noshow_progress --ui_event_filters=-info //examples:crosslang >/dev/null 2>&1 \
    && printf '{"traceparent":"%s"}' "$next" | bazel-bin/examples/crosslang 2>/dev/null | grep '"received_traceparent"' | tail -1)"
  hop "c++    (shipping-cpp)" "$cpp_out"
fi

if [[ -n "$trace" && "$broken" == 0 ]]; then
  echo "OK: one trace across every service"
else
  echo "BROKEN: a hop started a new trace" >&2
  exit 1
fi
