package retry

import "time"

type Config struct {
	MaxAttempts       int
	InitialBackoff    time.Duration
	MaxBackoff        time.Duration
	BackoffMultiplier float64
	JitterFactor      float64 // 0.0-1.0, percentage of jitter to add
}

func DefaultConfig() Config {
	return Config{
		MaxAttempts:       5,
		InitialBackoff:    1 * time.Second,
		MaxBackoff:        5 * time.Minute,
		BackoffMultiplier: 2.0,
		JitterFactor:      0.2, // 20% jitter
	}
}
