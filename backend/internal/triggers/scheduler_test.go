package triggers

import (
	"context"
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
