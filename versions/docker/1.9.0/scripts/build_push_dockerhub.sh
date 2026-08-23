#!/usr/bin/env bash
set -euo pipefail

SRC_DIR="${SRC_DIR:-/opt/searchterm_build_new}"
IMAGE="${IMAGE:-dannis1514/searchterm}"
BUILDER="${BUILDER:-searchterm-multi}"

cd "$SRC_DIR"

echo "== source check =="
test -f Dockerfile
test -f web/static/app.js
grep -q 'SEARCH_HISTORY_KEY' web/static/app.js || { echo "web/static/app.js is stale"; exit 1; }

echo "== docker auth =="
if [ -n "${DOCKER_TOKEN:-}" ]; then
  echo "$DOCKER_TOKEN" | docker login -u dannis1514 --password-stdin
else
  echo "DOCKER_TOKEN unset, using existing docker config credentials"
fi

echo "== register qemu binfmt =="
docker run --privileged --rm tonistiigi/binfmt --install arm64,amd64

echo "== buildx builder =="
if ! docker buildx inspect "$BUILDER" >/dev/null 2>&1; then
  docker buildx create --name "$BUILDER" --driver docker-container --bootstrap
fi
docker buildx use "$BUILDER"
docker buildx ls

echo "== push arch-specific 1.5 tags =="
docker buildx build --platform linux/amd64 -t "$IMAGE:1.5-amd64" --push --provenance=false "$SRC_DIR"
docker buildx build --platform linux/arm64 -t "$IMAGE:1.5-arm64" --push --provenance=false "$SRC_DIR"

echo "== push multi-arch 1.5 + latest =="
docker buildx build --platform linux/amd64,linux/arm64 \
  -t "$IMAGE:1.5" -t "$IMAGE:latest" \
  --push --provenance=false "$SRC_DIR"

echo "== verify =="
docker buildx imagetools inspect "$IMAGE:1.5"
docker buildx imagetools inspect "$IMAGE:latest"

echo "done"
