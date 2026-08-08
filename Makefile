GO         := go
DC         := docker compose
SERVER_BIN := bin/mcp-server
SEED_BIN   := bin/seed
AGENT_BIN  := bin/agent

# Load .env if present so `make seed` picks up OPENOBSERVE_USERNAME etc.
ifneq (,$(wildcard ./.env))
include .env
export
endif

.PHONY: help build up down logs ps seed mcp agent verify test test-unit test-integration fmt vet clean tidy

help: ## Show this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage: make \033[36m<target>\033[0m\n\nTargets:\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

build: ## Build all binaries into ./bin
	mkdir -p bin
	$(GO) build -o $(SERVER_BIN) ./cmd/mcp-server
	$(GO) build -o $(SEED_BIN)   ./cmd/seed
	$(GO) build -o $(AGENT_BIN)  ./cmd/agent

up: ## Start OpenObserve (and wait for healthy)
	$(DC) up -d
	@echo "Waiting for OpenObserve..."
	@for i in $$(seq 1 30); do \
	  status=$$(curl -sf http://localhost:5080/healthz > /dev/null && echo ok || echo fail); \
	  if [ "$$status" = "ok" ]; then echo "OpenObserve is healthy"; exit 0; fi; \
	  sleep 2; \
	done; \
	echo "OpenObserve did not become healthy in time" >&2; exit 1

down: ## Stop and remove OpenObserve container (data volume kept)
	$(DC) down

ps: ## Show container status
	$(DC) ps

logs: ## Tail OpenObserve container logs
	$(DC) logs -f openobserve

seed: build ## Load deterministic sample logs/metrics/traces
	$(SEED_BIN)

mcp: build ## Run the MCP server (stdio) in foreground
	$(SERVER_BIN)

agent: build ## Run the LLM agent (requires ANTHROPIC_API_KEY)
	MCP_SERVER_BIN=$(SERVER_BIN) $(AGENT_BIN) -i

verify: build ## Print a transcript of natural-language queries against the live MCP server
	$(GO) run ./scripts/verify

test: ## Run unit tests
	$(GO) test ./...

test-integration: build ## Run integration tests against running OpenObserve
	$(GO) test -tags=integration ./tests/integration/...

fmt: ## Run gofmt
	gofmt -w .

vet: ## Run go vet
	$(GO) vet ./...

tidy: ## Run go mod tidy
	$(GO) mod tidy

clean: ## Remove built binaries and any stray test artifacts
	rm -rf bin
	$(DC) down -v