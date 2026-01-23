package domain

import (
	"encoding/json"
	"time"
)

type DestinationType int

const (
	DestinationTypeUnspecified DestinationType = iota
	DestinationTypeWebhook
	DestinationTypeEmail
)

type Destination struct {
	Type   DestinationType
	Target string
}

type Notification struct {
	ID           string
	ServiceID    string
	Topic        string
	Payload      []byte
	Destinations []Destination
	CreatedAt    time.Time
}
type NotificationJob struct {
	RequestID   string          `json:"request_id" db:"request_id" bson:"request_id"`
	ServiceID   string          `json:"service_id" db:"service_id" bson:"service_id"`
	Payload     json.RawMessage `json:"payload" db:"payload" bson:"payload"`
	Status      string          `json:"status" db:"status" bson:"status"`
	RetryCount  int             `json:"retry_count" db:"retry_count" bson:"retry_count"`
	NextRetryAt *time.Time      `json:"next_retry_at,omitempty" db:"next_retry_at" bson:"next_retry_at,omitempty"`
	CreatedAt   time.Time       `json:"created_at" db:"created_at" bson:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at" db:"updated_at" bson:"updated_at"`
}
