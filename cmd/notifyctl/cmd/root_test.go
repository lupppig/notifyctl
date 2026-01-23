package cmd

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/metadata"
)

func TestNewCommandContext(t *testing.T) {
	// Reset global flags before/after tests
	origTimeout := timeout
	origAuthToken := authToken
	defer func() {
		timeout = origTimeout
		authToken = origAuthToken
	}()

	tests := []struct {
		name        string
		timeoutVal  time.Duration
		tokenVal    string
		expectOutMD string
	}{
		{
			name:       "Timeout only",
			timeoutVal: 100 * time.Millisecond,
			tokenVal:   "",
		},
		{
			name:        "Auth token only",
			timeoutVal:  0,
			tokenVal:    "my-token",
			expectOutMD: "my-token",
		},
		{
			name:        "Both timeout and token",
			timeoutVal:  500 * time.Millisecond,
			tokenVal:    "secret-token",
			expectOutMD: "secret-token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			timeout = tt.timeoutVal
			authToken = tt.tokenVal

			ctx, cancel := NewCommandContext(context.Background())
			defer cancel()

			if tt.timeoutVal > 0 {
				deadline, ok := ctx.Deadline()
				if !ok {
					t.Error("expected context to have a deadline")
				}
				expectedMax := time.Now().Add(tt.timeoutVal + time.Second)
				if deadline.After(expectedMax) {
					t.Errorf("deadline too far in the future: %v", deadline)
				}
			}

			if tt.tokenVal != "" {
				md, ok := metadata.FromOutgoingContext(ctx)
				if !ok {
					t.Error("expected metadata to be present in context")
				}
				tokens := md.Get("x-auth-token")
				if len(tokens) == 0 || tokens[0] != tt.tokenVal {
					t.Errorf("expected auth token %s, got %v", tt.tokenVal, tokens)
				}
			}
		})
	}
}
