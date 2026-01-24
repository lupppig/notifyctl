package retry

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/lupppig/notifyctl/internal/logging"
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

func (s *Scheduler) ShouldRetry(attempt int) bool {
	return attempt < s.config.MaxAttempts
}

func (s *Scheduler) NextDelay(attempt int) time.Duration {
	backoff := DefaultBackoff()
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
		slog.Warn("retry scheduler started in logic-only mode", slog.String("code", "SYS_STARTUP"))
		return
	}

	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	slog.Info("background retry scheduler started",
		slog.String("code", "SYS_STARTUP"),
		slog.Int("maxRetries", s.config.MaxAttempts),
		slog.Duration("pollInterval", s.pollInterval),
	)

	for {
		select {
		case <-ctx.Done():
			slog.Info("background retry scheduler shutting down", slog.String("code", "SYS_SHUTDOWN"))
			return
		case <-ticker.C:
			s.processRetries(ctx)
		}
	}
}

func (s *Scheduler) processRetries(ctx context.Context) {
	jobs, err := s.jobStore.GetRetryableJobs(ctx, 50)
	if err != nil {
		slog.Error("scheduler error fetching jobs", slog.String("code", "DB_ERROR"), slog.Any("error", err))
		return
	}

	for _, job := range jobs {
		ctx := logging.WithEventID(ctx, job.RequestID)
		ctx = logging.WithService(ctx, job.ServiceID, "")
		l := logging.FromContext(ctx)

		if !s.ShouldRetry(job.RetryCount) {
			l.Error("terminal failure: max retries exceeded",
				slog.String("code", "DEL_FAILED"),
				slog.Int("attempts", job.RetryCount),
				slog.Int("maxAttempts", s.config.MaxAttempts),
			)
			if err := s.jobStore.IncrementStats(ctx, job.ServiceID, "FAILED", time.Now()); err != nil {
				l.Warn("failed to increment stats", slog.String("code", "DB_ERROR"), slog.Any("error", err))
			}
			continue
		}

		nextDelay := s.NextDelay(job.RetryCount)
		l.Info("re-enqueueing job for retry",
			slog.String("code", "DEL_RETRY"),
			slog.Int("attempt", job.RetryCount+1),
			slog.Duration("delay", nextDelay),
		)

		data, err := json.Marshal(job)
		if err != nil {
			l.Error("failed to marshal job", slog.String("code", "SYS_ERR"), slog.Any("error", err))
			continue
		}

		if err := s.nc.Publish("notifications.jobs", data); err != nil {
			l.Error("failed to publish job to NATS", slog.String("code", "BROKER_ERROR"), slog.Any("error", err))
			continue
		}

		if err := s.jobStore.UpdateStatus(ctx, job.RequestID, "PENDING"); err != nil {
			l.Error("failed to update status to PENDING", slog.String("code", "DB_ERROR"), slog.Any("error", err))
		} else {
			l.Info("job re-enqueued and status updated to PENDING", slog.String("code", "JOB_REENQUEUED"))
		}
	}
}
