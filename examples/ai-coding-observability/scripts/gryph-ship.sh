#!/bin/bash
set -euo pipefail

OPENSEARCH_URL="${GRYPH_SHIP_TARGET:-http://localhost:9200}"
INDEX_PREFIX="${GRYPH_SHIP_INDEX:-gryph-events}"
STATE_FILE="${GRYPH_SHIP_STATE:-${HOME}/.local/state/gryph/last-export}"

HOSTNAME_SHORT=$(hostname -s)
USERNAME=$(whoami)

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

log_info() { echo -e "${GREEN}[INFO]${NC}  $(date -u +%FT%TZ) $*"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC}  $(date -u +%FT%TZ) $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $(date -u +%FT%TZ) $*" >&2; }

if ! command -v gryph &>/dev/null; then
  log_error "gryph not found in PATH. Install from https://github.com/safedep/gryph"
  exit 1
fi

if ! curl -sf "${OPENSEARCH_URL}/_cluster/health" &>/dev/null; then
  log_error "Cannot reach OpenSearch at ${OPENSEARCH_URL}"
  exit 1
fi

mkdir -p "$(dirname "$STATE_FILE")"

if [ -f "$STATE_FILE" ]; then
  SINCE_TS=$(cat "$STATE_FILE")
  SINCE_ARG="--since ${SINCE_TS}"
  log_info "Exporting events since ${SINCE_TS}"
else
  SINCE_ARG="--since 24h"
  log_info "First run, exporting last 24 hours"
fi

EVENTS=$(gryph export ${SINCE_ARG} 2>/dev/null) || {
  log_error "gryph export failed"
  exit 1
}

if [ -z "$EVENTS" ]; then
  log_info "No new events"
  date -u +%FT%TZ >"$STATE_FILE"
  exit 0
fi

EVENT_COUNT=$(echo "$EVENTS" | wc -l | tr -d ' ')
log_info "Exported ${EVENT_COUNT} events"

INDEX_NAME="${INDEX_PREFIX}-$(date -u +%Y.%m)"

BULK_BODY=$(GRYPH_SHIP_HOSTNAME="$HOSTNAME_SHORT" \
  GRYPH_SHIP_USERNAME="$USERNAME" \
  GRYPH_SHIP_INDEX_NAME="$INDEX_NAME" \
  python3 -c "
import sys, json, os

hostname = os.environ['GRYPH_SHIP_HOSTNAME']
username = os.environ['GRYPH_SHIP_USERNAME']
index_name = os.environ['GRYPH_SHIP_INDEX_NAME']

for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    event = json.loads(line)
    event['endpoint_hostname'] = hostname
    event['endpoint_username'] = username
    print(json.dumps({'index': {'_index': index_name, '_id': event['id']}}))
    print(json.dumps(event))
" <<< "$EVENTS")

log_info "Shipping to ${OPENSEARCH_URL}/${INDEX_NAME}..."
RESPONSE=$(echo "$BULK_BODY" | curl -s -w "\n%{http_code}" \
  -X POST "${OPENSEARCH_URL}/_bulk" \
  -H "Content-Type: application/x-ndjson" \
  --data-binary @-)

HTTP_CODE=$(echo "$RESPONSE" | tail -1)
BODY=$(echo "$RESPONSE" | sed '$d')

if [ "$HTTP_CODE" -ge 200 ] && [ "$HTTP_CODE" -lt 300 ]; then
  ERRORS=$(echo "$BODY" | python3 -c "
import sys, json
r = json.loads(sys.stdin.read())
print(sum(1 for i in r.get('items',[]) if 'error' in i.get('index',{})))
" 2>/dev/null || echo "0")

  if [ "$ERRORS" -gt 0 ]; then
    log_warn "Shipped ${EVENT_COUNT} events (HTTP ${HTTP_CODE}) with ${ERRORS} item errors"
  else
    log_info "Shipped ${EVENT_COUNT} events successfully (HTTP ${HTTP_CODE})"
  fi
  date -u +%FT%TZ >"$STATE_FILE"
else
  log_error "OpenSearch returned HTTP ${HTTP_CODE}"
  log_error "Response: ${BODY}"
  exit 1
fi
