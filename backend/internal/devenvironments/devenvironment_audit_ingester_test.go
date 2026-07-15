package devenvironments

import (
	"context"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/orbitapi"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
)

func newTestEnvForIngester(name, phase string, awake bool) *orbitapi.OrbitDevEnvironment {
	return &orbitapi.OrbitDevEnvironment{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       orbitapi.OrbitDevEnvironmentSpec{Owner: "dev@example.com", Awake: awake},
		Status:     orbitapi.OrbitDevEnvironmentStatus{Phase: phase},
	}
}

// TestAuditIngester_AddedOnlyPrimesNeverEmits guards the core idempotency
// design: an ADDED event (what every ingester restart redelivers as the
// current state of each existing environment) must never emit an audit row
// by itself — only prime the in-memory last-observed-phase map.
func TestAuditIngester_AddedOnlyPrimesNeverEmits(t *testing.T) {
	repo := newFakeRepository()
	ing := NewAuditIngester(nil, repo, logging.NewDevelopmentLogger())

	ing.handleEvent(context.Background(), watch.Event{
		Type:   watch.Added,
		Object: newTestEnvForIngester("alice", orbitapi.DevEnvPhaseReady, true),
	})

	events, err := repo.ListAuditEvents(context.Background(), "alice")
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("audit events after ADDED = %+v, want none", events)
	}

	ing.mu.Lock()
	got, ok := ing.lastPhase["alice"]
	ing.mu.Unlock()
	if !ok || got != orbitapi.DevEnvPhaseReady {
		t.Errorf("lastPhase[alice] = %q, ok=%v, want %q, true", got, ok, orbitapi.DevEnvPhaseReady)
	}
}

// TestAuditIngester_ModifiedTransitionToReady guards the common derivation:
// a genuine (in-memory-observed) transition to Ready emits provision_ready.
func TestAuditIngester_ModifiedTransitionToReady(t *testing.T) {
	repo := newFakeRepository()
	ing := NewAuditIngester(nil, repo, logging.NewDevelopmentLogger())
	ctx := context.Background()

	ing.handleEvent(ctx, watch.Event{Type: watch.Added, Object: newTestEnvForIngester("bob", orbitapi.DevEnvPhaseProvisioning, true)})
	ing.handleEvent(ctx, watch.Event{Type: watch.Modified, Object: newTestEnvForIngester("bob", orbitapi.DevEnvPhaseReady, true)})

	events, err := repo.ListAuditEvents(ctx, "bob")
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(events) != 1 || events[0].Action != "provision_ready" {
		t.Fatalf("audit events = %+v, want exactly one provision_ready", events)
	}
	if events[0].ActorType != "system" || events[0].Actor != "system:operator" {
		t.Errorf("event = %+v, want actorType=system actor=system:operator", events[0])
	}
}

// TestAuditIngester_ModifiedTransitionToFailed guards the provision_failed derivation.
func TestAuditIngester_ModifiedTransitionToFailed(t *testing.T) {
	repo := newFakeRepository()
	ing := NewAuditIngester(nil, repo, logging.NewDevelopmentLogger())
	ctx := context.Background()

	ing.handleEvent(ctx, watch.Event{Type: watch.Added, Object: newTestEnvForIngester("carol", orbitapi.DevEnvPhaseProvisioning, true)})
	ing.handleEvent(ctx, watch.Event{Type: watch.Modified, Object: newTestEnvForIngester("carol", orbitapi.DevEnvPhaseFailed, true)})

	events, err := repo.ListAuditEvents(ctx, "carol")
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(events) != 1 || events[0].Action != "provision_failed" {
		t.Fatalf("audit events = %+v, want exactly one provision_failed", events)
	}
}

// TestAuditIngester_ModifiedTransitionToSleepingWhenNotAwake guards the
// auto_slept derivation: a genuine transition into Sleeping with
// spec.awake=false (the only way the real reconciler's enterSleep ever
// persists a Sleeping phase — phase and spec.awake=false always land in the
// same status write) emits exactly one auto_slept.
func TestAuditIngester_ModifiedTransitionToSleepingWhenNotAwake(t *testing.T) {
	repo := newFakeRepository()
	ing := NewAuditIngester(nil, repo, logging.NewDevelopmentLogger())
	ctx := context.Background()

	ing.handleEvent(ctx, watch.Event{Type: watch.Added, Object: newTestEnvForIngester("dave", orbitapi.DevEnvPhaseReady, true)})
	ing.handleEvent(ctx, watch.Event{Type: watch.Modified, Object: newTestEnvForIngester("dave", orbitapi.DevEnvPhaseSleeping, false)})

	events, err := repo.ListAuditEvents(ctx, "dave")
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(events) != 1 || events[0].Action != "auto_slept" {
		t.Fatalf("audit events = %+v, want exactly one auto_slept", events)
	}
}

