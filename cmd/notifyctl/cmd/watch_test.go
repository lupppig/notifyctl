package cmd

import (
	"context"
	"io"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/lupppig/notifyctl/internal/config"
	notifyv1 "github.com/lupppig/notifyctl/pkg/grpc/notify/v1"
)

type mockStreamClient struct {
	grpc.ClientStream
	ctx    context.Context
	events []*notifyv1.DeliveryStatusEvent
	index  int
}

func (m *mockStreamClient) Recv() (*notifyv1.DeliveryStatusEvent, error) {
	if m.index >= len(m.events) {
		return nil, io.EOF
	}
	event := m.events[m.index]
	m.index++
	return event, nil
}

func (m *mockStreamClient) Context() context.Context {
	return m.ctx
}

func (m *mockStreamClient) Header() (metadata.MD, error) { return nil, nil }
func (m *mockStreamClient) Trailer() metadata.MD         { return nil }
func (m *mockStreamClient) CloseSend() error             { return nil }

func TestWatchCommand(t *testing.T) {
	// Setup mock
	originalFactory := clientFactory
	defer func() { clientFactory = originalFactory }()

	events := []*notifyv1.DeliveryStatusEvent{
		{
			NotificationId: "notif-1",
			Destination:    "http://webhook.com",
			Status:         notifyv1.DeliveryStatus_DELIVERY_STATUS_DELIVERED,
			Message:        "OK",
		},
	}

	mock := &mockNotifyClient{
		streamFunc: func(ctx context.Context, in *notifyv1.StreamDeliveryStatusRequest) (notifyv1.NotifyService_StreamDeliveryStatusClient, error) {
			return &mockStreamClient{
				ctx:    ctx,
				events: events,
			}, nil
		},
	}
	clientFactory = func(conn grpc.ClientConnInterface) notifyv1.NotifyServiceClient { return mock }

	// Run command
	watchRequestID = "notif-1"
	globalServiceID = ""
	quiet = true
	defer func() { quiet = false }()

	cfg = &config.Config{ServiceID: globalServiceID}

	err := watchCmd.RunE(watchCmd, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
