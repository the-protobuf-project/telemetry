#!/usr/bin/env bash
# Sets every SDK's package version to one release version, so a single tag
# releases all of them in step:
#
#   bench/set-version.sh 1.3.0
#
# Rust: workspace version and the telemetry-derive dependency pin.
# Python: pyproject.toml. C++: MODULE.bazel and CMakeLists.txt.
# Go needs no file change: its version is the telemetry-go/vX.Y.Z tag the
# release workflow pushes (see .github/workflows/release.yaml).
#
# The release workflow runs this on the tagged commit before packaging; run it
# yourself and commit the result to keep the files in step with the tags.
set -euo pipefail

version="${1#v}"
if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]; then
  echo "usage: $0 X.Y.Z[-prerelease]   (got '${1:-}')" >&2
  exit 2
fi
root="$(cd "$(dirname "$0")/.." && pwd)"

# Replace the first match of a line-anchored regex in a file, failing if
# nothing matched so a renamed field cannot silently skip a language.
set_line() {
  local file="$1" pattern="$2" replacement="$3"
  if ! grep -Eq "$pattern" "$file"; then
    echo "set-version: no line matching '$pattern' in $file" >&2
    exit 1
  fi
  perl -0pi -e "s/$pattern/$replacement/m" "$file"
}

set_line "$root/telemetry-rs/Cargo.toml" '^version = "[^"]*"' "version = \"$version\""
set_line "$root/telemetry-rs/Cargo.toml" '^(telemetry-derive = \{[^}]*version = )"[^"]*"' "\${1}\"$version\""
set_line "$root/telemetry-py/pyproject.toml" '^version = "[^"]*"' "version = \"$version\""
set_line "$root/telemetry-cpp/MODULE.bazel" '^(    version = )"[^"]*"' "\${1}\"$version\""
set_line "$root/telemetry-cpp/CMakeLists.txt" '^(project\(telemetry-cpp VERSION )[0-9.]+' "\${1}${version%%-*}"

echo "set all SDKs to $version"