// TestAuditIngester_SleepingObservedWithAwakeTrueIsIgnored guards the
// defensive belt-and-suspenders check in recordPhaseTransition: even though
// the real reconciler never persists Sleeping with spec.awake=true (phase
// and awake=false always land in the same status write), a Sleeping
// observation that DOES carry awake=true (e.g. a watch redelivering a
// stale/racing object) must not be mistaken for the idle auto-sleep this
// action means.
func TestAuditIngester_SleepingObservedWithAwakeTrueIsIgnored(t *testing.T) {
	repo := newFakeRepository()
	ing := NewAuditIngester(nil, repo, logging.NewDevelopmentLogger())
	ctx := context.Background()

	ing.handleEvent(ctx, watch.Event{Type: watch.Added, Object: newTestEnvForIngester("dave2", orbitapi.DevEnvPhaseReady, true)})
	ing.handleEvent(ctx, watch.Event{Type: watch.Modified, Object: newTestEnvForIngester("dave2", orbitapi.DevEnvPhaseSleeping, true)})

	events, err := repo.ListAuditEvents(ctx, "dave2")
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("audit events for Sleeping+awake=true = %+v, want none (not idle auto-sleep)", events)
	}
}

// TestAuditIngester_UnchangedPhaseEmitsNothing guards against redelivery
// noise: a MODIFIED event carrying the SAME phase as last observed must not
// emit anything.
func TestAuditIngester_UnchangedPhaseEmitsNothing(t *testing.T) {
	repo := newFakeRepository()
	ing := NewAuditIngester(nil, repo, logging.NewDevelopmentLogger())
	ctx := context.Background()

	ing.handleEvent(ctx, watch.Event{Type: watch.Added, Object: newTestEnvForIngester("erin", orbitapi.DevEnvPhaseReady, true)})
	ing.handleEvent(ctx, watch.Event{Type: watch.Modified, Object: newTestEnvForIngester("erin", orbitapi.DevEnvPhaseReady, true)})

	events, err := repo.ListAuditEvents(ctx, "erin")
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("audit events for a redelivered identical phase = %+v, want none", events)
	}
}

// TestAuditIngester_DeletedRecordsExpiredWhenNotTerminal guards the CR
// deletion -> expired derivation, and that it is skipped once a terminal
// (destroyed/force_destroyed) event already accounts for the disappearance.
func TestAuditIngester_DeletedRecordsExpiredWhenNotTerminal(t *testing.T) {
	repo := newFakeRepository()
	ing := NewAuditIngester(nil, repo, logging.NewDevelopmentLogger())
	ctx := context.Background()

	ing.handleEvent(ctx, watch.Event{Type: watch.Added, Object: newTestEnvForIngester("frank", orbitapi.DevEnvPhaseReady, true)})
	ing.handleEvent(ctx, watch.Event{Type: watch.Deleted, Object: newTestEnvForIngester("frank", orbitapi.DevEnvPhaseReady, true)})

	events, err := repo.ListAuditEvents(ctx, "frank")
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(events) != 1 || events[0].Action != "expired" {
		t.Fatalf("audit events = %+v, want exactly one expired", events)
	}

	ing.mu.Lock()
	_, stillTracked := ing.lastPhase["frank"]
	ing.mu.Unlock()
	if stillTracked {
		t.Error("lastPhase still tracks a deleted environment")
	}
}

func TestAuditIngester_DeletedSkipsExpiredWhenAlreadyDestroyed(t *testing.T) {
	repo := newFakeRepository()
	ing := NewAuditIngester(nil, repo, logging.NewDevelopmentLogger())
	ctx := context.Background()

	if err := repo.InsertAuditEvent(ctx, AuditEvent{
		EnvironmentID: "grace", Action: "destroyed", Actor: "dev@example.com", ActorType: "user",
		OccurredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed destroyed event: %v", err)
	}

	ing.handleEvent(ctx, watch.Event{Type: watch.Added, Object: newTestEnvForIngester("grace", orbitapi.DevEnvPhaseReady, true)})
	ing.handleEvent(ctx, watch.Event{Type: watch.Deleted, Object: newTestEnvForIngester("grace", orbitapi.DevEnvPhaseReady, true)})

	events, err := repo.ListAuditEvents(ctx, "grace")
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(events) != 1 || events[0].Action != "destroyed" {
		t.Fatalf("audit events = %+v, want only the seeded destroyed event (no expired)", events)
	}
}
