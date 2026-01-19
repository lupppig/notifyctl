package worker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/lupppig/notifyctl/internal/domain"
	"github.com/lupppig/notifyctl/internal/httpclient"
	"github.com/lupppig/notifyctl/internal/retry"
)

// mockNotificationStore implements store.NotificationStore for testing
type mockNotificationStore struct {
	notifications map[string]*domain.Notification
	mu            sync.RWMutex
}

func newMockNotificationStore() *mockNotificationStore {
	return &mockNotificationStore{
		notifications: make(map[string]*domain.Notification),
	}
}

func (s *mockNotificationStore) Create(ctx context.Context, n *domain.Notification) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notifications[n.ID] = n
	return nil
}

func (s *mockNotificationStore) GetByID(ctx context.Context, id string) (*domain.Notification, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.notifications[id], nil
}

// mockDeliveryAttemptStore implements store.DeliveryAttemptStore for testing
type mockDeliveryAttemptStore struct {
	attempts []*domain.DeliveryAttempt
	mu       sync.Mutex
}

func newMockDeliveryAttemptStore() *mockDeliveryAttemptStore {
	return &mockDeliveryAttemptStore{
		attempts: make([]*domain.DeliveryAttempt, 0),
	}
}

func (s *mockDeliveryAttemptStore) Create(ctx context.Context, attempt *domain.DeliveryAttempt) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempts = append(s.attempts, attempt)
	return nil
}

func (s *mockDeliveryAttemptStore) GetAll() []*domain.DeliveryAttempt {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]*domain.DeliveryAttempt, len(s.attempts))
	copy(result, s.attempts)
	return result
}

// mockPublisher implements broker.Publisher for testing
type mockPublisher struct {
	dlqMessages [][]byte
	mu          sync.Mutex
}

func newMockPublisher() *mockPublisher {
	return &mockPublisher{
		dlqMessages: make([][]byte, 0),
	}
}

func (p *mockPublisher) Publish(ctx context.Context, subject string, data []byte) error {
	return nil
}

func (p *mockPublisher) PublishToDLQ(ctx context.Context, data []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.dlqMessages = append(p.dlqMessages, data)
	return nil
}

func (p *mockPublisher) Close() error {
	return nil
}

func (p *mockPublisher) GetDLQMessages() [][]byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := make([][]byte, len(p.dlqMessages))
	copy(result, p.dlqMessages)
	return result
}

// TestEveryDeliveryAttemptPersisted verifies that every delivery attempt is recorded
func TestEveryDeliveryAttemptPersisted(t *testing.T) {
	var postCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		postCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notificationStore := newMockNotificationStore()
	attemptStore := newMockDeliveryAttemptStore()
	httpClient := httpclient.New(5 * time.Second)

	notification := &domain.Notification{
		ID:        "test-notification-1",
		ServiceID: "test-service",
		Topic:     "test.topic",
		Payload:   []byte(`{"test": "data"}`),
		Destinations: []domain.Destination{
			{Type: domain.DestinationTypeWebhook, Target: server.URL},
		},
		CreatedAt: time.Now(),
	}
	notificationStore.Create(context.Background(), notification)

	worker := &WebhookWorker{
		notifications: notificationStore,
		attempts:      attemptStore,
		httpClient:    httpClient,
	}

	worker.deliverWebhook(context.Background(), notification, notification.Destinations[0], 0)

	attempts := attemptStore.GetAll()
	if len(attempts) != 1 {
		t.Errorf("expected 1 delivery attempt, got %d", len(attempts))
	}

	if attempts[0].NotificationID != notification.ID {
		t.Errorf("attempt notification ID mismatch")
	}

	if attempts[0].Status != domain.DeliveryStatusSuccess {
		t.Errorf("expected SUCCESS status, got %s", attempts[0].Status)
	}
}

// TestExactlyOnePostPerWebhook verifies only one POST per destination per attempt
func TestExactlyOnePostPerWebhook(t *testing.T) {
	var postCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		postCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notificationStore := newMockNotificationStore()
	attemptStore := newMockDeliveryAttemptStore()
	httpClient := httpclient.New(5 * time.Second)

	notification := &domain.Notification{
		ID:        "test-notification-2",
		ServiceID: "test-service",
		Topic:     "test.topic",
		Payload:   []byte(`{"test": "data"}`),
		Destinations: []domain.Destination{
			{Type: domain.DestinationTypeWebhook, Target: server.URL},
		},
		CreatedAt: time.Now(),
	}
	notificationStore.Create(context.Background(), notification)

	worker := &WebhookWorker{
		notifications: notificationStore,
		attempts:      attemptStore,
		httpClient:    httpClient,
	}

	worker.deliverWebhook(context.Background(), notification, notification.Destinations[0], 0)

	if postCount.Load() != 1 {
		t.Errorf("expected exactly 1 POST, got %d", postCount.Load())
	}
}

