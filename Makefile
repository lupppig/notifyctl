.PHONY: proto build run clean build-cli build-server test test-race test-integration test-e2e test-all test-cli

proto:
	protoc --go_out=. --go_opt=module=github.com/lupppig/notifyctl \
		--go-grpc_out=. --go-grpc_opt=module=github.com/lupppig/notifyctl \
		api/health/v1/health.proto \
		api/notify/v1/notify.proto

build: build-server build-cli

build-server:
	go build -o bin/server ./cmd/server

build-cli:
	go build -o bin/notifyctl ./cmd/notifyctl

run:
	go run ./cmd/server

clean:
	rm -rf bin/

# ----- Tests -----
# Default unit tests: fast, no external services, no build tags.
test:
	go test ./...

# Unit tests under the race detector (covers the in-memory concurrency tests).
test-race:
	go test -race ./...

# Integration tests against a real Postgres. Requires DATABASE_URL; individual
# tests t.Skip when it is unset/unreachable. NOTE: host port 5432 may already be
# in use — point DATABASE_URL at an alternate port, e.g.:
#   DATABASE_URL=postgres://notifyctl:pw@localhost:5433/notifyctl?sslmode=disable \
#     make test-integration
# -p 1: both packages truncate the same database; package-level parallelism
# would make them clobber each other's fixtures.
test-integration:
	go test -race -count=1 -p 1 -tags=integration ./internal/store/... ./internal/retry/...

# End-to-end tests: real gRPC server (ephemeral port) + real Postgres + real
# NATS, exercising the full DLQ lifecycle including race scenarios.
# Requires the docker-compose stack; tests t.Skip when unreachable.
# Override connection strings with E2E_DATABASE_URL / E2E_NATS_URL, e.g.:
#   E2E_DATABASE_URL=postgres://notifyctl:pw@localhost:55432/notifyctl?sslmode=disable \
#     make test-e2e
test-e2e:
	go test -race -count=1 -tags=e2e ./e2e/...

# CLI smoke test: builds the binaries, boots Postgres + NATS (on non-standard
# host ports) and the server, then drives every major notifyctl command —
# including the full DLQ list/replay lifecycle — asserting on real output.
# Requires docker (compose plugin) and a psql client. KEEP_INFRA=1 leaves the
# stack running; NO_COMPOSE=1 reuses already-running infra.
test-cli:
	./scripts/cli_test.sh

# Everything (assumes the docker-compose stack is up).
test-all: test-race test-integration test-e2e
