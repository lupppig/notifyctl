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

# Build the server
make build

# Run the server
make run

# Clean build artifacts
make clean
```

---

## License

MIT License © 2026