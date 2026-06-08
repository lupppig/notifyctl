package main

import (
	"context"
	"log"
	"net"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/lupppig/notifyctl/internal/events"
	"github.com/lupppig/notifyctl/internal/logging"
	"github.com/lupppig/notifyctl/internal/retry"
	"github.com/lupppig/notifyctl/internal/server"
	"github.com/lupppig/notifyctl/internal/store/postgres"
	"github.com/lupppig/notifyctl/internal/worker"
	healthv1 "github.com/lupppig/notifyctl/pkg/grpc/health/v1"
	notifyv1 "github.com/lupppig/notifyctl/pkg/grpc/notify/v1"
	"github.com/nats-io/nats.go"
)

const (
	grpcPort           = ":50051"
	defaultDatabaseURL = "postgres://notifyctl:your_secure_password_here@localhost:5432/notifyctl?sslmode=disable"
	defaultNatsURL     = nats.DefaultURL
)

func main() {
	logging.Init()
	ctx := context.Background()

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

	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = defaultNatsURL
	}
	nc, err := nats.Connect(natsURL)
	if err != nil {
		log.Fatalf("failed to connect to NATS: %v", err)
	}
	defer nc.Close()

	eventHub := events.NewHub()
	serviceStore := postgres.NewServiceStore(db)
	jobStore := postgres.NewNotificationJobStore(db)
	deadLetterStore := postgres.NewDeadLetterStore(db)

	sched := retry.NewScheduler(retry.DefaultConfig()).
		WithStore(jobStore).
		WithNATS(nc).
		WithDeadLetterStore(deadLetterStore)
	go sched.Start(ctx)

	w := worker.NewWorker(nc, jobStore)
	if err := w.Start(); err != nil {
		log.Fatalf("failed to start worker: %v", err)
	}

	listener, err := net.Listen("tcp", grpcPort)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", grpcPort, err)
	}

	auth := server.NewAuthInterceptor(serviceStore)
	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(auth.Unary()),
		grpc.ChainStreamInterceptor(auth.Stream()),
	)

	// Register services
	healthv1.RegisterHealthServiceServer(grpcServer, server.NewHealthServer())
	notifyServer := server.NewNotifyServer(eventHub, serviceStore, jobStore, deadLetterStore, nc)
	notifyv1.RegisterNotifyServiceServer(grpcServer, notifyServer)

	reflection.Register(grpcServer)

	log.Printf("gRPC server listening on %s", grpcPort)
	if err := grpcServer.Serve(listener); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
