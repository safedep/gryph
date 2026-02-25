#!/bin/bash
URL="${1:-http://opensearch:9200}"
echo "  Polling ${URL}..."
until curl -sf "${URL}/_cluster/health" | grep -qE '"status":"(green|yellow)"'; do
  sleep 2
done
echo "  OpenSearch ready."