// TestMultipleDestinations verifies each destination gets one POST
func TestMultipleDestinations(t *testing.T) {
	var postCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		postCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notificationStore := newMockNotificationStore()
	attemptStore := newMockDeliveryAttemptStore()
	httpClient := httpclient.New(5 * time.Second)

	notification := &domain.Notification{
		ID:        "test-notification-4",
		ServiceID: "test-service",
		Topic:     "test.topic",
		Payload:   []byte(`{"test": "data"}`),
		Destinations: []domain.Destination{
			{Type: domain.DestinationTypeWebhook, Target: server.URL + "/endpoint1"},
			{Type: domain.DestinationTypeWebhook, Target: server.URL + "/endpoint2"},
			{Type: domain.DestinationTypeWebhook, Target: server.URL + "/endpoint3"},
		},
		CreatedAt: time.Now(),
	}
	notificationStore.Create(context.Background(), notification)

	worker := &WebhookWorker{
		notifications: notificationStore,
		attempts:      attemptStore,
		httpClient:    httpClient,
	}

	for _, dest := range notification.Destinations {
		worker.deliverWebhook(context.Background(), notification, dest, 0)
	}

	if postCount.Load() != 3 {
		t.Errorf("expected 3 POSTs for 3 destinations, got %d", postCount.Load())
	}

	attempts := attemptStore.GetAll()
	if len(attempts) != 3 {
		t.Errorf("expected 3 attempts, got %d", len(attempts))
	}
}

// TestFailedDeliveryRecorded verifies failed deliveries are persisted with error
func TestFailedDeliveryRecorded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(`{"error": "upstream failed"}`))
	}))
	defer server.Close()

	notificationStore := newMockNotificationStore()
	attemptStore := newMockDeliveryAttemptStore()
	httpClient := httpclient.New(5 * time.Second)

	notification := &domain.Notification{
		ID:        "test-notification-6",
		ServiceID: "test-service",
		Topic:     "test.topic",
		Payload:   []byte(`{"test": "data"}`),
		Destinations: []domain.Destination{
			{Type: domain.DestinationTypeWebhook, Target: server.URL},
		},
		CreatedAt: time.Now(),
	}
	notificationStore.Create(context.Background(), notification)

	worker := &WebhookWorker{
		notifications: notificationStore,
		attempts:      attemptStore,
		httpClient:    httpClient,
	}

	worker.deliverWebhook(context.Background(), notification, notification.Destinations[0], 0)

	attempts := attemptStore.GetAll()
	if len(attempts) != 1 {
		t.Fatalf("expected 1 attempt, got %d", len(attempts))
	}

	attempt := attempts[0]
	if attempt.Status != domain.DeliveryStatusFailed {
		t.Errorf("expected FAILED status, got %s", attempt.Status)
	}
	if attempt.StatusCode != http.StatusBadGateway {
		t.Errorf("expected status code %d, got %d", http.StatusBadGateway, attempt.StatusCode)
	}
	if attempt.ResponseBody == "" {
		t.Error("expected response body to be recorded")
	}
}

// mockMessage implements jetstream.Msg for testing processMessage
type mockMessage struct {
	data      []byte
	acked     bool
	nakDelay  time.Duration
	nakCalled bool
	mu        sync.Mutex
}

func (m *mockMessage) Data() []byte         { return m.data }
func (m *mockMessage) Subject() string      { return "notifications.created" }
func (m *mockMessage) Reply() string        { return "" }
func (m *mockMessage) Headers() nats.Header { return nil }
func (m *mockMessage) Ack() error           { m.mu.Lock(); m.acked = true; m.mu.Unlock(); return nil }
func (m *mockMessage) Nak() error           { m.mu.Lock(); m.nakCalled = true; m.mu.Unlock(); return nil }
func (m *mockMessage) NakWithDelay(d time.Duration) error {
	m.mu.Lock()
	m.nakCalled = true
	m.nakDelay = d
	m.mu.Unlock()
	return nil
}
func (m *mockMessage) InProgress() error                         { return nil }
func (m *mockMessage) Term() error                               { return nil }
func (m *mockMessage) TermWithReason(reason string) error        { return nil }
func (m *mockMessage) DoubleAck(ctx context.Context) error       { return nil }
func (m *mockMessage) Metadata() (*jetstream.MsgMetadata, error) { return nil, nil }

