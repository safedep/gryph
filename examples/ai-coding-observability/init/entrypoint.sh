#!/bin/bash
set -euo pipefail

OPENSEARCH_URL="${OPENSEARCH_URL:-http://opensearch:9200}"
DASHBOARDS_URL="${DASHBOARDS_URL:-http://dashboards:5601}"

echo "=== Gryph PoC Init ==="

# 1. Wait for OpenSearch
echo "[1/6] Waiting for OpenSearch..."
/wait-for-opensearch.sh "$OPENSEARCH_URL"

# 2. Wait for Dashboards
echo "[2/6] Waiting for Dashboards..."
until curl -sf "${DASHBOARDS_URL}/api/status" >/dev/null 2>&1; do
  sleep 3
done
echo "  Dashboards ready."

# 2b. Disable disk threshold entirely for PoC (Docker Desktop has limited disk)
echo "  Disabling disk threshold for PoC..."
curl -sf -X PUT "${OPENSEARCH_URL}/_cluster/settings" \
  -H "Content-Type: application/json" \
  -d '{"persistent":{"cluster.routing.allocation.disk.threshold_enabled":false}}' >/dev/null

# Wait for settings to take effect, then clear any existing blocks
echo "  Waiting for settings to propagate..."
sleep 5

# Repeatedly clear read-only blocks until it sticks
echo "  Clearing read-only blocks..."
for i in $(seq 1 10); do
  curl -s -X PUT "${OPENSEARCH_URL}/_all/_settings" \
    -H "Content-Type: application/json" \
    -d '{"index.blocks.read_only_allow_delete":null}' >/dev/null 2>&1 || true
  # Verify the .kibana index is writable by doing a test write
  TEST_RESULT=$(curl -s -o /dev/null -w "%{http_code}" \
    -X POST "${DASHBOARDS_URL}/api/saved_objects/_import?overwrite=true" \
    -H "osd-xsrf: true" \
    --form file=@"/dashboards-config/index-pattern.ndjson" 2>/dev/null)
  if [ "$TEST_RESULT" = "200" ]; then
    VERIFY=$(curl -s \
      -X POST "${DASHBOARDS_URL}/api/saved_objects/_import?overwrite=true" \
      -H "osd-xsrf: true" \
      --form file=@"/dashboards-config/index-pattern.ndjson")
    if echo "$VERIFY" | jq -e '.success == true' >/dev/null 2>&1; then
      echo "  Indices writable (attempt ${i})."
      break
    fi
  fi
  echo "  Attempt ${i}: indices still blocked, retrying..."
  sleep 3
done

# 3. Apply index template
echo "[3/6] Applying index template..."
curl -sf -X PUT "${OPENSEARCH_URL}/_index_template/gryph-events" \
  -H "Content-Type: application/json" \
  -d @/opensearch-config/index-template.json >/dev/null
echo "  Index template applied."

# 4. Apply ISM policy
echo "[4/6] Applying ISM policy..."
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" -X PUT \
  "${OPENSEARCH_URL}/_plugins/_ism/policies/gryph-retention" \
  -H "Content-Type: application/json" \
  -d @/opensearch-config/ism-policy.json)
if [ "$HTTP_CODE" = "200" ] || [ "$HTTP_CODE" = "201" ]; then
  echo "  ISM policy applied."
elif [ "$HTTP_CODE" = "409" ]; then
  echo "  ISM policy already exists (skipped)."
else
  echo "  ISM policy response: HTTP ${HTTP_CODE}"
fi

# 5. Import dashboards
echo "[5/6] Importing dashboards..."
for f in /dashboards-config/*.ndjson; do
  [ -f "$f" ] || continue
  NAME=$(basename "$f" .ndjson)
  echo "  Importing: ${NAME}"
  RESPONSE=$(curl -s \
    -X POST "${DASHBOARDS_URL}/api/saved_objects/_import?overwrite=true" \
    -H "osd-xsrf: true" \
    --form file=@"$f")
  SUCCESS=$(echo "$RESPONSE" | jq -r '.success // false')
  COUNT=$(echo "$RESPONSE" | jq -r '.successCount // 0')
  if [ "$SUCCESS" = "true" ]; then
    echo "    OK (${COUNT} objects)"
  else
    echo "    WARN: import reported errors"
    echo "$RESPONSE" | jq -r '.errors[]? | "      \(.id): \(.error.message)"' 2>/dev/null || true
  fi
done

# 6. Create alert monitors
echo "[6/6] Creating alert monitors..."
for f in /opensearch-config/alerts/*.json; do
  [ -f "$f" ] || continue
  NAME=$(basename "$f" .json)
  echo "  Creating monitor: ${NAME}"
  HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" \
    -X POST "${OPENSEARCH_URL}/_plugins/_alerting/monitors" \
    -H "Content-Type: application/json" \
    -d @"$f")
  echo "    HTTP ${HTTP_CODE}"
done

echo ""
echo "=== Init complete ==="
echo "  OpenSearch:  ${OPENSEARCH_URL}"
echo "  Dashboards:  http://localhost:5601"
echo "=== Ready for demo ==="
