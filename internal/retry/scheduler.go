package retry

import (
	"math"
	"math/rand"
	"time"
)

type Scheduler struct {
	config Config
}

func NewScheduler(config Config) *Scheduler {
	return &Scheduler{config: config}
}

// ShouldRetry returns true if attempt count is below max attempts
func (s *Scheduler) ShouldRetry(attemptCount int) bool {
	return attemptCount < s.config.MaxAttempts
}

// NextDelay calculates the next retry delay with exponential backoff and jitter.
// Formula: min(initialBackoff * (multiplier ^ attempt), maxBackoff) + jitter
func (s *Scheduler) NextDelay(attemptCount int) time.Duration {
	backoff := float64(s.config.InitialBackoff) * math.Pow(s.config.BackoffMultiplier, float64(attemptCount))

	if backoff > float64(s.config.MaxBackoff) {
		backoff = float64(s.config.MaxBackoff)
	}

	// Add jitter: random value between -jitter% and +jitter%
	if s.config.JitterFactor > 0 {
		jitterRange := backoff * s.config.JitterFactor
		jitter := (rand.Float64()*2 - 1) * jitterRange // -jitterRange to +jitterRange
		backoff += jitter
	}

	// Ensure minimum delay of 100ms
	if backoff < float64(100*time.Millisecond) {
		backoff = float64(100 * time.Millisecond)
	}

	return time.Duration(backoff)
}

// MaxAttempts returns the configured max attempts
func (s *Scheduler) MaxAttempts() int {
	return s.config.MaxAttempts
}
