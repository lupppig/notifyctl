package domain

import "time"

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
