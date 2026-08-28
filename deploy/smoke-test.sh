#!/bin/sh
# Local checks that require only Docker. This does not spend OpenRouter credits.
set -eu

BRIDGE_CONTAINER="${1:-${BRIDGE_CONTAINER:-openrouter-imagejem}}"

fatal() {
  printf 'Error: %s\n' "$*" >&2
  exit 1
}

command -v docker >/dev/null 2>&1 || fatal "docker is not installed or is not on PATH"
docker inspect "$BRIDGE_CONTAINER" >/dev/null 2>&1 || fatal "container '$BRIDGE_CONTAINER' was not found"

printf '%s\n' 'Container state:'
docker inspect --format '  status={{.State.Status}} health={{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}} network={{.HostConfig.NetworkMode}}' "$BRIDGE_CONTAINER"

printf '%s\n' 'Build information:'
docker exec "$BRIDGE_CONTAINER" /bridge version

printf '%s\n' 'Loopback health check:'
docker exec "$BRIDGE_CONTAINER" /bridge healthcheck

printf '%s\n' 'Recent non-sensitive service logs:'
docker logs --tail 20 "$BRIDGE_CONTAINER"

printf '%s\n' 'Smoke test passed.'
