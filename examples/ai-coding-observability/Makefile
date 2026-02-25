.PHONY: help setup up down clean generate generate-stdout stream ship status logs logs-opensearch demo-setup demo-threat demo-live

SHELL := /bin/bash

-include .env
export

PYTHON := python3
GENERATOR := generator/generate.py
COLLECTOR := scripts/gryph-ship.sh
OPENSEARCH_URL ?= http://localhost:9200
DASHBOARDS_URL ?= http://localhost:5601

help:
	@echo ""
	@echo "  Gryph Observability PoC"
	@echo "  ======================"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'
	@echo ""

.env:
	@bash scripts/setup-env.sh

setup: .env
	@echo "  Setup complete. Review .env if needed."

up: .env 
	@docker compose up -d --build
	@echo ""
	@echo "  Stack starting..."
	@echo "  OpenSearch:  $(OPENSEARCH_URL)"
	@echo "  Dashboards:  $(DASHBOARDS_URL)"
	@echo ""
	@echo "  Waiting for init to complete..."
	@docker compose logs -f init 2>/dev/null || true
	@echo ""
	@echo "  Ready! Open $(DASHBOARDS_URL) in your browser."

down: 
	docker compose down

clean:
	docker compose down -v
	@echo "All data deleted."

## Generate 7 days of synthetic data and load into OpenSearch
generate: 
	$(PYTHON) $(GENERATOR) --mode backfill --days 7 --load --opensearch-url $(OPENSEARCH_URL)

## Generate synthetic data as JSONL to stdout
generate-stdout: 
	$(PYTHON) $(GENERATOR) --mode backfill --days 7

## Stream synthetic events in real-time (~5/sec)
generate-stream: 
	$(PYTHON) $(GENERATOR) --mode stream --rate 5 --load --opensearch-url $(OPENSEARCH_URL)

# Ship real data to OpenSearch
ship: ship-gryph

## Ship real Gryph audit data to OpenSearch
ship-gryph:
	GRYPH_SHIP_TARGET=$(OPENSEARCH_URL) bash $(COLLECTOR)

status: 
	@echo "=== Cluster Health ==="
	@curl -s $(OPENSEARCH_URL)/_cluster/health | python3 -m json.tool 2>/dev/null || echo "  OpenSearch not reachable"
	@echo ""
	@echo "=== Indices ==="
	@curl -s "$(OPENSEARCH_URL)/_cat/indices/gryph-events-*?v&s=index" 2>/dev/null || echo "  No indices"
	@echo ""
	@echo "=== Total Events ==="
	@curl -s "$(OPENSEARCH_URL)/gryph-events-*/_count" 2>/dev/null | python3 -m json.tool 2>/dev/null || echo "  No events"
	@echo ""
	@echo "=== Alert Monitors ==="
	@curl -s "$(OPENSEARCH_URL)/_plugins/_alerting/monitors/_search" -H "Content-Type: application/json" -d '{"size":10}' 2>/dev/null | python3 -c "import sys,json; r=json.loads(sys.stdin.read()); [print(f'  {h[\"_source\"][\"name\"]} (enabled={h[\"_source\"][\"enabled\"]})') for h in r.get('hits',{}).get('hits',[])]" 2>/dev/null || echo "  No monitors"

logs: 
	docker compose logs -f init

logs-opensearch: 
	docker compose logs -f opensearch

demo-setup: up generate ## Full demo setup: start stack + generate 7 days of data
	@echo ""
	@echo "  ========================================="
	@echo "    Demo ready!"
	@echo "    Open $(DASHBOARDS_URL)"
	@echo "  ========================================="

demo-threat: ## Stream live threat events (watch dashboards update)
	@echo "  Starting threat simulation..."
	@echo "  Watch the Threat Detection dashboard at $(DASHBOARDS_URL)"
	@echo ""
	$(PYTHON) $(GENERATOR) --mode stream --rate 2 --load --opensearch-url $(OPENSEARCH_URL)

demo-live: ship ## Ship real Gryph data from this machine
	@echo ""
	@echo "  Real data shipped. Check the SOC Overview dashboard."
