package triggers

import (
	"context"
	"fmt"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// Scheduler evaluates all enabled scheduled triggers each minute and fires
// TickScheduled for each one whose cron expression matches the current time.
// It is single-instance best-effort: if the process is down during a tick, that
// tick is skipped (no backfill). The dedup index in the DB prevents double-fire
// on pod restart within the same minute window.
//
// When orbit-triggers runs with >1 replica, wrap Start/Stop with a distributed
// lock (Kubernetes Lease or DB advisory lock) before starting the scheduler.
type Scheduler struct {
	svc     *Service
	repo    Repository
	logger  logging.Logger
	metrics *Metrics // nil records nothing
	c       *cron.Cron
}

// NewScheduler creates a Scheduler. It does not start immediately; call Start.
// metrics may be nil.
func NewScheduler(svc *Service, repo Repository, logger logging.Logger, metrics *Metrics) *Scheduler {
	return &Scheduler{
		svc:     svc,
		repo:    repo,
		logger:  logger,
		metrics: metrics,
		// SecondOptional adds an optional leading seconds field; without it,
		// standard 5-field cron is parsed correctly.
		c: cron.New(cron.WithLocation(time.UTC)),
	}
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
		if matchesCron(t.ScheduleConfig, now) {
			tick := ScheduledTick{Trigger: t, ScheduledAt: now}
			// Dispatch each trigger in its own goroutine so one slow trigger
			// does not delay others. The service's dispatchTimeout bounds each.
			go s.svc.TickScheduled(context.WithoutCancel(ctx), tick)
			continue
		}
		// Not due now, but a fire may have been missed while the platform was
		// down. Reconcile the recent past so a missed fire leaves a record
		// instead of vanishing.
		go s.reconcileMissed(context.WithoutCancel(ctx), t, now)
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
func (s *Scheduler) reconcileMissed(ctx context.Context, t Trigger, now time.Time) {
	due, err := s.repo.DueScheduledMinutesWithoutDelivery(ctx, t.ID, now.Add(-backfillWindow))
	if err != nil {
		s.logger.Error(ctx, "scheduler: failed to look for missed fires",
			logging.Field{Key: "trigger_id", Value: t.ID},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return
	}
	for _, minute := range due {
		if !matchesCron(t.ScheduleConfig, minute) {
			continue
		}
		if t.ScheduleConfig.CatchUp {
			s.svc.TickScheduled(ctx, ScheduledTick{Trigger: t, ScheduledAt: minute})
			continue
		}
		s.svc.RecordMissedTick(ctx, t, minute)
	}
}

// matchesCron reports whether the given cron schedule matches the given time
// (to minute precision). It parses the expression in the trigger's configured
// timezone.
func matchesCron(sc *ScheduleConfig, now time.Time) bool {
	loc, err := time.LoadLocation(sc.Timezone)
	if err != nil {
		return false
	}
	p := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	sched, err := p.Parse(sc.CronExpression)
	if err != nil {
		return false
	}
	// cron.Schedule.Next returns the next time AFTER t. If Next(now-1min) == now
	// (truncated to minute), the cron fires at this minute.
	prevMinute := now.In(loc).Truncate(time.Minute).Add(-time.Second)
	next := sched.Next(prevMinute)
	return next.Truncate(time.Minute).Equal(now.In(loc).Truncate(time.Minute))
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
