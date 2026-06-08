package retry

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/lupppig/notifyctl/internal/domain"
	"github.com/lupppig/notifyctl/internal/logging"
	"github.com/lupppig/notifyctl/internal/store"
	"github.com/nats-io/nats.go"
)

// Scheduler handles both retry logic (backoff/counts) and background job polling.
type Scheduler struct {
	config          Config
	jobStore        store.NotificationJobStore
	deadLetterStore store.DeadLetterStore
	nc              *nats.Conn
	pollInterval    time.Duration
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

// WithDeadLetterStore adds a dead-letter store so jobs that exhaust max
// retries are persisted for later inspection.
func (s *Scheduler) WithDeadLetterStore(dlq store.DeadLetterStore) *Scheduler {
	s.deadLetterStore = dlq
	return s
}

// WithPollInterval overrides the background poll cadence (default 5s).
// Primarily for tests that need fast end-to-end cycles.
func (s *Scheduler) WithPollInterval(d time.Duration) *Scheduler {
	if d > 0 {
		s.pollInterval = d
	}
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
			s.deadLetterJob(ctx, l, job)
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

// deadLetterJob handles a job that has exhausted its retries: it persists the
// job to the dead-letter store, then marks it DEAD_LETTERED so the poll loop
// never picks it up again. The DLQ insert happens first so a crash in between
// leaves the job FAILED and the next poll retries the whole sequence (the
// insert is idempotent per notification).
func (s *Scheduler) deadLetterJob(ctx context.Context, l *slog.Logger, job *domain.NotificationJob) {
	l.Error("terminal failure: max retries exceeded, dead-lettering job",
		slog.String("code", "DEL_DEAD_LETTERED"),
		slog.Int("attempts", job.RetryCount),
		slog.Int("maxAttempts", s.config.MaxAttempts),
	)

	if s.deadLetterStore != nil {
		dl := &domain.DeadLetter{
			ID:             uuid.New().String(),
			NotificationID: job.RequestID,
			ServiceID:      job.ServiceID,
			Payload:        job.Payload,
			LastError:      "max retries exceeded",
			AttemptCount:   job.RetryCount,
			FailedAt:       time.Now(),
		}
		if err := s.deadLetterStore.Create(ctx, dl); err != nil {
			l.Error("failed to persist dead letter", slog.String("code", "DB_ERROR"), slog.Any("error", err))
			return // job stays FAILED; retried next poll
		}
	}

	if err := s.jobStore.UpdateStatus(ctx, job.RequestID, "DEAD_LETTERED"); err != nil {
		l.Error("failed to update status to DEAD_LETTERED", slog.String("code", "DB_ERROR"), slog.Any("error", err))
		return
	}

	if err := s.jobStore.IncrementStats(ctx, job.ServiceID, "FAILED", time.Now()); err != nil {
		l.Warn("failed to increment stats", slog.String("code", "DB_ERROR"), slog.Any("error", err))
	}
}
