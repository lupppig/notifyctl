# notifyctl

A CLI-driven, gRPC notification and webhook delivery platform for backend teams.

Services emit an event once; notifyctl delivers it asynchronously to one or more
destinations, retries on failure with exponential backoff, persists deliveries
that exhaust their retries to a dead-letter queue, and streams delivery status in
real time — all driven from a single CLI.

## Features

- **CLI-first** — manage services, send events, and watch deliveries from the terminal.
- **gRPC** — a single `NotifyService` API; no REST surface to maintain.
- **Reliable delivery** — exponential backoff retries backed by NATS JetStream.
- **Dead-letter queue** — deliveries that exhaust their retries are persisted, inspectable, and replayable.
- **Real-time streaming** — watch per-destination delivery status as it happens.
- **Structured logging** — JSON to file, human-readable to console, streamable over gRPC.

## Quick start

Requires Go, Docker (with the Compose plugin), and a `psql` client.

```bash
# 1. Start Postgres + NATS
cp .env.example .env          # adjust credentials if you like
docker compose up -d

# 2. Build the binaries (bin/server, bin/notifyctl)
make build

# 3. Run the server (listens on :50051)
make run                       # or: ./bin/server

# 4. Register a service — prints a service ID and API key
./bin/notifyctl service create \
  --name my-service \
  --webhook-url https://example.com/hook

# 5. Point the CLI at that service
export NOTIFYCTL_SERVICE_ID=<service-id>
export NOTIFYCTL_API_KEY=<api-key>

# 6. Send a notification
./bin/notifyctl send --raw '{
  "topic": "order.created",
  "payload": {"order_id": "A-1"},
  "destinations": [{"type": 1, "target": "https://example.com/hook"}]
}'

# 7. Watch its delivery status, or list past notifications
./bin/notifyctl watch --request-id <notification-id>
./bin/notifyctl logs
```

`destinations[].type` is the destination kind (`1` = webhook); `target` is the URL.

## CLI commands

All commands accept the global flags `--json` (machine-readable output),
`-q/--quiet` (IDs only), `--timeout`, `--auth-token`, `--service-id`, and
`--config`.

| Command | Description | Key flags |
| --- | --- | --- |
| `service create` | Register a service; returns its ID and API key | `--name`, `--webhook-url`, `--secret` |
| `service list` | List registered services | |
| `service delete` | Delete a service | `--id` |
| `send` | Send a notification | `--raw '<json>'` or `--payload <file>` |
| `watch` | Stream delivery status | `--request-id`, `--ui` |
| `logs` | List notification jobs, or show stats / stream server logs | `--stats`, `--stream`, `--service-id` |
| `dlq list` | List dead letters | `--service-id`, `--json`, `-q` |
| `dlq replay <id>` | Re-enqueue a dead letter | |

## Dead-letter queue

When a delivery exhausts its retry budget, the retry scheduler persists it to a
queryable `dead_letters` table — capturing the originating notification, owning
service, payload, last error, attempt count, and failure time — and marks the job
`DEAD_LETTERED` so it is never retried again. Failures are durable and inspectable
rather than silently dropped.

```bash
# Inspect dead letters for your service
notifyctl dlq list
notifyctl dlq list --json          # machine-readable
notifyctl dlq list -q              # dead-letter IDs only

# Replay one back into the dispatcher
notifyctl dlq replay <dead-letter-id>
```

Replay resets the original job to `PENDING` with a fresh retry budget, re-publishes
it, and removes the dead-letter record. DLQ operations require an API key and are
**scoped to the service that owns it** — passing another service's `--service-id`
is rejected with `PermissionDenied`.

## Configuration

The CLI reads `~/.notifyctl.yaml` (override with `--config`):

```yaml
server_addr: localhost:50051
service_id: <service-id>
api_key: <api-key>
```

Environment variables take precedence over the config file, and `--auth-token` /
`--service-id` flags take precedence over both:

| Variable | Purpose |
| --- | --- |
| `NOTIFYCTL_SERVER_ADDR` | gRPC server address (default `localhost:50051`) |
| `NOTIFYCTL_SERVICE_ID` | Default service ID |
| `NOTIFYCTL_API_KEY` | API key for authenticated calls |

The server reads `DATABASE_URL` (Postgres) and `NATS_URL` from the environment,
falling back to the local Compose defaults.

## Project structure

```
notifyctl/
├── api/                  # Proto definitions (health, notify)
├── cmd/
│   ├── notifyctl/        # CLI entrypoint and commands
│   └── server/           # gRPC server entrypoint
├── internal/
│   ├── server/           # gRPC service implementations + auth
│   ├── store/postgres/   # Postgres stores (services, jobs, dead letters)
│   ├── retry/            # Backoff retry scheduler
│   ├── worker/           # Webhook delivery worker
│   ├── events/           # Delivery-status pub/sub hub
│   └── logging/          # Structured logging + log streaming
├── pkg/grpc/             # Generated protobuf code
├── e2e/                  # Full-stack end-to-end tests
├── scripts/cli_test.sh   # CLI smoke test
└── Makefile
```

## Development

```bash
make proto    # regenerate protobuf code
make build    # build server and CLI
make build-cli
make run      # run the server
```

## Testing

```bash
make test              # unit tests, no external services
make test-race         # unit tests under the race detector
make test-integration  # store-layer tests against real Postgres
make test-e2e          # full stack: gRPC + Postgres + NATS, DLQ lifecycle
make test-cli          # CLI smoke test: every command incl. DLQ list/replay
make test-all          # race + integration + e2e
```

`test-integration` and `test-e2e` need the Docker Compose stack and skip
gracefully when it is unreachable; override connection strings with
`DATABASE_URL` (integration) or `E2E_DATABASE_URL` / `E2E_NATS_URL` (e2e).
`test-cli` boots its own stack on non-standard host ports (set `KEEP_INFRA=1`
to leave it up, or `NO_COMPOSE=1` to reuse already-running infra).

## License

MIT License © 2026
