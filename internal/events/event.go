package events

import "time"

type DeliveryStatus string

const (
	DeliveryStatusPending    DeliveryStatus = "PENDING"
	DeliveryStatusDelivering DeliveryStatus = "DELIVERING"
	DeliveryStatusDelivered  DeliveryStatus = "DELIVERED"
	DeliveryStatusFailed     DeliveryStatus = "FAILED"
	DeliveryStatusRetrying   DeliveryStatus = "RETRYING"
)

type DeliveryEvent struct {
	NotificationID string
	ServiceID      string
	Destination    string
	Status         DeliveryStatus
	Message        string
	Attempt        int
	Timestamp      time.Time
}
