package broker

import "context"

type Publisher interface {
	Publish(ctx context.Context, subject string, data []byte) error
	PublishToDLQ(ctx context.Context, data []byte) error
	Close() error
}
