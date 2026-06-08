package domain

import "time"

type DeadLetter struct {
	ID             string
	NotificationID string
	ServiceID      string
	Payload        []byte
	LastError      string
	AttemptCount   int
	FailedAt       time.Time
}
