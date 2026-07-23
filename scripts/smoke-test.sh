#!/usr/bin/env bash
# Smoke test the bridge: a sync ask, then an async round trip.
set -euo pipefail

if [[ -f .env ]]; then
  set -a
  # shellcheck disable=SC1091
  source .env
  set +a
fi

BRIDGE_URL="${BRIDGE_URL:-http://localhost:8080}"
KEY="${BRIDGE_API_KEY:-}"
ENDPOINT="${BRIDGE_URL%/}/v1/openclaw"

call() {
  curl -sS -X POST "$ENDPOINT" \
    -H 'Content-Type: application/json' \
    ${KEY:+-H "api_key: $KEY"} \
    --data "$1"
}

pretty() { if command -v jq >/dev/null 2>&1; then jq .; else cat; fi }

echo "POST $ENDPOINT"
[[ -z "$KEY" ]] && echo "warning: BRIDGE_API_KEY is empty; the bridge may reject this" >&2

echo
echo "--- ask ---"
call "$(cat payloads/ask.json)" | pretty

echo
echo "--- ask_async ---"
ASYNC=$(call "$(cat payloads/ask_async.json)")
printf '%s\n' "$ASYNC" | pretty

JOB=$(printf '%s' "$ASYNC" | sed -n 's/.*"jobId":"\([^"]*\)".*/\1/p')
if [[ -z "$JOB" ]]; then
  echo "no jobId returned; stopping" >&2
  exit 1
fi

echo
echo "--- get_result (polling $JOB; up to 7 min) ---"
START=$(date +%s)
for _ in $(seq 1 90); do
  RESULT=$(call "{\"action\":\"get_result\",\"jobId\":\"$JOB\"}")
  STATUS=$(printf '%s' "$RESULT" | sed -n 's/.*"status":"\([^"]*\)".*/\1/p')
  echo "  status: ${STATUS:-unknown} ($(( $(date +%s) - START ))s)"
  [[ "$STATUS" != "running" ]] && { printf '%s\n' "$RESULT" | pretty; break; }
  sleep 5
done
