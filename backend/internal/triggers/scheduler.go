package triggers

import (
	"context"
	"fmt"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// scheduleStore is the storage the cron loop touches: the schedules to evaluate,
// and the delivery rows that say which minutes have already fired. Two methods
// off two roles, so it is declared here rather than as a seventh role interface.
type scheduleStore interface {
	ListMatchableScheduledTriggers(ctx context.Context) ([]Trigger, error)
	DueScheduledMinutesWithoutDelivery(ctx context.Context, triggerID string, since time.Time) ([]time.Time, error)
}

// Scheduler evaluates the matchable scheduled triggers each minute and fires
// TickScheduled for each one whose cron expression matches the current time.
//
// It is safe under any number of replicas without a lock. The delivery row for a
// tick is inserted under UNIQUE (trigger_id, scheduled_at) BEFORE anything is
// dispatched, so a replica that loses the insert race stops: a schedule fires at
// most once per minute no matter how many replicas run it, and the same index
// covers a restart within the minute. Leader election would be worse rather than
// redundant here, because a lease handover window can miss a tick entirely
// (docs/adr/2026-06-20-extended-trigger-types.md, Decision 2A).
//
// A tick missed while the platform was down is not lost silently: each pass
// reconciles the last hour, recording a due minute with no delivery as 'missed',
// or firing it when the trigger opts into catch_up.
type Scheduler struct {
	svc     *Service
	repo    scheduleStore
	logger  logging.Logger
	metrics *Metrics // nil records nothing
	c       *cron.Cron
	// sem bounds how many trigger dispatches/reconciles run at once. See spawn.
	sem chan struct{}
}

const (
	// schedulerMaxConcurrent bounds the per-tick dispatch/reconcile goroutines.
	// A tenant with many scheduled triggers — or a database stall that makes
	// each one slow — must not let a tick that fires every minute pile up
	// goroutines, and pooled connections, without limit.
	schedulerMaxConcurrent = 16
	// schedulerOpTimeout bounds one scheduled dispatch or reconcile. Detached
	// from shutdown (an acknowledged tick still enqueues) but not from time: a
	// stalled query gives up and frees its slot rather than holding a goroutine
	// and a connection forever, which is what let a stall compound each minute.
	schedulerOpTimeout = 30 * time.Second
)

// NewScheduler creates a Scheduler. It does not start immediately; call Start.
// metrics may be nil.
func NewScheduler(svc *Service, repo scheduleStore, logger logging.Logger, metrics *Metrics) *Scheduler {
	return &Scheduler{
		svc:     svc,
		repo:    repo,
		logger:  logger,
		metrics: metrics,
		sem:     make(chan struct{}, schedulerMaxConcurrent),
		// SecondOptional adds an optional leading seconds field; without it,
		// standard 5-field cron is parsed correctly. SkipIfStillRunning drops a
		// tick that fires while the previous one is still draining rather than
		// overlapping them, so a slow pass cannot stack tick goroutines on top
		// of the work goroutines spawn already bounds.
		c: cron.New(
			cron.WithLocation(time.UTC),
			cron.WithChain(cron.SkipIfStillRunning(cron.DiscardLogger)),
		),
	}
}

// spawn runs fn in a bounded goroutine on a context detached from shutdown
// cancellation but capped at schedulerOpTimeout.
//
// The bound is the whole point: without it, a tick that fires every minute
// spawns one goroutine per scheduled trigger, and under a database stall each
// blocks on its query forever while the next minute spawns another full batch —
// an unbounded pile of goroutines and pooled connections that also outlives
// shutdown. The semaphore caps concurrency; the timeout frees a slot a stalled
// query would otherwise hold indefinitely; the detach keeps an acknowledged
// tick enqueuing across shutdown, now with a ceiling on how long it may take.
//
// Returns without spawning if ctx is already canceled or is canceled while
// waiting for a slot, so shutdown does not block on a wedged tick.
func (s *Scheduler) spawn(ctx context.Context, fn func(context.Context)) {
	if ctx.Err() != nil {
		return
	}
	select {
	case s.sem <- struct{}{}:
	case <-ctx.Done():
		return
	}
	go func() {
		defer func() { <-s.sem }()
		opCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), schedulerOpTimeout)
		defer cancel()
		fn(opCtx)
	}()
}

