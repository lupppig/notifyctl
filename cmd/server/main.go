package main

import (
	"context"
	"log"
	"net"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/lupppig/notifyctl/internal/events"
	"github.com/lupppig/notifyctl/internal/server"
	"github.com/lupppig/notifyctl/internal/store/postgres"
	healthv1 "github.com/lupppig/notifyctl/pkg/grpc/health/v1"
	notifyv1 "github.com/lupppig/notifyctl/pkg/grpc/notify/v1"
)

const (
	grpcPort           = ":50051"
	defaultDatabaseURL = "postgres://notifyctl:your_secure_password_here@localhost:5432/notifyctl?sslmode=disable"
)

func main() {
	ctx := context.Background()

	// 1. Initialize Postgres
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = defaultDatabaseURL
	}

	db, err := postgres.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(ctx); err != nil {
		log.Fatalf("failed to run migrations: %v", err)
	}

	// 2. Initialize Internal Services
	eventHub := events.NewHub()
	serviceStore := postgres.NewServiceStore(db)

	// 3. Setup gRPC
	listener, err := net.Listen("tcp", grpcPort)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", grpcPort, err)
	}

	grpcServer := grpc.NewServer()

	// Register services
	healthv1.RegisterHealthServiceServer(grpcServer, server.NewHealthServer())
	notifyServer := server.NewNotifyServer(eventHub, serviceStore)
	notifyv1.RegisterNotifyServiceServer(grpcServer, notifyServer)

	reflection.Register(grpcServer)

	log.Printf("gRPC server listening on %s", grpcPort)
	if err := grpcServer.Serve(listener); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
