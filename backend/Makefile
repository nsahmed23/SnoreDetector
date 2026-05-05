SHELL := /bin/bash

.PHONY: build vet test test-integration tidy run-sync run-analytics run-export sqlc

TEST_DATABASE_URL ?= postgres://snoreguard:dev-only-password@localhost:5432/snoreguard?sslmode=disable

# Shared env block used by every `run-*` target.
DEV_ENV = JWT_SIGNING_KEY=change-me-to-a-real-32-byte-secret-please-1234567890 \
          DATABASE_URL=postgres://snoreguard:dev-only-password@localhost:5432/snoreguard?sslmode=disable \
          LOG_LEVEL=debug

build:
	mkdir -p bin
	go build -o bin/ ./cmd/...

vet:
	go vet ./...

# Unit tests only (no DB required).
test:
	go test ./...

# Integration tests. Requires Postgres reachable via $(TEST_DATABASE_URL).
test-integration:
	DATABASE_URL=$(TEST_DATABASE_URL) go test -tags=integration ./internal/store/...

tidy:
	go mod tidy

# Boot each service against the docker-compose Postgres.
run-sync:
	$(DEV_ENV) APPLE_AUDIENCE=com.snoreguard.app go run ./cmd/sync-service

run-analytics:
	$(DEV_ENV) ADDR=:8081 go run ./cmd/analytics-service

run-export:
	$(DEV_ENV) ADDR=:8082 go run ./cmd/export-service

# Regenerate hand-rolled queries from sqlc/queries.sql when you have
# `sqlc` installed. The generated code is committed.
sqlc:
	cd sqlc && sqlc generate
