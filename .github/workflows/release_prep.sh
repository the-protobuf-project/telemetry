#!/usr/bin/env bash
# Run by bazel-contrib's release_ruleset workflow (release.yaml, release-cpp)
# at the release tag, from the repository root, after the C++ tests pass.
#
# Writes the C++ source archive, telemetry-cpp-<version>.tar.gz, which that
# workflow attests (telemetry-cpp-<version>.tar.gz.intoto.jsonl) and attaches
# to the release, and prints the release notes on stdout.
#
# Usage: .github/workflows/release_prep.sh v1.4.0

set -euo pipefail

TAG="$1"
VERSION="${TAG#v}"
REPO="${GITHUB_REPOSITORY:-the-protobuf-project/telemetry}"
ARCHIVE="telemetry-cpp-$VERSION.tar.gz"

# `just release` stamps the version before tagging; the BCR rejects an
# archive whose MODULE.bazel names another version.
module_version="$(sed -nE 's/^    version = "([^"]*)".*/\1/p' telemetry-cpp/MODULE.bazel)"
if [[ "$module_version" != "$VERSION" ]]; then
  echo "telemetry-cpp/MODULE.bazel is at '$module_version', the tag is $TAG" >&2
  exit 1
fi

# The source, which is what the BCR, Bazel's archive_override and CMake's
# FetchContent take. A prebuilt static library would hold one of the SDK's
# libraries and none of opentelemetry-cpp, which it links against.
tar -czf "$ARCHIVE" \
  --exclude='telemetry-cpp/bazel-*' --exclude='.DS_Store' --exclude='telemetry-cpp/.env*' \
  --transform "s,^telemetry-cpp,telemetry-cpp-$VERSION," telemetry-cpp
sha256sum "$ARCHIVE" > "$ARCHIVE.sha256"

sha="$(cut -d' ' -f1 "$ARCHIVE.sha256")"
integrity="sha256-$(openssl dgst -sha256 -binary "$ARCHIVE" | openssl base64 -A)"
url="https://github.com/$REPO/releases/download/$TAG"

cat <<EOF
## Install $VERSION

| Language | |
| -------- | - |
| Go       | \`go get github.com/$REPO/telemetry-go@v$VERSION\` |
| Rust     | \`telemetry-rs = { git = "https://github.com/$REPO", tag = "$TAG" }\` |
| Python   | \`pip install $url/telemetry_py-$VERSION-py3-none-any.whl\` |
| C++      | \`tpp_telemetry.cpp\` on the Bazel Central Registry, or \`$ARCHIVE\` for CMake |

The C++ archive carries SLSA build provenance (\`$ARCHIVE.intoto.jsonl\`):

\`\`\`sh
gh attestation verify $ARCHIVE --repo $REPO
\`\`\`

### C++ with Bazel

\`\`\`starlark
bazel_dep(name = "tpp_telemetry.cpp", version = "$VERSION")
\`\`\`

Until the BCR entry is merged, pin the release archive:

\`\`\`starlark
archive_override(
    module_name = "tpp_telemetry.cpp",
    urls = ["$url/$ARCHIVE"],
    strip_prefix = "telemetry-cpp-$VERSION",
    integrity = "$integrity",
)
\`\`\`

### C++ with CMake

\`\`\`cmake
FetchContent_Declare(telemetry
    URL $url/$ARCHIVE
    URL_HASH SHA256=$sha)
FetchContent_MakeAvailable(telemetry)
\`\`\`
EOF