// Start registers a job that fires every minute, evaluates scheduled triggers,
// and dispatches matching ones. It is non-blocking — the cron loop runs in its
// own goroutine.
func (s *Scheduler) Start(ctx context.Context) error {
	_, err := s.c.AddFunc("* * * * *", func() {
		s.tick(ctx)
	})
	if err != nil {
		return err
	}
	s.c.Start()
	s.logger.Info(ctx, "trigger scheduler started")
	return nil
}

// Stop gracefully shuts down the cron runner, waiting for any in-flight tick to
// complete.
func (s *Scheduler) Stop(ctx context.Context) {
	stopCtx := s.c.Stop()
	select {
	case <-stopCtx.Done():
	case <-ctx.Done():
	}
	s.logger.Info(ctx, "trigger scheduler stopped")
}

// tick evaluates all enabled scheduled triggers and fires any whose cron
// expression matches now (in the trigger's configured timezone).
func (s *Scheduler) tick(ctx context.Context) {
	now := time.Now().UTC()
	// A tick that starts taking longer than the minute between ticks has
	// quietly stopped being a minutely schedule, and nothing else in the system
	// would say so. Deferred so a tick that returns early on error is still
	// timed: those are the slow ones worth seeing.
	scanned := 0
	defer func(started time.Time) {
		s.metrics.recordSchedulerTick(ctx, scanned, time.Since(started))
	}(time.Now())

	// Auto-paused schedules are evaluated too. Their tick reaches admission,
	// which records 'paused' rather than starting a run, so a paused schedule
	// shows the fires it refused instead of going silent.
	triggers, err := s.repo.ListMatchableScheduledTriggers(ctx)
	if err != nil {
		s.logger.Error(ctx, "scheduler: failed to list scheduled triggers",
			logging.Field{Key: "error", Value: err.Error()},
		)
		return
	}
	scanned = len(triggers)

	for _, t := range triggers {
		if t.ScheduleConfig == nil {
			continue
		}
		sched, loc, err := ParseSchedule(t.ScheduleConfig)
		if err != nil {
			// A stored expression that no longer parses would otherwise never
			// fire and never say so. Parsing once per pass keeps the log to one
			// line per trigger per minute rather than one per due minute.
			s.logger.Error(ctx, "scheduler: stored schedule does not parse; trigger cannot fire",
				logging.Field{Key: "trigger_id", Value: t.ID},
				logging.Field{Key: "error", Value: err.Error()},
			)
			continue
		}
		if firesAt(sched, loc, now) {
			tick := ScheduledTick{Trigger: t, ScheduledAt: now}
			// Dispatch each trigger in its own bounded goroutine so one slow
			// trigger does not delay others. spawn detaches it from shutdown so
			// an acknowledged fire still enqueues, while bounding concurrency
			// and time.
			s.spawn(ctx, func(opCtx context.Context) { s.svc.TickScheduled(opCtx, tick) })
			continue
		}
		// Not due now, but a fire may have been missed while the platform was
		// down. Reconcile the recent past so a missed fire leaves a record
		// instead of vanishing.
		s.spawn(ctx, func(opCtx context.Context) { s.reconcileMissed(opCtx, t, sched, loc, now) })
	}
}

// backfillWindow bounds how far back a tick looks for fires it missed.
//
// One hour is a deliberate ceiling on the write burst after an outage: a
// minutely schedule that was down for a day would otherwise produce 1440 rows
// the moment it recovers, which is noise rather than information.
const backfillWindow = time.Hour

