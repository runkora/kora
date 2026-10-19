package triggers

import (
	"context"
	"time"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// Worker drains the pending-delivery queue.
//
// Every fire is durably queued before it dispatches, so this is the only thing
// that starts runs. That is the point: a fire the platform acknowledged survives
// a restart and starts when the process comes back, rather than disappearing
// between the acknowledgement and the dispatch.
type Worker struct {
	svc    *Service
	repo   PendingQueue
	logger logging.Logger

	// interval is how often the queue is polled. Short because it is the
	// latency between a fire being accepted and its run starting, and the query
	// is a cheap indexed claim that returns nothing when idle.
	interval time.Duration
	// batch bounds how many fires one tick claims, so a burst is drained
	// steadily rather than in one thundering herd against the agent API.
	batch int
	// sweepEvery is how many ticks pass between housekeeping sweeps (reclaiming
	// stranded claims, collecting orphaned fires). Both scan the whole queue
	// table, and neither is urgent: the stale-claim window is fifteen minutes,
	// and an orphaned fire is already never going to run. Running them every
	// second cost two table scans a second to fix conditions measured in
	// minutes.
	sweepEvery int
	// ticks counts drains since start, so the sweep can run on its own cadence.
	// Only the single drain goroutine touches it.
	ticks int

	stopped chan struct{}
}

// NewWorker constructs the pending-delivery worker.
func NewWorker(svc *Service, repo PendingQueue, logger logging.Logger) *Worker {
	return &Worker{
		svc:        svc,
		repo:       repo,
		logger:     logger,
		interval:   time.Second,
		batch:      16,
		sweepEvery: 30,
		stopped:    make(chan struct{}),
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

// Wait blocks until the drain loop has exited or the grace context expires. A
// dispatch already in flight when shutdown was signaled runs to completion on
// its own detached context (see drain), so the loop returns without cutting it;
// rows not yet started keep their claim for the next tick.
func (w *Worker) Wait(ctx context.Context) {
	select {
	case <-w.stopped:
	case <-ctx.Done():
	}
}

// sweep is the queue's housekeeping: take back fires stranded by a dead worker,
// and collect fires whose trigger can no longer run them.
//
// Both are repairs of conditions measured in minutes, so they run on their own
// slower cadence rather than on every tick. It runs before claiming so anything
// reclaimed can be picked up in the same tick.
func (w *Worker) sweep(ctx context.Context) {
	w.reclaimStale(ctx)
	w.collectOrphaned(ctx)
}

// collectOrphaned drops fires waiting on a trigger that was deleted or switched
// off, recording each so the work is visibly accounted for rather than
// disappearing.
//
// The lifecycle paths drain their own backlog; this catches the fire admitted in
// the instant between a disable committing and that drain running. Nothing else
// ever would: claiming requires an enabled trigger, and the stale-claim reaper
// only touches rows a worker already holds, so such a fire waits forever.
func (w *Worker) collectOrphaned(ctx context.Context) {
	orphans, err := w.repo.DrainOrphanedPending(ctx, w.batch)
	if err != nil {
		w.logger.Error(ctx, "failed to collect orphaned pending deliveries",
			logging.Field{Key: "error", Value: err.Error()})
		return
	}
	for _, o := range orphans {
		w.svc.RecordOrphanedPending(ctx, o)
	}
	if len(orphans) > 0 {
		w.logger.Info(ctx, "dropped fires whose trigger can no longer run them",
			logging.Field{Key: "count", Value: len(orphans)})
	}
}

// reclaimStale takes back fires whose worker died mid-dispatch.
//
// Each one is logged individually and at warning level: a reclaim means a pod
// died holding acknowledged work, which is worth knowing about even though the
// queue recovers by itself.
func (w *Worker) reclaimStale(ctx context.Context) {
	reclaimed, err := w.repo.ReclaimStalePending(ctx, StaleClaimWindow)
	if err != nil {
		w.logger.Error(ctx, "failed to reclaim stale pending deliveries",
			logging.Field{Key: "error", Value: err.Error()})
		return
	}
	for _, id := range reclaimed {
		w.logger.Warn(ctx, "reclaimed a fire whose worker never dispatched it",
			logging.Field{Key: "pending_id", Value: id},
			logging.Field{Key: "stale_after", Value: StaleClaimWindow.String()})
	}
}

// drain sweeps the queue on its slower cadence, then claims a batch of waiting
// fires and dispatches each.
//
// A claim that fails to dispatch is still released: leaving it claimed would
// hold a slot no run occupies, silently throttling a healthy trigger.
func (w *Worker) drain(ctx context.Context) {
	// Sweep on the first tick, then every sweepEvery ticks. The first one
	// matters: a pod restarting is exactly when there are stranded claims to
	// take back, and waiting half a minute to look would be waiting for no
	// reason.
	w.ticks++
	if w.sweepEvery <= 1 || w.ticks%w.sweepEvery == 1 {
		w.sweep(ctx)
	}

	claimed, err := w.repo.ClaimPending(ctx, w.batch)
	if err != nil {
		w.logger.Error(ctx, "failed to claim pending deliveries",
			logging.Field{Key: "error", Value: err.Error()})
		return
	}
	for i := range claimed {
		p := claimed[i]
		// Once shutdown is signaled, stop taking new rows from the batch. The
		// untouched ones keep their claim and the next tick — here or on
		// another replica — picks them up. Only the row already in flight below
		// must be allowed to finish.
		if ctx.Err() != nil {
			return
		}
		// A dispatch that has begun runs to completion on a context detached
		// from shutdown cancellation, the same way ingest detaches its fan-out.
		// Cutting it after StartRun had landed would leave the outcome unwritten
		// and the row still claimed, and a later reaper would start the same
		// fire a second time. DispatchPending bounds its own StartRun, so this
		// cannot hang indefinitely.
		dctx := context.WithoutCancel(ctx)
		// Re-stamp this row AND the rows still waiting behind it. The batch was
		// claimed in one statement and dispatched serially, so a row waiting
		// behind slow dispatches would otherwise carry a claim stamped before
		// the first dispatch even began — old enough for another replica's
		// reaper to reclaim it and start the same fire a second time. One
		// dispatch is bounded below the stale-claim window, so re-stamping the
		// remaining batch before each dispatch holds all of it inside the window
		// while this worker lives.
		remaining := make([]string, len(claimed)-i)
		for j := i; j < len(claimed); j++ {
			remaining[j-i] = claimed[j].ID
		}
		if err := w.repo.TouchPendingClaims(dctx, remaining); err != nil {
			w.logger.Error(dctx, "failed to re-stamp claims before dispatch",
				logging.Field{Key: "pending_id", Value: p.ID},
				logging.Field{Key: "error", Value: err.Error()})
		}
		w.svc.DispatchPending(dctx, p)
		if err := w.repo.ReleasePending(dctx, p.ID); err != nil {
			w.logger.Error(dctx, "failed to release claimed delivery",
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
	repo        RetentionStore
	logger      logging.Logger
	metrics     *Metrics // nil records nothing
	interval    time.Duration
	defaultDays int
}

// NewRetentionJob constructs the payload purge job. metrics may be nil.
func NewRetentionJob(repo RetentionStore, logger logging.Logger, defaultDays int, metrics *Metrics) *RetentionJob {
	return &RetentionJob{
		repo:        repo,
		logger:      logger,
		metrics:     metrics,
		interval:    time.Hour,
		defaultDays: defaultDays,
	}
}

// Start runs the purge loop until ctx is canceled. It purges once immediately
// so a shortened retention window takes effect on the next deploy rather than
// after the first interval.
//
// Unlike Worker, this deliberately has no Wait: there is nothing to wait for.
// A purge is a pair of DELETEs with no acknowledged work behind them, so a
// shutdown that cuts one short loses nothing — the rows it did not reach are
// still past their window on the next pass, an hour or a restart later.
// Blocking shutdown on an hourly cleanup would trade real availability for a
// tidiness no caller can observe.
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
	// The replay window is a window, not a ledger. Pruned here rather than on a
	// job of its own: it is the same kind of promise (data the platform holds
	// about inbound deliveries has a lifetime), on the same cadence, and a
	// second timer for one DELETE would be ceremony.
	if _, err := j.repo.PruneReplayKeys(ctx, replayWindow); err != nil {
		j.logger.Error(ctx, "replay-key prune failed",
			logging.Field{Key: "error", Value: err.Error()})
	}

	n, err := j.repo.PurgePayloads(ctx, j.defaultDays)
	if err != nil {
		// Loud: a retention promise that has silently stopped running is worse
		// than not having made it.
		j.logger.Error(ctx, "payload retention purge failed",
			logging.Field{Key: "error", Value: err.Error()})
		return
	}
	// Record the success, not only the rows. A rows counter sits at zero both
	// when there is nothing to purge and when the job has died; only the
	// timestamp tells those apart, and it is the one an alert should watch.
	j.metrics.recordPurge(ctx, n, time.Now())
	if n > 0 {
		j.logger.Info(ctx, "purged delivery payloads past retention",
			logging.Field{Key: "rows", Value: n})
	}
}
