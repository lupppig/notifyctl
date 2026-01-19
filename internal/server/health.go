package server

import (
	"context"

	healthv1 "github.com/lupppig/notifyctl/pkg/grpc/health/v1"
)

type HealthServer struct {
	healthv1.UnimplementedHealthServiceServer
}

func NewHealthServer() *HealthServer {
	return &HealthServer{}
}

func (s *HealthServer) Check(ctx context.Context, req *healthv1.HealthCheckRequest) (*healthv1.HealthCheckResponse, error) {
	return &healthv1.HealthCheckResponse{
		Status: "SERVING",
	}, nil
}
