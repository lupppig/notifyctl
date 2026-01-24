package logging

import (
	"context"
	"log/slog"
	"os"
	"time"
)

type contextKey string

const (
	RequestIDKey   contextKey = "request_id"
	EventIDKey     contextKey = "event_id"
	ServiceIDKey   contextKey = "service_id"
	ServiceNameKey contextKey = "service_name"
)

// MultiHandler sends log records to multiple handlers.
type MultiHandler struct {
	handlers []slog.Handler
}

func (m *MultiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (m *MultiHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, h := range m.handlers {
		if err := h.Handle(ctx, r); err != nil {
			return err
		}
	}
	return nil
}

func (m *MultiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newHandlers := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		newHandlers[i] = h.WithAttrs(attrs)
	}
	return &MultiHandler{handlers: newHandlers}
}

func (m *MultiHandler) WithGroup(name string) slog.Handler {
	newHandlers := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		newHandlers[i] = h.WithGroup(name)
	}
	return &MultiHandler{handlers: newHandlers}
}

func Init() {
	// Custom time format: yyyy:mm:dd:HH:MM:SS -> 2006:01:02:15:04:05
	opts := &slog.HandlerOptions{
		Level: slog.LevelInfo,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				if t, ok := a.Value.Any().(time.Time); ok {
					return slog.String(a.Key, t.Format("2006:01:02:15:04:05"))
				}
			}
			return a
		},
	}

	// Stdout: Text format
	stdoutHandler := slog.NewTextHandler(os.Stdout, opts)

	// File: JSON format
	logFile, err := os.OpenFile("notifyctl.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		slog.Error("failed to open log file", slog.Any("error", err))
		slog.SetDefault(slog.New(stdoutHandler))
		return
	}

	jsonHandler := slog.NewJSONHandler(logFile, opts)

	logger := slog.New(&MultiHandler{
		handlers: []slog.Handler{stdoutHandler, jsonHandler},
	})
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
