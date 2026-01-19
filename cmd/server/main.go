package main

import (
	"log"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/lupppig/notifyctl/internal/server"
	healthv1 "github.com/lupppig/notifyctl/pkg/grpc/health/v1"
)

const grpcPort = ":50051"

func main() {
	listener, err := net.Listen("tcp", grpcPort)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", grpcPort, err)
	}

	grpcServer := grpc.NewServer()
	healthv1.RegisterHealthServiceServer(grpcServer, server.NewHealthServer())
	reflection.Register(grpcServer)

	log.Printf("gRPC server listening on %s", grpcPort)
	if err := grpcServer.Serve(listener); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

