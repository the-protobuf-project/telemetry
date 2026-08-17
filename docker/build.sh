#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
IMAGE_NAME="${IMAGE_NAME:-ghcr.io/the-protobuf-project/telemetry}"

VERSION="${IMAGE_TAG:-1.0.0}"

usage() {
    echo "Usage: $0 <amd64|arm64|manifest|local>"
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
    exit 1
}

ARCH="${1:-}"
[ -z "$ARCH" ] && usage

if [ "$ARCH" = "local" ]; then
    echo "Building opentelemetry-stack:${VERSION} locally"

    docker build \
        -f "${SCRIPT_DIR}/Dockerfile" \
        -t "opentelemetry-stack:${VERSION}" \
        "${SCRIPT_DIR}"

    echo "Done! Built opentelemetry-stack:${VERSION}"
elif [ "$ARCH" = "amd64" ] || [ "$ARCH" = "arm64" ]; then
    TAG="${VERSION}-${ARCH}"

    echo "Building ${IMAGE_NAME}:${TAG} for linux/${ARCH}"

    docker buildx build \
        --platform "linux/${ARCH}" \
        -f "${SCRIPT_DIR}/Dockerfile" \
        -t "${IMAGE_NAME}:${TAG}" \
        --push \
        "${SCRIPT_DIR}"

    echo "Done! Pushed ${IMAGE_NAME}:${TAG}"
elif [ "$ARCH" = "manifest" ]; then
    echo "Creating multi-arch manifest for ${IMAGE_NAME}:${VERSION}"

    docker buildx imagetools create \
        -t "${IMAGE_NAME}:${VERSION}" \
        "${IMAGE_NAME}:${VERSION}-amd64" \
        "${IMAGE_NAME}:${VERSION}-arm64"

    echo "Done! Multi-arch manifest pushed."
else
    usage
fi
