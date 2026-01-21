package grpc

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func UnaryAuthInterceptor(apiKey, serviceID string) grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req, reply interface{},
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		md := metadata.Pairs(
			"x-api-key", apiKey,
			"x-service-id", serviceID,
		)
		
		if exMD, ok := metadata.FromOutgoingContext(ctx); ok {
			md = metadata.Join(exMD, md)
		}

		newCtx := metadata.NewOutgoingContext(ctx, md)
		return invoker(newCtx, method, req, reply, cc, opts...)
	}
}
