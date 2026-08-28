#!/bin/sh
# Build and install OpenRouter ImageJem using only a POSIX shell and Docker.
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)
OPENWEBUI_CONTAINER="${1:-${OPENWEBUI_CONTAINER:-open-webui}}"
IMAGE="${IMAGE:-local/openrouter-imagejem:dev}"

fatal() {
  printf 'Error: %s\n' "$*" >&2
  exit 1
}

command -v docker >/dev/null 2>&1 || fatal "docker is not installed or is not on PATH"
docker version >/dev/null 2>&1 || fatal "the Docker daemon is not reachable"

printf 'Building %s from %s\n' "$IMAGE" "$REPO_DIR"
docker build \
  --pull \
  --build-arg VERSION=dev \
  --build-arg COMMIT=local \
  --build-arg BUILD_DATE=local \
  -t "$IMAGE" \
  "$REPO_DIR"

printf 'Installing the locally built image\n'
PULL_IMAGE=false IMAGE="$IMAGE" "$SCRIPT_DIR/docker-only.sh" "$OPENWEBUI_CONTAINER"
