//go:build e2e

// Package e2e boots the full notifyctl stack — real Postgres, real NATS,
// real gRPC server on a random port — and exercises the DLQ lifecycle the
// way production traffic would.
//
// Requirements (matching docker-compose.yml):
//
//	E2E_DATABASE_URL  postgres DSN (default: local compose instance)
//	E2E_NATS_URL      NATS URL    (default: nats://localhost:4222)
//
// Run with: make test-e2e   (or: go test -tags e2e -race ./e2e/...)
//
// IMPORTANT: the suite needs exclusive use of the database and the
// "notifications.jobs" subject. A manually started ./bin/server pointed at
// the same stack will race the harness's scheduler/worker and cause
// spurious failures — stop it first (pkill -x server).
package e2e

import (
	"context"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	"github.com/lupppig/notifyctl/internal/events"
	"github.com/lupppig/notifyctl/internal/retry"
	"github.com/lupppig/notifyctl/internal/server"
	"github.com/lupppig/notifyctl/internal/store/postgres"
	"github.com/lupppig/notifyctl/internal/worker"
	notifyv1 "github.com/lupppig/notifyctl/pkg/grpc/notify/v1"
)

const (
	pollInterval = 200 * time.Millisecond // scheduler cadence in tests
	waitTimeout  = 10 * time.Second
)

type harness struct {
	t      *testing.T
	db     *postgres.DB
	nc     *nats.Conn
	client notifyv1.NotifyServiceClient
	sched  *retry.Scheduler
	addr   string
}

func dsn() string {
	if v := os.Getenv("E2E_DATABASE_URL"); v != "" {
		return v
	}
	return "postgres://notifyctl:your_secure_password_here@localhost:55432/notifyctl?sslmode=disable"
}

func natsURL() string {
	if v := os.Getenv("E2E_NATS_URL"); v != "" {
		return v
	}
	return nats.DefaultURL
}

// newHarness boots the full stack. Each call runs Migrate (verifying its
// idempotency as a side effect) and wipes DLQ/job tables for isolation.
func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()

	db, err := postgres.New(ctx, dsn())
	if err != nil {
		t.Skipf("postgres unavailable (start docker compose first): %v", err)
	}
	t.Cleanup(db.Close)

	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	nc, err := nats.Connect(natsURL())
	if err != nil {
		t.Skipf("nats unavailable (start docker compose first): %v", err)
	}
	t.Cleanup(nc.Close)

	// Isolation: wipe data tables (order respects FKs).
	for _, q := range []string{
		`DELETE FROM dead_letters`,
		`DELETE FROM notification_stats`,
		`DELETE FROM notification_jobs`,
		`DELETE FROM delivery_attempts`,
		`DELETE FROM notifications`,
		`DELETE FROM services`,
	} {
		if _, err := db.Pool.Exec(ctx, q); err != nil {
			t.Fatalf("cleanup %q: %v", q, err)
		}
	}

	eventHub := events.NewHub()
	serviceStore := postgres.NewServiceStore(db)
	jobStore := postgres.NewNotificationJobStore(db)
	dlqStore := postgres.NewDeadLetterStore(db)

	sched := retry.NewScheduler(retry.DefaultConfig()).
		WithStore(jobStore).
		WithNATS(nc).
		WithDeadLetterStore(dlqStore).
		WithPollInterval(pollInterval)
	schedCtx, cancelSched := context.WithCancel(ctx)
	t.Cleanup(cancelSched)
	go sched.Start(schedCtx)

	w := worker.NewWorker(nc, jobStore)
	if err := w.Start(); err != nil {
		t.Fatalf("start worker: %v", err)
	}

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	auth := server.NewAuthInterceptor(serviceStore)
	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(auth.Unary()),
		grpc.ChainStreamInterceptor(auth.Stream()),
	)
	notifyv1.RegisterNotifyServiceServer(grpcServer,
		server.NewNotifyServer(eventHub, serviceStore, jobStore, dlqStore, nc))
	go grpcServer.Serve(lis)
	t.Cleanup(grpcServer.Stop)

	conn, err := grpc.NewClient(lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	return &harness{
		t:      t,
		db:     db,
		nc:     nc,
		client: notifyv1.NewNotifyServiceClient(conn),
		sched:  sched,
		addr:   lis.Addr().String(),
	}
}

// registerService creates a service and returns (serviceID, apiKey).
func (h *harness) registerService(name string) (string, string) {
	h.t.Helper()
	resp, err := h.client.RegisterService(context.Background(), &notifyv1.RegisterServiceRequest{
		Name:       name,
		WebhookUrl: "http://localhost:1/unreachable",
		Secret:     "s3cret",
	})
	if err != nil {
		h.t.Fatalf("register service: %v", err)
	}
	return resp.ServiceId, resp.ApiKey
}

func authCtx(apiKey string) context.Context {
	return metadata.AppendToOutgoingContext(context.Background(), "x-api-key", apiKey)
}

// send enqueues a notification and returns its ID.
func (h *harness) send(apiKey, serviceID, payload string) string {
	h.t.Helper()
	resp, err := h.client.SendNotification(authCtx(apiKey), &notifyv1.SendNotificationRequest{
		ServiceId: serviceID,
		Topic:     "e2e.test",
		Payload:   []byte(payload),
	})
	if err != nil {
		h.t.Fatalf("send notification: %v", err)
	}
	return resp.NotificationId
}

// exhaustRetries flips a job to FAILED with retry_count beyond MaxAttempts so
// the very next scheduler poll dead-letters it. (The simulated worker always
// "delivers", so failure is injected at the persistence layer — exactly what
// a crashed downstream would leave behind.)
func (h *harness) exhaustRetries(notificationID string) {
	h.t.Helper()
	_, err := h.db.Pool.Exec(context.Background(),
		`UPDATE notification_jobs SET status='FAILED', retry_count=10, next_retry_at=NOW() WHERE request_id=$1`,
		notificationID)
	if err != nil {
		h.t.Fatalf("exhaust retries: %v", err)
	}
}

func (h *harness) jobStatus(notificationID string) string {
	h.t.Helper()
	var status string
	err := h.db.Pool.QueryRow(context.Background(),
		`SELECT status FROM notification_jobs WHERE request_id=$1`, notificationID).Scan(&status)
	if err != nil {
		h.t.Fatalf("job status: %v", err)
	}
	return status
}

func (h *harness) deadLetterCount(notificationID string) int {
	h.t.Helper()
	var n int
	err := h.db.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM dead_letters WHERE notification_id=$1`, notificationID).Scan(&n)
	if err != nil {
		h.t.Fatalf("dead letter count: %v", err)
	}
	return n
}

// waitFor polls cond until true or the deadline elapses.
func (h *harness) waitFor(desc string, cond func() bool) {
	h.t.Helper()
	deadline := time.Now().Add(waitTimeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	h.t.Fatalf("timed out waiting for %s", desc)
}

// waitForDeadLetter waits until the notification is dead-lettered and returns
// the dead-letter ID.
func (h *harness) waitForDeadLetter(notificationID string) string {
	h.t.Helper()
	h.waitFor(fmt.Sprintf("dead letter for %s", notificationID), func() bool {
		return h.deadLetterCount(notificationID) == 1 && h.jobStatus(notificationID) == "DEAD_LETTERED"
	})
	var id string
	if err := h.db.Pool.QueryRow(context.Background(),
		`SELECT id FROM dead_letters WHERE notification_id=$1`, notificationID).Scan(&id); err != nil {
		h.t.Fatalf("dead letter id: %v", err)
	}
	return id
}
