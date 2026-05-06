SHELL := /bin/bash

.PHONY: build vet test test-integration tidy run-sync sqlc lint

# Test DB URL — override with TEST_DATABASE_URL=... if you want a different db.
TEST_DATABASE_URL ?= postgres://snoreguard:dev-only-password@localhost:5432/snoreguard?sslmode=disable

build:
	mkdir -p bin
	go build -o bin/ ./cmd/...

vet:
	go vet ./...

# Unit tests only (no DB required).
test:
	go test ./...

# Integration tests. Requires Postgres reachable via $(TEST_DATABASE_URL).
# Brings up `docker compose up -d postgres` first if you want a local one.
test-integration:
	DATABASE_URL=$(TEST_DATABASE_URL) go test -tags=integration ./internal/store/...

tidy:
	go mod tidy

# Boot the sync-service against the docker-compose Postgres.
run-sync:
	JWT_SIGNING_KEY=change-me-to-a-real-32-byte-secret-please-1234567890 \
	DATABASE_URL=postgres://snoreguard:dev-only-password@localhost:5432/snoreguard?sslmode=disable \
	APPLE_AUDIENCE=com.snoreguard.app \
	LOG_LEVEL=debug \
	go run ./cmd/sync-service

# Regenerate hand-rolled queries from sqlc/queries.sql when you have
# `sqlc` installed. The generated code is committed.
sqlc:
	cd sqlc && sqlc generate
