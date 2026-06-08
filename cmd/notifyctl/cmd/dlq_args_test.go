package cmd

import (
	"context"
	"os"
	"strings"
	"testing"

	"google.golang.org/grpc"

	"github.com/lupppig/notifyctl/internal/config"
	notifyv1 "github.com/lupppig/notifyctl/pkg/grpc/notify/v1"
)

// TestDLQReplayRequiresExactlyOneArg covers the cobra.ExactArgs(1) validator on
// `dlq replay`.
func TestDLQReplayRequiresExactlyOneArg(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{"no args", []string{}, true},
		{"one arg", []string{"dl-1"}, false},
		{"two args", []string{"dl-1", "dl-2"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := dlqReplayCmd.Args(dlqReplayCmd, tc.args)
			if tc.wantErr && err == nil {
				t.Errorf("expected arg-count error for %v", tc.args)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected arg error for %v: %v", tc.args, err)
			}
		})
	}
}

// TestDLQListServiceIDFlagFallsBackToConfig verifies the request carries the
// flag value when set, otherwise the configured service ID.
func TestDLQListServiceIDFlagFallsBackToConfig(t *testing.T) {
	originalFactory := clientFactory
	originalCfg := cfg
	defer func() {
		clientFactory = originalFactory
		cfg = originalCfg
		dlqServiceID = ""
	}()

	var gotServiceID string
	mock := &mockNotifyClient{
		listDeadLettersFunc: func(ctx context.Context, in *notifyv1.ListDeadLettersRequest) (*notifyv1.ListDeadLettersResponse, error) {
			gotServiceID = in.ServiceId
			return &notifyv1.ListDeadLettersResponse{}, nil
		},
	}
	clientFactory = func(conn grpc.ClientConnInterface) notifyv1.NotifyServiceClient { return mock }

	// Flag empty -> falls back to cfg.ServiceID.
	cfg = &config.Config{ServiceID: "cfg-svc"}
	dlqServiceID = ""
	if err := dlqListCmd.RunE(dlqListCmd, []string{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotServiceID != "cfg-svc" {
		t.Errorf("expected fallback to cfg service id, got %q", gotServiceID)
	}

	// Flag set -> flag wins.
	dlqServiceID = "flag-svc"
	if err := dlqListCmd.RunE(dlqListCmd, []string{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotServiceID != "flag-svc" {
		t.Errorf("expected flag to win, got %q", gotServiceID)
	}
}

// TestDLQPreRunRequiresAPIKey exercises the root PersistentPreRunE dlq branch:
// a dlq subcommand without an API key must error before any RPC.
//
// PersistentPreRunE calls config.Load(configPath), which reads a file and the
// NOTIFYCTL_* env vars, so we point configPath at an empty temp file and clear
// the API-key env var to make the result deterministic.
func TestDLQPreRunRequiresAPIKey(t *testing.T) {
	originalToken := authToken
	originalConfigPath := configPath
	originalServiceIDFlag := globalServiceID
	defer func() {
		authToken = originalToken
		configPath = originalConfigPath
		globalServiceID = originalServiceIDFlag
	}()

	// Empty config file (exists, parses to defaults) so Load doesn't pick up a
	// real ~/.notifyctl.yaml.
	emptyCfg := t.TempDir() + "/empty.yaml"
	if err := os.WriteFile(emptyCfg, []byte(""), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	configPath = emptyCfg
	authToken = ""
	globalServiceID = ""
	t.Setenv("NOTIFYCTL_API_KEY", "")
	t.Setenv("NOTIFYCTL_SERVICE_ID", "")

	// dlqListCmd's parent is dlqCmd ("dlq"), matching the prerun branch.
	err := rootCmd.PersistentPreRunE(dlqListCmd, []string{})
	if err == nil || !strings.Contains(err.Error(), "API key") {
		t.Errorf("expected missing-API-key error for dlq command, got %v", err)
	}
}
