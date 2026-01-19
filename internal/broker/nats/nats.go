package nats

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	StreamName     = "NOTIFICATIONS"
	StreamSubjects = "notifications.>"
)

type Publisher struct {
	conn   *nats.Conn
	js     jetstream.JetStream
	stream jetstream.Stream
}

func New(ctx context.Context, url string) (*Publisher, error) {
	conn, err := nats.Connect(url)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	js, err := jetstream.New(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to create JetStream context: %w", err)
	}

	stream, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     StreamName,
		Subjects: []string{StreamSubjects},
	})
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to create stream: %w", err)
	}

	return &Publisher{
		conn:   conn,
		js:     js,
		stream: stream,
	}, nil
}

func (p *Publisher) Publish(ctx context.Context, subject string, data []byte) error {
	_, err := p.js.Publish(ctx, subject, data)
	if err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}
	return nil
}

func (p *Publisher) Close() error {
	p.conn.Close()
	return nil
}
