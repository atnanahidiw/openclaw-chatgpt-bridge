#!/usr/bin/env bash
set -euo pipefail

if [[ -f .env ]]; then
  set -a
  # shellcheck disable=SC1091
  source .env
  set +a
fi

BRIDGE_URL="${BRIDGE_URL:-http://localhost:8080}"

echo "POST ${BRIDGE_URL%/}/v1/openclaw"

if command -v jq >/dev/null 2>&1; then
  curl -sS -X POST "${BRIDGE_URL%/}/v1/openclaw" \
    -H "Content-Type: application/json" \
    --data @payloads/create_flow.json | jq .
else
  curl -sS -X POST "${BRIDGE_URL%/}/v1/openclaw" \
    -H "Content-Type: application/json" \
    --data @payloads/create_flow.json
fi
