package domain

import "time"

type Service struct {
	ID         string
	Name       string
	WebhookURL string
	Secret     string
	APIKey     string
	CreatedAt  time.Time
}
