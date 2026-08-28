#!/bin/sh
set -eu
BRIDGE_CONTAINER="${1:-${BRIDGE_CONTAINER:-openrouter-imagejem}}"
if docker inspect "$BRIDGE_CONTAINER" >/dev/null 2>&1; then
  docker rm -f "$BRIDGE_CONTAINER"
else
  printf "Container '%s' does not exist.\n" "$BRIDGE_CONTAINER"
fi
