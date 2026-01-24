package logging

import (
	"context"
	"log/slog"
	"os"
)

type contextKey string

const (
	RequestIDKey   contextKey = "request_id"
	EventIDKey     contextKey = "event_id"
	ServiceIDKey   contextKey = "service_id"
	ServiceNameKey contextKey = "service_name"
)

func Init() {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	logger := slog.New(handler)
	slog.SetDefault(logger)
}

func FromContext(ctx context.Context) *slog.Logger {
	l := slog.Default()
	if val, ok := ctx.Value(RequestIDKey).(string); ok {
		l = l.With("request_id", val)
	}
	if val, ok := ctx.Value(EventIDKey).(string); ok {
		l = l.With("event_id", val)
	}
	if val, ok := ctx.Value(ServiceIDKey).(string); ok {
		l = l.With("service_id", val)
	}
	if val, ok := ctx.Value(ServiceNameKey).(string); ok {
		l = l.With("service_name", val)
	}
	return l
}

func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, RequestIDKey, id)
}

func WithEventID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, EventIDKey, id)
}

func WithService(ctx context.Context, id, name string) context.Context {
	ctx = context.WithValue(ctx, ServiceIDKey, id)
	return context.WithValue(ctx, ServiceNameKey, name)
}
