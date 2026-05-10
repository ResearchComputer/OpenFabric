#!/bin/bash
set -euo pipefail

# Default to latest if no version is specified
VERSION=${1:-latest}

# Validate version
case $VERSION in
    stable|latest|dev)
        ;;
    *)
        echo "Error: Version must be 'stable', 'latest', or 'dev'"
        exit 1
        ;;
esac

if [ -z "${BUILD_SIGN_KEY:-}" ]; then
    echo "Error: BUILD_SIGN_KEY is not set. Generate one with:"
    echo "  cd src && go run ./internal/attestation/cmd/buildsign keygen"
    exit 1
fi

# Build args mirror src/Makefile so the in-container signature matches
# what peers will compute from (version, commit) on the wire.
BUILD_VERSION=$(git describe --tags --exact-match 2>/dev/null || git symbolic-ref -q --short HEAD)
COMMIT_HASH=$(git rev-parse --short HEAD)
BUILD_DATE=$(date "+%FT%T%z")

IMAGE=ghcr.io/xiaozheyao/opentela:amd64-$VERSION

# Pass the signing key as a docker secret (not a build-arg) so it is
# never embedded in any image layer or build history.
DOCKER_BUILDKIT=1 docker build \
    -f meta/Dockerfile.amd64 \
    --secret id=build_sign_key,env=BUILD_SIGN_KEY \
    --build-arg VERSION="$BUILD_VERSION" \
    --build-arg COMMIT_HASH="$COMMIT_HASH" \
    --build-arg BUILD_DATE="$BUILD_DATE" \
    -t "$IMAGE" \
    .

docker push "$IMAGE"