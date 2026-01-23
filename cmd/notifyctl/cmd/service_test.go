package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"google.golang.org/grpc"

	notifyv1 "github.com/lupppig/notifyctl/pkg/grpc/notify/v1"
)

type mockNotifyClient struct {
	notifyv1.NotifyServiceClient
	registerFunc func(ctx context.Context, in *notifyv1.RegisterServiceRequest) (*notifyv1.RegisterServiceResponse, error)
	listFunc     func(ctx context.Context, in *notifyv1.ListServicesRequest) (*notifyv1.ListServicesResponse, error)
}

func (m *mockNotifyClient) RegisterService(ctx context.Context, in *notifyv1.RegisterServiceRequest, opts ...grpc.CallOption) (*notifyv1.RegisterServiceResponse, error) {
	return m.registerFunc(ctx, in)
}

func (m *mockNotifyClient) ListServices(ctx context.Context, in *notifyv1.ListServicesRequest, opts ...grpc.CallOption) (*notifyv1.ListServicesResponse, error) {
	return m.listFunc(ctx, in)
}

func TestServiceCreate(t *testing.T) {
	// Setup mock
	originalFactory := clientFactory
	defer func() { clientFactory = originalFactory }()

	mock := &mockNotifyClient{
		registerFunc: func(ctx context.Context, in *notifyv1.RegisterServiceRequest) (*notifyv1.RegisterServiceResponse, error) {
			return &notifyv1.RegisterServiceResponse{
				ServiceId: "svc-123",
				ApiKey:    "key-456",
			}, nil
		},
	}
	clientFactory = func(conn grpc.ClientConnInterface) notifyv1.NotifyServiceClient { return mock }

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run command
	svcName = "test-service"
	svcWebhookURL = "http://localhost:8080"
	svcSecret = "secret"

	err := createServiceCmd.RunE(createServiceCmd, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	if !strings.Contains(output, "Service created successfully!") {
		t.Errorf("expected success message, got: %s", output)
	}
	if !strings.Contains(output, "svc-123") {
		t.Errorf("expected service ID, got: %s", output)
	}
}

func TestServiceList(t *testing.T) {
	// Setup mock
	originalFactory := clientFactory
	defer func() { clientFactory = originalFactory }()

	mock := &mockNotifyClient{
		listFunc: func(ctx context.Context, in *notifyv1.ListServicesRequest) (*notifyv1.ListServicesResponse, error) {
			return &notifyv1.ListServicesResponse{
				Services: []*notifyv1.ServiceInfo{
					{Id: "1", Name: "S1", WebhookUrl: "U1"},
					{Id: "2", Name: "S2", WebhookUrl: "U2"},
				},
			}, nil
		},
	}
	clientFactory = func(conn grpc.ClientConnInterface) notifyv1.NotifyServiceClient { return mock }

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := listServicesCmd.RunE(listServicesCmd, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	if !strings.Contains(output, "S1") || !strings.Contains(output, "S2") {
		t.Errorf("expected S1 and S2 in output, got: %s", output)
	}
}

func TestServiceListJSON(t *testing.T) {
	// Setup mock
	originalFactory := clientFactory
	defer func() { clientFactory = originalFactory }()

	mock := &mockNotifyClient{
		listFunc: func(ctx context.Context, in *notifyv1.ListServicesRequest) (*notifyv1.ListServicesResponse, error) {
			return &notifyv1.ListServicesResponse{
				Services: []*notifyv1.ServiceInfo{
					{Id: "1", Name: "S1", WebhookUrl: "U1"},
				},
			}, nil
		},
	}
	clientFactory = func(conn grpc.ClientConnInterface) notifyv1.NotifyServiceClient { return mock }

	jsonOut = true
	defer func() { jsonOut = false }()

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := listServicesCmd.RunE(listServicesCmd, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	var services []*notifyv1.ServiceInfo
	if err := json.Unmarshal(buf.Bytes(), &services); err != nil {
		t.Fatalf("failed to unmarshal JSON output: %v, output: %s", err, output)
	}
	if len(services) != 1 || services[0].Name != "S1" {
		t.Errorf("unexpected JSON content: %+v", services[0])
	}
}
