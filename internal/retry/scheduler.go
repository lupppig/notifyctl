package retry

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/lupppig/notifyctl/internal/store"
	"github.com/nats-io/nats.go"
)

// Scheduler handles both retry logic (backoff/counts) and background job polling.
type Scheduler struct {
	config       Config
	jobStore     store.NotificationJobStore
	nc           *nats.Conn
	pollInterval time.Duration
}

// NewScheduler creates a new Scheduler with the logic-only configuration.
func NewScheduler(cfg Config) *Scheduler {
	return &Scheduler{
		config:       cfg,
		pollInterval: 5 * time.Second,
	}
}

// WithStore adds a job store to the scheduler for background polling.
func (s *Scheduler) WithStore(jobStore store.NotificationJobStore) *Scheduler {
	s.jobStore = jobStore
	return s
}

// WithNATS adds a NATS connection to the scheduler for background polling.
func (s *Scheduler) WithNATS(nc *nats.Conn) *Scheduler {
	s.nc = nc
	return s
}

// ShouldRetry returns true if the job should be retried based on attempt count.
func (s *Scheduler) ShouldRetry(attempt int) bool {
	return attempt < s.config.MaxAttempts
}

// NextDelay calculates the next backoff delay using the config.
func (s *Scheduler) NextDelay(attempt int) time.Duration {
	// Standard exponential backoff logic
	backoff := DefaultBackoff() // Use the utility we created
	backoff.BaseDelay = s.config.InitialBackoff
	backoff.MaxDelay = s.config.MaxBackoff
	backoff.Factor = s.config.BackoffMultiplier
	backoff.Jitter = s.config.JitterFactor

	return backoff.NextDelay(attempt)
}

// MaxAttempts returns the maximum configured retry attempts.
func (s *Scheduler) MaxAttempts() int {
	return s.config.MaxAttempts
}

// Start runs the background polling loop.
func (s *Scheduler) Start(ctx context.Context) {
	if s.jobStore == nil || s.nc == nil {
		log.Println("Retry scheduler started in logic-only mode (no DB or NATS)")
		return
	}

	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	log.Printf("Background retry scheduler started (maxRetries=%d, pollInterval=%v)", s.config.MaxAttempts, s.pollInterval)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.processRetries(ctx)
		}
	}
}

func (s *Scheduler) processRetries(ctx context.Context) {
	jobs, err := s.jobStore.GetRetryableJobs(ctx, 50)
	if err != nil {
		log.Printf("Scheduler error fetching jobs: %v", err)
		return
	}

	for _, job := range jobs {
		if !s.ShouldRetry(job.RetryCount) {
			log.Printf("[%s] ACTION: Terminal Failure | Job %s exceeded max retries (%d)",
				time.Now().Format(time.RFC3339), job.RequestID, s.config.MaxAttempts)
			continue
		}

		nextDelay := s.NextDelay(job.RetryCount)
		log.Printf("[%s] ACTION: Re-enqueueing Job | ID: %s | Attempt: %d/%d | Next Delay: %v",
			time.Now().Format(time.RFC3339), job.RequestID, job.RetryCount+1, s.config.MaxAttempts, nextDelay)

		// Re-publish to NATS
		data, err := json.Marshal(job)
		if err != nil {
			log.Printf("[ERROR] Failed to marshal job %s: %v", job.RequestID, err)
			continue
		}

		if err := s.nc.Publish("notifications.jobs", data); err != nil {
			log.Printf("[ERROR] Failed to publish job %s to NATS: %v", job.RequestID, err)
			continue
		}

		// Update status to PENDING
		if err := s.jobStore.UpdateStatus(ctx, job.RequestID, "PENDING"); err != nil {
			log.Printf("[ERROR] Failed to update status to PENDING for job %s: %v", job.RequestID, err)
		} else {
			log.Printf("[%s] SUCCESS: Job %s re-enqueued and status updated to PENDING",
				time.Now().Format(time.RFC3339), job.RequestID)
		}
	}
}
