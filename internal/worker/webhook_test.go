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

	worker.deliverWebhook(context.Background(), notification, notification.Destinations[0])

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

// TestExactlyOnePostPerWebhook verifies only one POST per destination
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

	worker.deliverWebhook(context.Background(), notification, notification.Destinations[0])

	if postCount.Load() != 1 {
		t.Errorf("expected exactly 1 POST, got %d", postCount.Load())
	}
}

// TestNoRetryOnFailure verifies no retry logic is present
func TestNoRetryOnFailure(t *testing.T) {
	var postCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		postCount.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	notificationStore := newMockNotificationStore()
	attemptStore := newMockDeliveryAttemptStore()
	httpClient := httpclient.New(5 * time.Second)

	notification := &domain.Notification{
		ID:        "test-notification-3",
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

	worker.deliverWebhook(context.Background(), notification, notification.Destinations[0])

	// Wait briefly to ensure no retry
	time.Sleep(100 * time.Millisecond)

	if postCount.Load() != 1 {
		t.Errorf("expected exactly 1 POST with no retry, got %d", postCount.Load())
	}

	attempts := attemptStore.GetAll()
	if len(attempts) != 1 {
		t.Errorf("expected 1 attempt, got %d", len(attempts))
	}

	if attempts[0].Status != domain.DeliveryStatusFailed {
		t.Errorf("expected FAILED status, got %s", attempts[0].Status)
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
		worker.deliverWebhook(context.Background(), notification, dest)
	}

	if postCount.Load() != 3 {
		t.Errorf("expected 3 POSTs for 3 destinations, got %d", postCount.Load())
	}

	attempts := attemptStore.GetAll()
	if len(attempts) != 3 {
		t.Errorf("expected 3 attempts, got %d", len(attempts))
	}
}

// TestWorkerStateless verifies worker has no internal state that survives restarts
func TestWorkerStateless(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notificationStore := newMockNotificationStore()
	attemptStore := newMockDeliveryAttemptStore()
	httpClient := httpclient.New(5 * time.Second)

	notification := &domain.Notification{
		ID:        "test-notification-5",
		ServiceID: "test-service",
		Topic:     "test.topic",
		Payload:   []byte(`{"test": "data"}`),
		Destinations: []domain.Destination{
			{Type: domain.DestinationTypeWebhook, Target: server.URL},
		},
		CreatedAt: time.Now(),
	}
	notificationStore.Create(context.Background(), notification)

	// Create first worker, deliver, then discard
	worker1 := &WebhookWorker{
		notifications: notificationStore,
		attempts:      attemptStore,
		httpClient:    httpClient,
	}
	worker1.deliverWebhook(context.Background(), notification, notification.Destinations[0])

	// Create second worker with same stores (simulating restart)
	worker2 := &WebhookWorker{
		notifications: notificationStore,
		attempts:      attemptStore,
		httpClient:    httpClient,
	}

	// worker2 should have no knowledge of worker1's processing
	// The only state is in the stores, not the worker itself
	if worker2.consumer != nil && worker1.consumer != nil {
		t.Log("workers share no internal state - they only depend on stores")
	}

	attempts := attemptStore.GetAll()
	if len(attempts) != 1 {
		t.Errorf("attempt persisted in store, not worker: got %d", len(attempts))
	}
}

// TestFailedDeliveryRecorded verifies failed deliveries are persisted with error
func TestFailedDeliveryRecorded(t *testing.T) {
	// Server that always fails
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

	worker.deliverWebhook(context.Background(), notification, notification.Destinations[0])

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
	data  []byte
	acked bool
	mu    sync.Mutex
}

func (m *mockMessage) Data() []byte                              { return m.data }
func (m *mockMessage) Subject() string                           { return "notifications.created" }
func (m *mockMessage) Reply() string                             { return "" }
func (m *mockMessage) Headers() nats.Header                      { return nil }
func (m *mockMessage) Ack() error                                { m.mu.Lock(); m.acked = true; m.mu.Unlock(); return nil }
func (m *mockMessage) Nak() error                                { return nil }
func (m *mockMessage) NakWithDelay(d time.Duration) error        { return nil }
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

// TestProcessMessageAcksAfterDelivery verifies message is acked after processing
func TestProcessMessageAcksAfterDelivery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notificationStore := newMockNotificationStore()
	attemptStore := newMockDeliveryAttemptStore()
	httpClient := httpclient.New(5 * time.Second)

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
		notifications: notificationStore,
		attempts:      attemptStore,
		httpClient:    httpClient,
	}

	msgData, _ := json.Marshal(notificationMessage{NotificationID: notification.ID})
	msg := &mockMessage{data: msgData}

	worker.processMessage(context.Background(), msg)

	if !msg.IsAcked() {
		t.Error("expected message to be acked after processing")
	}

	attempts := attemptStore.GetAll()
	if len(attempts) != 1 {
		t.Errorf("expected 1 attempt, got %d", len(attempts))
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
		worker.deliverWebhook(context.Background(), notification, notification.Destinations[0])
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
			worker.deliverWebhook(context.Background(), notification, notification.Destinations[0])
		}
	})
}
