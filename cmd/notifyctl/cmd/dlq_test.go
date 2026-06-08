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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	notifyv1 "github.com/lupppig/notifyctl/pkg/grpc/notify/v1"
)

func (m *mockNotifyClient) ListDeadLetters(ctx context.Context, in *notifyv1.ListDeadLettersRequest, opts ...grpc.CallOption) (*notifyv1.ListDeadLettersResponse, error) {
	return m.listDeadLettersFunc(ctx, in)
}

func (m *mockNotifyClient) ReplayDeadLetter(ctx context.Context, in *notifyv1.ReplayDeadLetterRequest, opts ...grpc.CallOption) (*notifyv1.ReplayDeadLetterResponse, error) {
	return m.replayDeadLetterFunc(ctx, in)
}

func TestDLQList(t *testing.T) {
	originalFactory := clientFactory
	defer func() { clientFactory = originalFactory }()

	var gotServiceID string
	mock := &mockNotifyClient{
		listDeadLettersFunc: func(ctx context.Context, in *notifyv1.ListDeadLettersRequest) (*notifyv1.ListDeadLettersResponse, error) {
			gotServiceID = in.ServiceId
			return &notifyv1.ListDeadLettersResponse{
				DeadLetters: []*notifyv1.DeadLetter{
					{Id: "dl-1", NotificationId: "n-1", ServiceId: "svc-1", LastError: "HTTP 500", AttemptCount: 5, FailedAt: "2026-06-08T10:00:00Z"},
					{Id: "dl-2", NotificationId: "n-2", ServiceId: "svc-1", LastError: "connection refused", AttemptCount: 5, FailedAt: "2026-06-08T11:00:00Z"},
				},
			}, nil
		},
	}
	clientFactory = func(conn grpc.ClientConnInterface) notifyv1.NotifyServiceClient { return mock }

	dlqServiceID = "svc-1"
	defer func() { dlqServiceID = "" }()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := dlqListCmd.RunE(dlqListCmd, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	if gotServiceID != "svc-1" {
		t.Errorf("expected request service_id svc-1, got %q", gotServiceID)
	}
	for _, want := range []string{"dl-1", "dl-2", "HTTP 500", "connection refused", "5"} {
		if !strings.Contains(output, want) {
			t.Errorf("expected output to contain %q, got: %s", want, output)
		}
	}
}

func TestDLQListJSON(t *testing.T) {
	originalFactory := clientFactory
	defer func() { clientFactory = originalFactory }()

	mock := &mockNotifyClient{
		listDeadLettersFunc: func(ctx context.Context, in *notifyv1.ListDeadLettersRequest) (*notifyv1.ListDeadLettersResponse, error) {
			return &notifyv1.ListDeadLettersResponse{
				DeadLetters: []*notifyv1.DeadLetter{
					{Id: "dl-1", NotificationId: "n-1", ServiceId: "svc-1", LastError: "HTTP 500", AttemptCount: 5, FailedAt: "2026-06-08T10:00:00Z"},
				},
			}, nil
		},
	}
	clientFactory = func(conn grpc.ClientConnInterface) notifyv1.NotifyServiceClient { return mock }

	dlqServiceID = "svc-1"
	defer func() { dlqServiceID = "" }()

	jsonOut = true
	defer func() { jsonOut = false }()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := dlqListCmd.RunE(dlqListCmd, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)

	var letters []*notifyv1.DeadLetter
	if err := json.Unmarshal(buf.Bytes(), &letters); err != nil {
		t.Fatalf("failed to unmarshal JSON output: %v, output: %s", err, buf.String())
	}
	if len(letters) != 1 || letters[0].Id != "dl-1" || letters[0].LastError != "HTTP 500" {
		t.Errorf("unexpected JSON content: %+v", letters)
	}
}

func TestDLQListQuiet(t *testing.T) {
	originalFactory := clientFactory
	defer func() { clientFactory = originalFactory }()

	mock := &mockNotifyClient{
		listDeadLettersFunc: func(ctx context.Context, in *notifyv1.ListDeadLettersRequest) (*notifyv1.ListDeadLettersResponse, error) {
			return &notifyv1.ListDeadLettersResponse{
				DeadLetters: []*notifyv1.DeadLetter{
					{Id: "dl-1", NotificationId: "n-1", ServiceId: "svc-1", LastError: "HTTP 500", AttemptCount: 5, FailedAt: "2026-06-08T10:00:00Z"},
					{Id: "dl-2", NotificationId: "n-2", ServiceId: "svc-1", LastError: "timeout", AttemptCount: 5, FailedAt: "2026-06-08T11:00:00Z"},
				},
			}, nil
		},
	}
	clientFactory = func(conn grpc.ClientConnInterface) notifyv1.NotifyServiceClient { return mock }

	dlqServiceID = "svc-1"
	defer func() { dlqServiceID = "" }()

	quiet = true
	defer func() { quiet = false }()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := dlqListCmd.RunE(dlqListCmd, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)

	if got := strings.TrimSpace(buf.String()); got != "dl-1\ndl-2" {
		t.Errorf("expected quiet output to be IDs only, got: %q", got)
	}
}

func TestDLQReplay(t *testing.T) {
	originalFactory := clientFactory
	defer func() { clientFactory = originalFactory }()

	var gotID string
	mock := &mockNotifyClient{
		replayDeadLetterFunc: func(ctx context.Context, in *notifyv1.ReplayDeadLetterRequest) (*notifyv1.ReplayDeadLetterResponse, error) {
			gotID = in.Id
			return &notifyv1.ReplayDeadLetterResponse{NotificationId: "n-1"}, nil
		},
	}
	clientFactory = func(conn grpc.ClientConnInterface) notifyv1.NotifyServiceClient { return mock }

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := dlqReplayCmd.RunE(dlqReplayCmd, []string{"dl-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	if gotID != "dl-1" {
		t.Errorf("expected replay to request dead-letter id dl-1, got %q", gotID)
	}
	if !strings.Contains(output, "dl-1") || !strings.Contains(output, "n-1") {
		t.Errorf("expected output to mention dead-letter and notification ids, got: %s", output)
	}
}

func TestDLQReplayQuiet(t *testing.T) {
	originalFactory := clientFactory
	defer func() { clientFactory = originalFactory }()

	mock := &mockNotifyClient{
		replayDeadLetterFunc: func(ctx context.Context, in *notifyv1.ReplayDeadLetterRequest) (*notifyv1.ReplayDeadLetterResponse, error) {
			return &notifyv1.ReplayDeadLetterResponse{NotificationId: "n-42"}, nil
		},
	}
	clientFactory = func(conn grpc.ClientConnInterface) notifyv1.NotifyServiceClient { return mock }

	quiet = true
	defer func() { quiet = false }()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := dlqReplayCmd.RunE(dlqReplayCmd, []string{"dl-9"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)

	if got := strings.TrimSpace(buf.String()); got != "n-42" {
		t.Errorf("expected quiet output to be the notification id only, got: %q", got)
	}
}

func TestDLQReplayError(t *testing.T) {
	originalFactory := clientFactory
	defer func() { clientFactory = originalFactory }()

	mock := &mockNotifyClient{
		replayDeadLetterFunc: func(ctx context.Context, in *notifyv1.ReplayDeadLetterRequest) (*notifyv1.ReplayDeadLetterResponse, error) {
			return nil, status.Error(codes.NotFound, "dead letter not found")
		},
	}
	clientFactory = func(conn grpc.ClientConnInterface) notifyv1.NotifyServiceClient { return mock }

	err := dlqReplayCmd.RunE(dlqReplayCmd, []string{"missing"})
	if status.Code(err) != codes.NotFound {
		t.Errorf("expected NotFound error to propagate, got %v", err)
	}
}

func TestDLQListEmpty(t *testing.T) {
	originalFactory := clientFactory
	defer func() { clientFactory = originalFactory }()

	mock := &mockNotifyClient{
		listDeadLettersFunc: func(ctx context.Context, in *notifyv1.ListDeadLettersRequest) (*notifyv1.ListDeadLettersResponse, error) {
			return &notifyv1.ListDeadLettersResponse{}, nil
		},
	}
	clientFactory = func(conn grpc.ClientConnInterface) notifyv1.NotifyServiceClient { return mock }

	dlqServiceID = "svc-1"
	defer func() { dlqServiceID = "" }()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := dlqListCmd.RunE(dlqListCmd, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)

	if !strings.Contains(buf.String(), "No dead letters found.") {
		t.Errorf("expected empty message, got: %s", buf.String())
	}
}
