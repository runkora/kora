package triggers

import (
	"context"
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
	svc    *Service
	repo   Repository
	logger logging.Logger
	c      *cron.Cron
}

// NewScheduler creates a Scheduler. It does not start immediately; call Start.
func NewScheduler(svc *Service, repo Repository, logger logging.Logger) *Scheduler {
	return &Scheduler{
		svc:    svc,
		repo:   repo,
		logger: logger,
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

	triggers, err := s.repo.ListEnabledScheduledTriggers(ctx)
	if err != nil {
		s.logger.Error(ctx, "scheduler: failed to list scheduled triggers",
			logging.Field{Key: "error", Value: err.Error()},
		)
		return
	}

	for _, t := range triggers {
		if t.ScheduleConfig == nil {
			continue
		}
		if !matchesCron(t.ScheduleConfig, now) {
			continue
		}
		tick := ScheduledTick{Trigger: t, ScheduledAt: now}
		// Dispatch each trigger in its own goroutine so one slow trigger does
		// not delay others. The service's own dispatchTimeout bounds each call.
		go s.svc.TickScheduled(context.WithoutCancel(ctx), tick)
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
