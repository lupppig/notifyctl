package retry

import (
	"math"
	"math/rand"
	"time"
)

// Backoff handles exponential backoff calculations with jitter.
type Backoff struct {
	BaseDelay time.Duration
	MaxDelay  time.Duration
	Factor    float64
	Jitter    float64
}

// DefaultBackoff returns a standard backoff configuration.
func DefaultBackoff() *Backoff {
	return &Backoff{
		BaseDelay: 1 * time.Second,
		MaxDelay:  30 * time.Minute,
		Factor:    2.0,
		Jitter:    0.1,
	}
}

// NextDelay calculates the next delay based on the attempt count.
func (b *Backoff) NextDelay(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}

	delay := float64(b.BaseDelay) * math.Pow(b.Factor, float64(attempt))
	if delay > float64(b.MaxDelay) {
		delay = float64(b.MaxDelay)
	}

	// Add jitter
	if b.Jitter > 0 {
		jitterRange := delay * b.Jitter
		jitter := (rand.Float64() * 2 * jitterRange) - jitterRange
		delay += jitter
	}

	// Enforce 100ms minimum floor
	if delay < float64(100*time.Millisecond) {
		delay = float64(100 * time.Millisecond)
	}

	return time.Duration(delay)
}