// reconcileMissed records fires that were due while the platform was down.
//
// Default behavior is to record and skip (PD1): an agent doing real work twice
// is worse than not doing it at all, and a long outage on a frequent schedule
// would otherwise dump a burst of runs at once. A trigger whose work genuinely
// must not be skipped opts in with catch_up, and then the missed minutes fire
// for real.
func (s *Scheduler) reconcileMissed(ctx context.Context, t Trigger, sched cron.Schedule, loc *time.Location, now time.Time) {
	due, err := s.repo.DueScheduledMinutesWithoutDelivery(ctx, t.ID, now.Add(-backfillWindow))
	if err != nil {
		s.logger.Error(ctx, "scheduler: failed to look for missed fires",
			logging.Field{Key: "trigger_id", Value: t.ID},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return
	}
	for _, minute := range due {
		if !firesAt(sched, loc, minute) {
			continue
		}
		if t.ScheduleConfig.CatchUp {
			s.svc.TickScheduled(ctx, ScheduledTick{Trigger: t, ScheduledAt: minute})
			continue
		}
		s.svc.RecordMissedTick(ctx, t, minute)
	}
}

// firesAt reports whether an already-parsed schedule fires at the given time,
// to minute precision, in the schedule's own timezone.
func firesAt(sched cron.Schedule, loc *time.Location, at time.Time) bool {
	// cron.Schedule.Next returns the next time AFTER t. If Next(now-1min) == now
	// (truncated to minute), the cron fires at this minute.
	minute := at.In(loc).Truncate(time.Minute)
	return sched.Next(minute.Add(-time.Second)).Truncate(time.Minute).Equal(minute)
}

// ParseSchedule parses a schedule's cron expression in its own timezone.
//
// Standard 5-field cron is the deliberate limit: its finest granularity is one
// minute, so the "no faster than a minute" rule is enforced by the grammar
// rather than by a check that could be forgotten.
func ParseSchedule(sc *ScheduleConfig) (cron.Schedule, *time.Location, error) {
	loc, err := time.LoadLocation(sc.Timezone)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: timezone %q is not a valid IANA timezone", ErrValidation, sc.Timezone)
	}
	p := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	sched, err := p.Parse(sc.CronExpression)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: cron expression %q is not valid 5-field cron: %w",
			ErrValidation, sc.CronExpression, err)
	}
	return sched, loc, nil
}

// NextFires returns the next n fire times for a schedule, in its own timezone.
//
// The creation wizard shows these before saving: a cron expression is easy to
// get subtly wrong, and seeing "next: Sunday 02:00" catches a mistake that would
// otherwise surface as a job that quietly never runs.
func NextFires(sc *ScheduleConfig, from time.Time, n int) ([]time.Time, error) {
	sched, loc, err := ParseSchedule(sc)
	if err != nil {
		return nil, err
	}
	out := make([]time.Time, 0, n)
	t := from.In(loc)
	for range n {
		t = sched.Next(t)
		if t.IsZero() {
			break
		}
		out = append(out, t)
	}
	return out, nil
}

// schedulePreviewCount is how many upcoming fires to show. Three is enough to
// reveal the shape of a schedule (daily, weekdays, monthly) without becoming a
// wall of timestamps.
const schedulePreviewCount = 3

// SchedulePreview answers "what will this actually do?" before a schedule is
// saved, from the same NextFires the scheduler itself runs, so what the console
// shows and what fires cannot drift. Lives beside NextFires rather than in the
// transport layer, like every other endpoint's logic.
func (s *Service) SchedulePreview(req SchedulePreviewRequest) (*SchedulePreviewResponse, error) {
	if req.ScheduleConfig == nil {
		return nil, fmt.Errorf("%w: schedule_config is required", ErrValidation)
	}
	fires, err := NextFires(req.ScheduleConfig, time.Now(), schedulePreviewCount)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	out := make([]SchedulePreviewFire, 0, len(fires))
	for _, f := range fires {
		out = append(out, SchedulePreviewFire{At: f, Label: f.Format("Mon 2 Jan 2006, 15:04")})
	}
	return &SchedulePreviewResponse{NextFires: out, Timezone: req.ScheduleConfig.Timezone}, nil
}
