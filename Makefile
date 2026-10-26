VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
TEST_DB ?= postgres://lde:lde@localhost:5432/lde?sslmode=disable
OPERATOR ?=

.PHONY: build run test test-integration validate vet up down image verify-chain

build: ## Static binary in ./bin
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=$(VERSION)" -o bin/labrat ./cmd/server

run: ## Run against the compose stack
	go run ./cmd/server

test: ## Unit tests (no database)
	go test ./...

test-integration: ## Unit + database guarantees (needs `make up`)
	LDE_TEST_DATABASE_URL="$(TEST_DB)" go test -count=1 ./...

validate: ## OQ: run the suite against the database and write a stamped traceability report
	LDE_TEST_DATABASE_URL="$(TEST_DB)" go run ./tools/validate --operator "$(OPERATOR)"

vet:
	go vet ./...

up:
	docker compose up -d --wait

down:
	docker compose down

image:
	docker build --build-arg VERSION=$(VERSION) -t labrat:$(VERSION) .

verify-chain: ## Export the dev tenant's audit chain and verify it independently
	curl -sf localhost:8080/v1/audit/export.ndjson \
	  -H 'X-LDE-Tenant: 00000000-0000-4000-8000-000000000001' -H 'X-LDE-Actor: auditor' > /tmp/labrat-chain.ndjson
	python3 tools/verify_audit.py /tmp/labrat-chain.ndjson
