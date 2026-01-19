package retry

import (
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.MaxAttempts != 5 {
		t.Errorf("expected MaxAttempts 5, got %d", cfg.MaxAttempts)
	}
	if cfg.InitialBackoff != 1*time.Second {
		t.Errorf("expected InitialBackoff 1s, got %v", cfg.InitialBackoff)
	}
	if cfg.MaxBackoff != 5*time.Minute {
		t.Errorf("expected MaxBackoff 5m, got %v", cfg.MaxBackoff)
	}
	if cfg.BackoffMultiplier != 2.0 {
		t.Errorf("expected BackoffMultiplier 2.0, got %f", cfg.BackoffMultiplier)
	}
	if cfg.JitterFactor != 0.2 {
		t.Errorf("expected JitterFactor 0.2, got %f", cfg.JitterFactor)
	}
}

func TestShouldRetry(t *testing.T) {
	scheduler := NewScheduler(DefaultConfig())

	tests := []struct {
		attemptCount int
		shouldRetry  bool
	}{
		{0, true},
		{1, true},
		{2, true},
		{3, true},
		{4, true},
		{5, false}, // max attempts reached
		{6, false},
		{10, false},
	}

	for _, tt := range tests {
		result := scheduler.ShouldRetry(tt.attemptCount)
		if result != tt.shouldRetry {
			t.Errorf("ShouldRetry(%d) = %v, want %v", tt.attemptCount, result, tt.shouldRetry)
		}
	}
}

func TestExponentialBackoff(t *testing.T) {
	cfg := Config{
		MaxAttempts:       5,
		InitialBackoff:    1 * time.Second,
		MaxBackoff:        1 * time.Minute,
		BackoffMultiplier: 2.0,
		JitterFactor:      0, // No jitter for predictable testing
	}
	scheduler := NewScheduler(cfg)

	// Expected delays without jitter: 1s, 2s, 4s, 8s, 16s (capped at 60s)
	expectedDelays := []time.Duration{
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
	}

	for i, expected := range expectedDelays {
		delay := scheduler.NextDelay(i)
		if delay != expected {
			t.Errorf("NextDelay(%d) = %v, want %v", i, delay, expected)
		}
	}
}

func TestBackoffCappedAtMax(t *testing.T) {
	cfg := Config{
		MaxAttempts:       10,
		InitialBackoff:    1 * time.Second,
		MaxBackoff:        10 * time.Second,
		BackoffMultiplier: 2.0,
		JitterFactor:      0,
	}
	scheduler := NewScheduler(cfg)

	// After attempt 3 (8s), attempt 4 would be 16s but capped at 10s
	delay := scheduler.NextDelay(4)
	if delay != 10*time.Second {
		t.Errorf("expected delay capped at 10s, got %v", delay)
	}

	delay = scheduler.NextDelay(10)
	if delay != 10*time.Second {
		t.Errorf("expected delay capped at 10s for high attempt, got %v", delay)
	}
}

func TestJitterApplied(t *testing.T) {
	cfg := Config{
		MaxAttempts:       5,
		InitialBackoff:    1 * time.Second,
		MaxBackoff:        1 * time.Minute,
		BackoffMultiplier: 2.0,
		JitterFactor:      0.2, // 20% jitter
	}
	scheduler := NewScheduler(cfg)

	// Run multiple times to verify jitter creates variation
	delays := make(map[time.Duration]bool)
	for i := 0; i < 100; i++ {
		delay := scheduler.NextDelay(0)
		delays[delay] = true
	}

	// With jitter, we should see multiple different delays
	if len(delays) < 2 {
		t.Error("expected jitter to produce varying delays, but got uniform delays")
	}

	// All delays should be within expected range (1s +/- 20%)
	for delay := range delays {
		if delay < 800*time.Millisecond || delay > 1200*time.Millisecond {
			t.Errorf("delay %v outside expected jitter range (800ms-1200ms)", delay)
		}
	}
}

func TestMinimumDelay(t *testing.T) {
	cfg := Config{
		MaxAttempts:       5,
		InitialBackoff:    10 * time.Millisecond, // Very small
		MaxBackoff:        1 * time.Minute,
		BackoffMultiplier: 2.0,
		JitterFactor:      0.5, // Large jitter could make it negative
	}
	scheduler := NewScheduler(cfg)

	for i := 0; i < 100; i++ {
		delay := scheduler.NextDelay(0)
		if delay < 100*time.Millisecond {
			t.Errorf("delay %v below minimum 100ms", delay)
		}
	}
}

func TestNoInfiniteRetries(t *testing.T) {
	scheduler := NewScheduler(DefaultConfig())

	// Simulate repeated attempts - should eventually return false
	maxAttempts := scheduler.MaxAttempts()
	for i := 0; i <= maxAttempts+5; i++ {
		shouldRetry := scheduler.ShouldRetry(i)
		if i >= maxAttempts && shouldRetry {
			t.Errorf("ShouldRetry returned true for attempt %d, max is %d", i, maxAttempts)
		}
	}
}

func BenchmarkNextDelay(b *testing.B) {
	scheduler := NewScheduler(DefaultConfig())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scheduler.NextDelay(i % 5)
	}
}

func BenchmarkShouldRetry(b *testing.B) {
	scheduler := NewScheduler(DefaultConfig())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scheduler.ShouldRetry(i % 10)
	}
}