func (m *mockMessage) IsAcked() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.acked
}

func (m *mockMessage) GetNakDelay() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.nakDelay
}

func (m *mockMessage) WasNakCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.nakCalled
}

// TestProcessMessageAcksOnSuccess verifies message is acked after successful delivery
func TestProcessMessageAcksOnSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notificationStore := newMockNotificationStore()
	attemptStore := newMockDeliveryAttemptStore()
	httpClient := httpclient.New(5 * time.Second)
	publisher := newMockPublisher()
	scheduler := retry.NewScheduler(retry.DefaultConfig())

	notification := &domain.Notification{
		ID:        "test-notification-7",
		ServiceID: "test-service",
		Topic:     "test.topic",
		Payload:   []byte(`{"test": "data"}`),
		Destinations: []domain.Destination{
			{Type: domain.DestinationTypeWebhook, Target: server.URL},
		},
		CreatedAt: time.Now(),
	}
	notificationStore.Create(context.Background(), notification)

	worker := &WebhookWorker{
		notifications:  notificationStore,
		attempts:       attemptStore,
		httpClient:     httpClient,
		publisher:      publisher,
		retryScheduler: scheduler,
	}

	msgData, _ := json.Marshal(notificationMessage{NotificationID: notification.ID, AttemptCount: 0})
	msg := &mockMessage{data: msgData}

	worker.processMessage(context.Background(), msg)

	if !msg.IsAcked() {
		t.Error("expected message to be acked after successful delivery")
	}

	if msg.WasNakCalled() {
		t.Error("NAK should not be called on success")
	}
}

// TestProcessMessageNaksOnFailureForRetry verifies failed message is NAK'd with delay for retry
func TestProcessMessageNaksOnFailureForRetry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	notificationStore := newMockNotificationStore()
	attemptStore := newMockDeliveryAttemptStore()
	httpClient := httpclient.New(5 * time.Second)
	publisher := newMockPublisher()
	scheduler := retry.NewScheduler(retry.DefaultConfig())

	notification := &domain.Notification{
		ID:        "test-notification-8",
		ServiceID: "test-service",
		Topic:     "test.topic",
		Payload:   []byte(`{"test": "data"}`),
		Destinations: []domain.Destination{
			{Type: domain.DestinationTypeWebhook, Target: server.URL},
		},
		CreatedAt: time.Now(),
	}
	notificationStore.Create(context.Background(), notification)

	worker := &WebhookWorker{
		notifications:  notificationStore,
		attempts:       attemptStore,
		httpClient:     httpClient,
		publisher:      publisher,
		retryScheduler: scheduler,
	}

	msgData, _ := json.Marshal(notificationMessage{NotificationID: notification.ID, AttemptCount: 0})
	msg := &mockMessage{data: msgData}

	worker.processMessage(context.Background(), msg)

	if msg.IsAcked() {
		t.Error("message should not be acked on failure with retries remaining")
	}

	if !msg.WasNakCalled() {
		t.Error("NAK should be called for retry")
	}

	if msg.GetNakDelay() <= 0 {
		t.Error("NAK delay should be positive for backoff")
	}
}

// TestDLQOnMaxAttempts verifies message goes to DLQ after max attempts
func TestDLQOnMaxAttempts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	notificationStore := newMockNotificationStore()
	attemptStore := newMockDeliveryAttemptStore()
	httpClient := httpclient.New(5 * time.Second)
	publisher := newMockPublisher()

	cfg := retry.Config{
		MaxAttempts:       3,
		InitialBackoff:    100 * time.Millisecond,
		MaxBackoff:        1 * time.Second,
		BackoffMultiplier: 2.0,
		JitterFactor:      0,
	}
	scheduler := retry.NewScheduler(cfg)

	notification := &domain.Notification{
		ID:        "test-notification-dlq",
		ServiceID: "test-service",
		Topic:     "test.topic",
		Payload:   []byte(`{"test": "data"}`),
		Destinations: []domain.Destination{
			{Type: domain.DestinationTypeWebhook, Target: server.URL},
		},
		CreatedAt: time.Now(),
	}
	notificationStore.Create(context.Background(), notification)

	worker := &WebhookWorker{
		notifications:  notificationStore,
		attempts:       attemptStore,
		httpClient:     httpClient,
		publisher:      publisher,
		retryScheduler: scheduler,
	}

	// Simulate max attempts reached (attemptCount >= MaxAttempts)
	msgData, _ := json.Marshal(notificationMessage{NotificationID: notification.ID, AttemptCount: 3})
	msg := &mockMessage{data: msgData}

	worker.processMessage(context.Background(), msg)

	if !msg.IsAcked() {
		t.Error("message should be acked after moving to DLQ")
	}

	if msg.WasNakCalled() {
		t.Error("NAK should not be called after max attempts")
	}

	dlqMessages := publisher.GetDLQMessages()
	if len(dlqMessages) != 1 {
		t.Errorf("expected 1 DLQ message, got %d", len(dlqMessages))
	}
}

