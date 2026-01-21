package grpc

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func TestUnaryAuthInterceptor(t *testing.T) {
	apiKey := "test-api-key"
	serviceID := "test-service-id"
	interceptor := UnaryAuthInterceptor(apiKey, serviceID)

	// Mock invoker that checks metadata
	invoker := func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		md, ok := metadata.FromOutgoingContext(ctx)
		if !ok {
			t.Fatal("no metadata found in context")
		}

		if keys := md.Get("x-api-key"); len(keys) == 0 || keys[0] != apiKey {
			t.Errorf("expected x-api-key %s, got %v", apiKey, keys)
		}

		if ids := md.Get("x-service-id"); len(ids) == 0 || ids[0] != serviceID {
			t.Errorf("expected x-service-id %s, got %v", serviceID, ids)
		}

		return nil
	}

	err := interceptor(context.Background(), "/test.Method", nil, nil, nil, invoker)
	if err != nil {
		t.Fatalf("interceptor returned error: %v", err)
	}
}

func TestUnaryAuthInterceptor_MergesMetadata(t *testing.T) {
	apiKey := "test-api-key"
	serviceID := "test-service-id"
	interceptor := UnaryAuthInterceptor(apiKey, serviceID)

	existingMD := metadata.Pairs("custom-header", "custom-value")
	ctx := metadata.NewOutgoingContext(context.Background(), existingMD)

	invoker := func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		md, ok := metadata.FromOutgoingContext(ctx)
		if !ok {
			t.Fatal("no metadata found in context")
		}

		if v := md.Get("custom-header"); len(v) == 0 || v[0] != "custom-value" {
			t.Errorf("expected custom-header custom-value, got %v", v)
		}
		if v := md.Get("x-api-key"); len(v) == 0 || v[0] != apiKey {
			t.Errorf("expected x-api-key %s, got %v", apiKey, v)
		}

		return nil
	}

	err := interceptor(ctx, "/test.Method", nil, nil, nil, invoker)
	if err != nil {
		t.Fatalf("interceptor returned error: %v", err)
	}
}
