package server

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/lupppig/notifyctl/internal/security"
	"github.com/lupppig/notifyctl/internal/store"
)

// AuthInterceptor handles API key validation for incoming gRPC requests.
type AuthInterceptor struct {
	serviceStore store.ServiceStore
}

func NewAuthInterceptor(serviceStore store.ServiceStore) *AuthInterceptor {
	return &AuthInterceptor{serviceStore: serviceStore}
}

func (a *AuthInterceptor) Unary() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		// Skip auth for registration and health checks
		if info.FullMethod == "/notify.v1.NotifyService/RegisterService" ||
			info.FullMethod == "/grpc.health.v1.Health/Check" {
			return handler(ctx, req)
		}

		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "missing metadata")
		}

		apiKeys := md.Get("x-api-key")
		if len(apiKeys) == 0 || apiKeys[0] == "" {
			return nil, status.Error(codes.Unauthenticated, "missing API key")
		}

		key := apiKeys[0]
		hash := security.HashKey(key)

		svc, err := a.serviceStore.GetByAPIKeyHash(ctx, hash)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "invalid API key")
		}

		// Inject service into context
		newCtx := context.WithValue(ctx, "service", svc)
		return handler(newCtx, req)
	}
}

func (a *AuthInterceptor) Stream() grpc.StreamServerInterceptor {
	return func(
		srv interface{},
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		md, ok := metadata.FromIncomingContext(ss.Context())
		if !ok {
			return status.Error(codes.Unauthenticated, "missing metadata")
		}

		apiKeys := md.Get("x-api-key")
		if len(apiKeys) == 0 || apiKeys[0] == "" {
			return status.Error(codes.Unauthenticated, "missing API key")
		}

		key := apiKeys[0]
		hash := security.HashKey(key)

		_, err := a.serviceStore.GetByAPIKeyHash(ss.Context(), hash)
		if err != nil {
			return status.Error(codes.Unauthenticated, "invalid API key")
		}

		return handler(srv, ss)
	}
}
