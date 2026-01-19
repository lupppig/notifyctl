package domain

import "time"

type DeliveryStatus string

const (
	DeliveryStatusSuccess DeliveryStatus = "SUCCESS"
	DeliveryStatusFailed  DeliveryStatus = "FAILED"
)

type DeliveryAttempt struct {
	ID             string
	NotificationID string
	Destination    string
	Status         DeliveryStatus
	StatusCode     int
	ResponseBody   string
	Error          string
	AttemptedAt    time.Time
}
