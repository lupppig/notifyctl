# 🛎️ Multi-Channel Notification Platform

**A distributed, event-driven notification platform for Slack, Discord, WhatsApp, and Email, operated via a CLI (`notifyctl`) built in Go and gRPC.**

---

## 🚀 Features

* **Multi-channel delivery** – Slack, Discord, WhatsApp, Email
* **Event-driven architecture** – reliable, decoupled notifications
* **Rule Engine** – YAML-based rules, aggregation, and filtering
* **CLI (`notifyctl`)** – send events, manage rules, view metrics, audit logs
* **Metrics & Observability** – Prometheus metrics, OpenTelemetry tracing
* **Admin APIs** – health checks, rule reload, pause/resume notifications
* **Security** – token-based authentication, RBAC, mTLS
* **Reliability** – retries, backpressure handling, dead-letter queues

---

## 💻 CLI Examples

### Send an event

```bash
notifyctl send \
  --channel slack \
  --channel-id "#alerts" \
  --source payments \
  --severity critical \
  --env prod \
  --message "Stripe charge failed"
```

### Apply rules

```bash
notifyctl rules apply -f rules.yaml
```

### Check metrics

```bash
notifyctl stats --since 1h
```

### Audit event

```bash
notifyctl audit describe <event-id>
```

---

## 🏗️ Architecture

```
Event Producers (CLI, Apps)
          |
          v
  Event Ingestion Service
          |
          v
     Message Bus (NATS JetStream)
          |
          v
      Rule Engine
          |
          v
Notification Dispatcher → Slack | Discord | WhatsApp | Email
```

---

## 📦 Installation

```bash
go install github.com/yourusername/notifyctl@latest
```

> Requires Go 1.20+ and API credentials for supported channels.

---

## 🧰 Technologies

* **Go** – Core language
* **gRPC** – Communication between services
* **Cobra** – CLI
* **NATS JetStream** – Event bus
* **Zap/Zerolog** – Logging
* **Prometheus & Grafana** – Metrics
* **OpenTelemetry** – Tracing
* **Slack / Discord / Twilio / SendGrid APIs** – Notification channels

---

## 📂 Repository Structure

```
/cmd/notifyctl        # CLI application
/internal/ingestion   # Event ingestion service
/internal/dispatcher  # Notification dispatcher
/internal/rules       # Rule engine
/internal/bus         # Message bus integration
/internal/admin       # Admin APIs
/config               # YAML config and rules
```

---

## 🤝 Contributing

1. Fork the repo
2. Create a branch (`git checkout -b feature-name`)
3. Commit changes (`git commit -am 'Add new feature'`)
4. Push (`git push origin feature-name`)
5. Open a Pull Request

---

## 📄 License

MIT License © 2026