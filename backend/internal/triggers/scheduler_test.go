package triggers

import (
	"context"
	"net/http"
	"slices"
	"testing"
	"time"
)

// spawn bounds concurrency: with a single slot, a second dispatch cannot start
// until the first releases. This is the cap that stops a per-minute tick from
// piling goroutines up under a database stall.
func TestScheduler_SpawnBoundsConcurrentWork(t *testing.T) {
	s := &Scheduler{sem: make(chan struct{}, 1)}

	firstRunning := make(chan struct{})
	release := make(chan struct{})
	s.spawn(context.Background(), func(context.Context) {
		close(firstRunning)
		<-release
	})
	<-firstRunning // the only slot is now held

	secondRan := make(chan struct{})
	go s.spawn(context.Background(), func(context.Context) { close(secondRan) })

	select {
	case <-secondRan:
		t.Fatal("a second op ran while the only slot was held; concurrency is not bounded")
	case <-time.After(50 * time.Millisecond):
		// Good: blocked on the semaphore.
	}

	close(release) // free the slot
	select {
	case <-secondRan:
	case <-time.After(2 * time.Second):
		t.Fatal("the second op did not run after the slot freed")
	}
}

// A canceled context skips the spawn entirely, so shutdown does not start new
// scheduled work.
func TestScheduler_SpawnSkipsWhenCanceled(t *testing.T) {
	s := &Scheduler{sem: make(chan struct{}, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ran := make(chan struct{})
	s.spawn(ctx, func(context.Context) { close(ran) })

	select {
	case <-ran:
		t.Error("spawn ran fn despite an already-canceled context")
	case <-time.After(30 * time.Millisecond):
		// Good: skipped.
	}
}

// The spawned op runs on a context with a deadline, so a stalled query gives up
// and frees its slot rather than blocking a goroutine forever.
func TestScheduler_SpawnAppliesTimeout(t *testing.T) {
	s := &Scheduler{sem: make(chan struct{}, 1)}

	gotDeadline := make(chan bool, 1)
	s.spawn(context.Background(), func(opCtx context.Context) {
		_, ok := opCtx.Deadline()
		gotDeadline <- ok
	})

	select {
	case ok := <-gotDeadline:
		if !ok {
			t.Error("a scheduled op ran without a deadline; a stalled query could hold its slot forever")
		}
	case <-time.After(time.Second):
		t.Fatal("the op did not run")
	}
}

// The scheduler wakes a trigger's agent once per fire when the fire is within the lead, and retries a
// failed pre-wake on the next tick.
func TestScheduler_PreWake(t *testing.T) {
	svc, deps := newTestService(t)
	s := NewScheduler(svc, nil, silentLogger(), nil)
	s.SetPreWakeLead(3 * time.Minute)
	trig := Trigger{ID: "trig-1", TenantID: testTenant, Kind: KindScheduled, Enabled: true, AgentTypeID: "at-1",
		PrincipalID: "trigger:trig-1", ScheduleConfig: &ScheduleConfig{CronExpression: "*/10 * * * *", Timezone: "UTC"}}
	sched, loc, err := ParseSchedule(trig.ScheduleConfig)
	if err != nil {
		t.Fatal(err)
	}
	preWakes := func() []string {
		deps.agents.mu.Lock()
		defer deps.agents.mu.Unlock()
		return slices.Clone(deps.agents.preWakes)
	}
	waitFor := func(n int) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for len(preWakes()) < n && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if got := preWakes(); len(got) != n {
			t.Fatalf("pre-wakes = %v, want %d", got, n)
		}
	}
	at := func(clock string) time.Time {
		v, err := time.Parse(time.RFC3339, "2026-11-23T"+clock+"Z")
		if err != nil {
			t.Fatal(err)
		}
		return v
	}

	s.preWake(t.Context(), trig, sched, loc, at("12:01:00"))
	disabled := trig
	disabled.Enabled = false
	s.preWake(t.Context(), disabled, sched, loc, at("12:08:00"))
	routed := trig
	routed.AgentTypeID = ""
	s.preWake(t.Context(), routed, sched, loc, at("12:08:00"))
	s.preWake(t.Context(), trig, sched, loc, at("12:08:00"))
	waitFor(1)
	s.preWake(t.Context(), trig, sched, loc, at("12:09:00"))
	time.Sleep(50 * time.Millisecond)
	if got := preWakes(); len(got) != 1 || got[0] != "at-1 2026-11-23T12:10:00Z" {
		t.Fatalf("pre-wakes = %v, want one for the 12:10 fire", got)
	}

	deps.agents.mu.Lock()
	deps.agents.preWakeStatus = http.StatusServiceUnavailable
	deps.agents.mu.Unlock()
	s.preWake(t.Context(), trig, sched, loc, at("12:18:00"))
	waitFor(2)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s.preWokenMu.Lock()
		_, held := s.preWoken[trig.ID]
		s.preWokenMu.Unlock()
		if !held {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	deps.agents.mu.Lock()
	deps.agents.preWakeStatus = 0
	deps.agents.mu.Unlock()
	s.preWake(t.Context(), trig, sched, loc, at("12:19:00"))
	waitFor(3)
}