// TestNoInfiniteRetriesInWorker verifies no infinite retry loop
func TestNoInfiniteRetriesInWorker(t *testing.T) {
	var postCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		postCount.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	notificationStore := newMockNotificationStore()
	attemptStore := newMockDeliveryAttemptStore()
	httpClient := httpclient.New(5 * time.Second)
	publisher := newMockPublisher()

	cfg := retry.Config{
		MaxAttempts:       3,
		InitialBackoff:    10 * time.Millisecond,
		MaxBackoff:        100 * time.Millisecond,
		BackoffMultiplier: 2.0,
		JitterFactor:      0,
	}
	scheduler := retry.NewScheduler(cfg)

	notification := &domain.Notification{
		ID:        "test-notification-no-infinite",
		ServiceID: "test-service",
		Topic:     "test.topic",
		Payload:   []byte(`{"test": "data"}`),
		Destinations: []domain.Destination{
			{Type: domain.DestinationTypeWebhook, Target: server.URL},
		},
		CreatedAt: time.Now(),
	}
	notificationStore.Create(context.Background(), notification)

	worker := &WebhookWorker{
		notifications:  notificationStore,
		attempts:       attemptStore,
		httpClient:     httpClient,
		publisher:      publisher,
		retryScheduler: scheduler,
	}

	// Simulate multiple attempts
	for attempt := 0; attempt <= cfg.MaxAttempts+2; attempt++ {
		msgData, _ := json.Marshal(notificationMessage{NotificationID: notification.ID, AttemptCount: attempt})
		msg := &mockMessage{data: msgData}
		worker.processMessage(context.Background(), msg)
	}

	// Should have exactly (MaxAttempts + 1) POSTs plus any after max
	// But DLQ should be triggered for attempts >= MaxAttempts
	dlqMessages := publisher.GetDLQMessages()
	if len(dlqMessages) < 1 {
		t.Error("expected at least 1 message in DLQ after max attempts")
	}
}

// BenchmarkWebhookDelivery benchmarks the delivery performance
func BenchmarkWebhookDelivery(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notificationStore := newMockNotificationStore()
	attemptStore := newMockDeliveryAttemptStore()
	httpClient := httpclient.New(5 * time.Second)

	notification := &domain.Notification{
		ID:        "bench-notification",
		ServiceID: "test-service",
		Topic:     "test.topic",
		Payload:   []byte(`{"test": "data"}`),
		Destinations: []domain.Destination{
			{Type: domain.DestinationTypeWebhook, Target: server.URL},
		},
		CreatedAt: time.Now(),
	}
	notificationStore.Create(context.Background(), notification)

	worker := &WebhookWorker{
		notifications: notificationStore,
		attempts:      attemptStore,
		httpClient:    httpClient,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		worker.deliverWebhook(context.Background(), notification, notification.Destinations[0], 0)
	}
}

// BenchmarkConcurrentDeliveries benchmarks concurrent webhook delivery
func BenchmarkConcurrentDeliveries(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notificationStore := newMockNotificationStore()
	attemptStore := newMockDeliveryAttemptStore()
	httpClient := httpclient.New(5 * time.Second)

	notification := &domain.Notification{
		ID:        "bench-concurrent",
		ServiceID: "test-service",
		Topic:     "test.topic",
		Payload:   []byte(`{"test": "data"}`),
		Destinations: []domain.Destination{
			{Type: domain.DestinationTypeWebhook, Target: server.URL},
		},
		CreatedAt: time.Now(),
	}
	notificationStore.Create(context.Background(), notification)

	worker := &WebhookWorker{
		notifications: notificationStore,
		attempts:      attemptStore,
		httpClient:    httpClient,
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			worker.deliverWebhook(context.Background(), notification, notification.Destinations[0], 0)
		}
	})
}
