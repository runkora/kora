package triggers

import (
	"context"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// Worker drains the pending-delivery queue.
//
// Every fire is durably queued before it dispatches, so this is the only thing
// that starts runs. That is the point: a fire the platform acknowledged survives
// a restart and starts when the process comes back, rather than disappearing
// between the acknowledgement and the dispatch.
type Worker struct {
	svc    *Service
	repo   Repository
	logger logging.Logger

	// interval is how often the queue is polled. Short because it is the
	// latency between a fire being accepted and its run starting, and the query
	// is a cheap indexed claim that returns nothing when idle.
	interval time.Duration
	// batch bounds how many fires one tick claims, so a burst is drained
	// steadily rather than in one thundering herd against the agent API.
	batch int

	stopped chan struct{}
}

// NewWorker constructs the pending-delivery worker.
func NewWorker(svc *Service, repo Repository, logger logging.Logger) *Worker {
	return &Worker{
		svc:      svc,
		repo:     repo,
		logger:   logger,
		interval: time.Second,
		batch:    16,
		stopped:  make(chan struct{}),
	}
}

// Start runs the drain loop until ctx is canceled.
func (w *Worker) Start(ctx context.Context) {
	go func() {
		defer close(w.stopped)
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()

		w.logger.Info(ctx, "trigger delivery worker started")
		for {
			select {
			case <-ctx.Done():
				w.logger.Info(ctx, "trigger delivery worker stopped")
				return
			case <-ticker.C:
				w.drain(ctx)
			}
		}
	}()
}

// Wait blocks until the loop has exited, so shutdown does not cut a dispatch in
// half.
func (w *Worker) Wait(ctx context.Context) {
	select {
	case <-w.stopped:
	case <-ctx.Done():
	}
}

// drain claims a batch of waiting fires and dispatches each.
//
// A claim that fails to dispatch is still released: leaving it claimed would
// hold a slot no run occupies, silently throttling a healthy trigger.
func (w *Worker) drain(ctx context.Context) {
	claimed, err := w.repo.ClaimPending(ctx, w.batch)
	if err != nil {
		w.logger.Error(ctx, "failed to claim pending deliveries",
			logging.Field{Key: "error", Value: err.Error()})
		return
	}
	for _, p := range claimed {
		w.svc.DispatchPending(ctx, p)
		if err := w.repo.ReleasePending(ctx, p.ID); err != nil {
			w.logger.Error(ctx, "failed to release claimed delivery",
				logging.Field{Key: "pending_id", Value: p.ID},
				logging.Field{Key: "error", Value: err.Error()})
		}
	}
}

// RetentionJob purges delivery payloads past each tenant's retention window.
//
// It runs on a timer rather than on write because retention is a promise about
// data at rest: it has to hold for payloads already stored when a tenant
// shortens their window, not only for new ones.
type RetentionJob struct {
	repo        Repository
	logger      logging.Logger
	interval    time.Duration
	defaultDays int
}

// NewRetentionJob constructs the payload purge job.
func NewRetentionJob(repo Repository, logger logging.Logger, defaultDays int) *RetentionJob {
	return &RetentionJob{
		repo:        repo,
		logger:      logger,
		interval:    time.Hour,
		defaultDays: defaultDays,
	}
}

// Start runs the purge loop until ctx is canceled. It purges once immediately
// so a shortened retention window takes effect on the next deploy rather than
// after the first interval.
func (j *RetentionJob) Start(ctx context.Context) {
	go func() {
		j.purge(ctx)
		ticker := time.NewTicker(j.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				j.purge(ctx)
			}
		}
	}()
}

func (j *RetentionJob) purge(ctx context.Context) {
	n, err := j.repo.PurgePayloads(ctx, j.defaultDays)
	if err != nil {
		// Loud: a retention promise that has silently stopped running is worse
		// than not having made it.
		j.logger.Error(ctx, "payload retention purge failed",
			logging.Field{Key: "error", Value: err.Error()})
		return
	}
	if n > 0 {
		j.logger.Info(ctx, "purged delivery payloads past retention",
			logging.Field{Key: "rows", Value: n})
	}
}
