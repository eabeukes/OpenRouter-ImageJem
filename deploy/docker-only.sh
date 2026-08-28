#!/bin/sh
# Install or update OpenRouter ImageJem using only a POSIX shell and Docker.
set -eu

OPENWEBUI_CONTAINER="${1:-${OPENWEBUI_CONTAINER:-open-webui}}"
BRIDGE_CONTAINER="${BRIDGE_CONTAINER:-openrouter-imagejem}"
IMAGE="${IMAGE:-ghcr.io/eabeukes/openrouter-imagejem:latest}"
MEMORY_LIMIT="${MEMORY_LIMIT:-768m}"
CPU_LIMIT="${CPU_LIMIT:-0.50}"
PULL_IMAGE="${PULL_IMAGE:-true}"

fatal() {
  printf 'Error: %s\n' "$*" >&2
  exit 1
}

command -v docker >/dev/null 2>&1 || fatal "docker is not installed or is not on PATH"
docker version >/dev/null 2>&1 || fatal "the Docker daemon is not reachable"
docker inspect "$OPENWEBUI_CONTAINER" >/dev/null 2>&1 || \
  fatal "OpenWebUI container '$OPENWEBUI_CONTAINER' was not found. Pass its name as the first argument."

openwebui_running="$(docker inspect --format '{{.State.Running}}' "$OPENWEBUI_CONTAINER")"
[ "$openwebui_running" = "true" ] || fatal "OpenWebUI container '$OPENWEBUI_CONTAINER' is not running"

case "$PULL_IMAGE" in
  true|1|yes)
    printf 'Pulling %s\n' "$IMAGE"
    docker pull "$IMAGE"
    ;;
  false|0|no)
    docker image inspect "$IMAGE" >/dev/null 2>&1 || \
      fatal "local image '$IMAGE' was not found; build it first or enable PULL_IMAGE"
    printf 'Using local image %s without pulling\n' "$IMAGE"
    ;;
  *)
    fatal "PULL_IMAGE must be true or false"
    ;;
esac

if docker inspect "$BRIDGE_CONTAINER" >/dev/null 2>&1; then
  printf 'Replacing existing container %s\n' "$BRIDGE_CONTAINER"
  docker rm -f "$BRIDGE_CONTAINER" >/dev/null
fi

printf 'Starting %s in the network namespace of %s\n' "$BRIDGE_CONTAINER" "$OPENWEBUI_CONTAINER"
docker run -d \
  --name "$BRIDGE_CONTAINER" \
  --restart unless-stopped \
  --network "container:$OPENWEBUI_CONTAINER" \
  --read-only \
  --user 65532:65532 \
  --cap-drop ALL \
  --security-opt no-new-privileges:true \
  --pids-limit 64 \
  --memory "$MEMORY_LIMIT" \
  --cpus "$CPU_LIMIT" \
  -e LISTEN_ADDR=127.0.0.1:8787 \
  -e OPENROUTER_IMAGES_URL=https://openrouter.ai/api/v1/images \
  -e UPSTREAM_TIMEOUT=600s \
  -e MAX_REQUEST_BYTES=67108864 \
  -e MAX_RESPONSE_BYTES=134217728 \
  -e MAX_SCALAR_FIELD_BYTES=1048576 \
  -e MAX_REFERENCE_IMAGES=10 \
  -e MAX_CONCURRENT_REQUESTS=2 \
  "$IMAGE" >/dev/null

attempt=0
while [ "$attempt" -lt 30 ]; do
  state="$(docker inspect --format '{{.State.Status}}' "$BRIDGE_CONTAINER" 2>/dev/null || printf unknown)"
  health="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$BRIDGE_CONTAINER" 2>/dev/null || printf unknown)"
  case "$health" in
    healthy)
      printf '\nOpenRouter ImageJem is healthy.\n'
      printf 'OpenWebUI base URL: http://127.0.0.1:8787/v1\n'
      printf 'Container: %s\n' "$BRIDGE_CONTAINER"
      printf 'Image: %s\n' "$IMAGE"
      exit 0
      ;;
    unhealthy)
      docker logs --tail 50 "$BRIDGE_CONTAINER" >&2 || true
      fatal "container health check failed"
      ;;
  esac
  if [ "$state" = "exited" ] || [ "$state" = "dead" ]; then
    docker logs --tail 50 "$BRIDGE_CONTAINER" >&2 || true
    fatal "container stopped during startup"
  fi
  attempt=$((attempt + 1))
  sleep 1
done

docker logs --tail 50 "$BRIDGE_CONTAINER" >&2 || true
fatal "container did not become healthy within 30 seconds"
