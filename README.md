# notifyctl

A CLI-driven, gRPC-only notification and webhook delivery platform for backend teams.

**notifyctl** lets services emit events once and have them delivered safely and asynchronously to multiple destinations (webhooks, email, etc.), with retries, delivery guarantees, and real-time status streaming — all managed through a developer-friendly CLI.

---

## Features

- **CLI-first** — Send events, manage destinations, and monitor delivery from the terminal
- **gRPC-only** — No REST, pure gRPC for service-to-service communication
- **Reliable delivery** — Retries, backoff, and dead-letter handling
- **Real-time streaming** — Watch delivery status as it happens
- **Multi-destination** — Webhooks, email, and more
- **Observable** — Prometheus metrics and structured logging

---

## Quick Start

```bash
# Start the server
make run

# Send an event (CLI coming in future phases)
notifyctl send --topic orders.created --payload '{"id": "123"}'

# Watch delivery status
notifyctl watch --event-id abc123
```

---

## Architecture

```
Services/CLI
     │
     ▼
┌─────────────┐
│ gRPC Server │  ◄── Event ingestion
└─────────────┘
     │
     ▼
┌─────────────┐
│  Dispatcher │  ◄── Retry logic, delivery guarantees
└─────────────┘
     │
     ▼
┌─────────────────────────┐
│ Webhooks │ Email │ ...  │
└─────────────────────────┘
```

---

## Project Structure

```
notifyctl/
├── api/                    # Proto definitions
│   └── health/v1/
├── cmd/
│   └── server/             # gRPC server entrypoint
├── internal/
│   └── server/             # Service implementations
├── pkg/
│   └── grpc/               # Generated protobuf code
├── Makefile
└── go.mod
```

---

## Development

```bash
# Generate proto code
make proto

# Build everything
make build

# Build CLI only
make build-cli

# Run the server
make run
```

## Testing

### Automated Tests
```bash
go test ./...
```

### Manual Verification
1. Start infrastructure: `docker-compose up -d`
2. Run server: `make run`
3. Use CLI: `./bin/notifyctl --help`

See the [testing_guide.md](file:///home/klein/.gemini/antigravity/brain/9867e884-cb25-431b-bda3-455aba942b45/testing_guide.md) for a detailed walkthrough of the end-to-end flow.

---

## License

MIT License © 2026