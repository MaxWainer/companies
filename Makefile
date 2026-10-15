SHELL := /bin/bash
.DEFAULT_GOAL := help

GOLANGCI_LINT_VERSION ?= v2.14.0
# Installed once into ./bin, so linting (and the pre-commit hook) needs no network.
GOLANGCI_LINT := bin/golangci-lint-$(GOLANGCI_LINT_VERSION)
SQLC_VERSION ?= v1.31.1
SQLC := bin/sqlc-$(SQLC_VERSION)
BUF_VERSION ?= v1.73.0
BUF := bin/buf-$(BUF_VERSION)
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

# Loads .env.example and then .env (if present) into the environment.
LOAD_ENV := set -a; source ./.env.example; [ -f .env ] && source ./.env; set +a

.PHONY: help
help: ## Show available targets
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_-]+:.*##/ {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.PHONY: build
build: ## Build service and tokengen binaries into ./bin
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=$(VERSION)" -o bin/companies ./cmd/companies
	CGO_ENABLED=0 go build -trimpath -o bin/tokengen ./cmd/tokengen

.PHONY: run
run: ## Run the service on the host (needs `make deps`)
	$(LOAD_ENV); go run ./cmd/companies serve

.PHONY: migrate
migrate: ## Apply database migrations
	$(LOAD_ENV); go run ./cmd/companies migrate

.PHONY: token
token: ## Print a development JWT (SUB=alice TTL=1h)
	@$(LOAD_ENV); go run ./cmd/tokengen -sub $${SUB:-dev-user} -ttl $${TTL:-1h}

$(SQLC):
	GOBIN=$(CURDIR)/bin CGO_ENABLED=1 go install github.com/sqlc-dev/sqlc/cmd/sqlc@$(SQLC_VERSION)
	mv bin/sqlc $@

$(BUF):
	GOBIN=$(CURDIR)/bin go install github.com/bufbuild/buf/cmd/buf@$(BUF_VERSION)
	mv bin/buf $@

.PHONY: generate
generate: $(SQLC) $(BUF) ## Regenerate protobuf, sqlc queries, easyjson codecs, gomock mocks and the Swagger spec (api/)
	$(BUF) lint
	$(BUF) generate
	$(SQLC) generate
	@# Codecs first: mockgen type-checks packages that rely on them.
	go generate -run easyjson ./...
	go generate -run mockgen ./...
	go tool swag init --quiet -g cmd/companies/main.go -o api --outputTypes go,yaml \
		--packageName api --parseInternal --parseDependency --parseDepth 1

.PHONY: check-generate
check-generate: generate ## Fail if generated code is out of date
	git diff --exit-code -- '*_easyjson.go' internal/mocks api internal/event/eventpb \
		internal/company/repository/companydb
	$(SQLC) vet

.PHONY: proto-breaking
proto-breaking: $(BUF) ## Fail if the event contract breaks compatibility with main (AGAINST=<git ref>)
	$(BUF) breaking --against '.git#branch=$(or $(AGAINST),main)'

.PHONY: bench
bench: ## Run micro-benchmarks (codecs, handlers, auth, logging)
	go test -run='^$$' -bench=. -benchmem ./internal/...

.PHONY: bench-integration
bench-integration: ## Run end-to-end API benchmarks against Postgres + Kafka (requires Docker)
	go test -tags=integration -run='^$$' -bench=. -benchmem -timeout 15m ./test/integration/...

.PHONY: test
test: ## Run unit tests
	go test -race -count=1 ./...

.PHONY: test-integration
test-integration: ## Run integration tests (requires Docker)
	go test -tags=integration -race -count=1 -timeout 15m ./test/integration/...

.PHONY: cover
cover: ## Unit + integration tests with a coverage report
	go test -tags=integration -race -count=1 -timeout 15m -coverpkg=./internal/... -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

$(GOLANGCI_LINT):
	GOBIN=$(CURDIR)/bin go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	mv bin/golangci-lint $@

.PHONY: lint
lint: $(GOLANGCI_LINT) ## Run golangci-lint
	$(GOLANGCI_LINT) run ./...

.PHONY: lint-fix
lint-fix: $(GOLANGCI_LINT) ## Run golangci-lint and apply the fixes it can make (incl. formatting)
	$(GOLANGCI_LINT) run --fix ./...

.PHONY: hooks
hooks: ## Install the git pre-commit hook that runs golangci-lint
	git config core.hooksPath .githooks
	@echo "pre-commit hook installed (.githooks/pre-commit); remove with: git config --unset core.hooksPath"

.PHONY: fmt
fmt: $(GOLANGCI_LINT) ## Format code
	$(GOLANGCI_LINT) fmt ./...

.PHONY: docker-build
docker-build: ## Build the production image
	docker build --build-arg VERSION=$(VERSION) -t companies-service:$(VERSION) .

.PHONY: start
start: ## Build and start everything, wait until healthy, print all links/credentials (NO_BUILD=1 to skip the build)
	@./scripts/run.sh up

.PHONY: info
info: ## Print links, credentials, a fresh JWT and curl examples for the running stack
	@./scripts/run.sh info

.PHONY: stop
stop: ## Stop the stack (data volumes are kept)
	@./scripts/run.sh stop

.PHONY: up
up: ## Start the full stack with plain docker compose (fixed default ports)
	docker compose up -d --build

.PHONY: deps
deps: ## Start only dependencies, observability and tools (run the service with `make run`)
	docker compose up -d postgres redpanda otel-collector tempo loki prometheus grafana redpanda-console pgadmin

.PHONY: down
down: ## Stop the stack
	docker compose down

.PHONY: clean
clean: ## Stop the stack and delete its volumes
	docker compose down -v
	rm -rf bin coverage.out

.PHONY: dlq
dlq: ## Tail the dead-letter topic (events the broker refused, with dlq_* headers)
	docker compose exec redpanda rpk topic consume companies.events.v1.dlq --offset start \
		--format '%k  %h{%k=%v }(%V bytes, protobuf)\n'

NEWMAN_IMAGE ?= postman/newman:6.1.3-alpine

.PHONY: postman
postman: ## Run the Postman collection against the running stack (newman in Docker)
	@env=companies.postman_environment.json; \
	[ -f postman/local.postman_environment.json ] && env=local.postman_environment.json; \
	docker run --rm --network companies_default -v "$(CURDIR)/postman:/etc/newman:ro" $(NEWMAN_IMAGE) \
		run companies.postman_collection.json -e $$env --env-var baseUrl=http://api:8080

.PHONY: events
events: ## Tail company events (key, headers, size; Redpanda Console shows the decoded protobuf)
	docker compose exec redpanda rpk topic consume companies.events.v1 --offset start \
		--format '%k  %h{%k=%v }(%V bytes, protobuf)\n'
