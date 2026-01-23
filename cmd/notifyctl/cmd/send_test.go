package cmd

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"google.golang.org/grpc"

	"github.com/lupppig/notifyctl/internal/config"
	notifyv1 "github.com/lupppig/notifyctl/pkg/grpc/notify/v1"
)

func TestSendNotification(t *testing.T) {
	// Setup mock
	originalFactory := clientFactory
	defer func() { clientFactory = originalFactory }()

	var capturedReq *notifyv1.SendNotificationRequest
	mock := &mockNotifyClient{
		sendFunc: func(ctx context.Context, in *notifyv1.SendNotificationRequest) (*notifyv1.SendNotificationResponse, error) {
			capturedReq = in
			return &notifyv1.SendNotificationResponse{
				NotificationId: "notif-123",
			}, nil
		},
	}
	clientFactory = func(conn grpc.ClientConnInterface) notifyv1.NotifyServiceClient { return mock }

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run command
	globalServiceID = "svc-1"
	sendRawPayload = `{"topic": "test-topic", "payload": {"foo": "bar"}, "destinations": [{"type": 1, "target": "http://webhook.com"}]}`
	sendPayloadPath = ""
	jsonOut = true

	cfg = &config.Config{ServiceID: globalServiceID}

	err := sendCmd.RunE(sendCmd, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	if !strings.Contains(output, "notif-123") {
		t.Errorf("expected notification ID, got: %s", output)
	}

	if capturedReq.ServiceId != "svc-1" {
		t.Errorf("expected service ID svc-1, got: %s", capturedReq.ServiceId)
	}
	if capturedReq.Topic != "test-topic" {
		t.Errorf("expected topic test-topic, got: %s", capturedReq.Topic)
	}
}

func TestSendNotificationFromFile(t *testing.T) {
	// Setup mock
	originalFactory := clientFactory
	defer func() { clientFactory = originalFactory }()

	mock := &mockNotifyClient{
		sendFunc: func(ctx context.Context, in *notifyv1.SendNotificationRequest) (*notifyv1.SendNotificationResponse, error) {
			return &notifyv1.SendNotificationResponse{
				NotificationId: "notif-456",
			}, nil
		},
	}
	clientFactory = func(conn grpc.ClientConnInterface) notifyv1.NotifyServiceClient { return mock }

	// Create temp payload file
	tmpFile, err := os.CreateTemp("", "payload-*.json")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	content := `{"topic": "file-topic", "payload": {"hello": "world"}}`
	if _, err := tmpFile.Write([]byte(content)); err != nil {
		t.Fatalf("failed to write to temp file: %v", err)
	}
	tmpFile.Close()

	// Capture stdout
	oldStdout := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w

	// Run command
	globalServiceID = "svc-file"
	sendPayloadPath = tmpFile.Name()
	sendRawPayload = ""
	jsonOut = true

	cfg = &config.Config{ServiceID: globalServiceID}

	err = sendCmd.RunE(sendCmd, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	w.Close()
	os.Stdout = oldStdout
}
