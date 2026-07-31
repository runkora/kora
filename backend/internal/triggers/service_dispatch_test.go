package triggers

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	agentsclient "github.com/clear-route/orbit/internal/platform/clients/agents"
)

// errTriggerSideFailure stands for a dispatch failure that says something about
// the trigger rather than about the platform, which is what auto-pause counts.
// A revoked grant is the archetype: it denies this trigger and no other, past
// the propagation retry, and retrying it is futile. Platform faults are covered
// separately by TestAutoPause_IgnoresPlatformFaults.
var errTriggerSideFailure = fmt.Errorf("start run: %w", agentsclient.ErrForbidden)

// The ADR's headline example: one at a time, replace what is waiting, three
// overlapping fires. Exactly one further run, and the displaced fires are
// visibly superseded rather than silently gone.
func TestAdmission_Replace_SupersedesWaitingFires(t *testing.T) {
	svc, deps := newTestService(t)
	trig := newAdmissionTrigger(t, deps, OverflowReplace, 1, 32)

	ids := enqueue(t, svc, deps, trig, 3)

	// Only the newest fire is still waiting.
	if pending := deps.repo.pendingFor(trig.ID); len(pending) != 1 {
		t.Fatalf("want 1 waiting fire, got %d", len(pending))
	}
	if pending := deps.repo.pendingFor(trig.ID); pending[0].DeliveryID != ids[2] {
		t.Errorf("waiting fire = %s, want the newest (%s)", pending[0].DeliveryID, ids[2])
	}

	// The two it displaced are recorded, not lost.
	for _, id := range ids[:2] {
		runs := deps.repo.deliveryRunsFor(id)
		if len(runs) != 1 || runs[0].Status != RunStatusSuperseded {
			t.Errorf("delivery %s outcomes = %+v, want one superseded", id, runs)
		}
	}
}

// Refuse rejects at the ceiling rather than queueing, so a caller waiting on the
// answer gets one immediately.
func TestAdmission_Refuse_DropsAtCeiling(t *testing.T) {
	svc, deps := newTestService(t)
	trig := newAdmissionTrigger(t, deps, OverflowRefuse, 1, 32)

	// The store is at the ceiling and turns the fire away. Whether it refuses at
	// the right moment is the statement's business, covered against Postgres;
	// what matters here is what the service does with the refusal.
	deps.repo.refuseAdmission = true

	ids := enqueue(t, svc, deps, trig, 1)

	if pending := deps.repo.pendingFor(trig.ID); len(pending) != 0 {
		t.Errorf("refuse must not queue, got %d waiting", len(pending))
	}
	runs := deps.repo.deliveryRunsFor(ids[0])
	if len(runs) != 1 || runs[0].Status != RunStatusDropped {
		t.Fatalf("outcomes = %+v, want one dropped", runs)
	}
}

// Wait bounds its backlog, and evicts the oldest: when a trigger is behind, the
// freshest events are the ones still worth acting on.
func TestAdmission_Wait_EvictsOldestBeyondDepth(t *testing.T) {
	svc, deps := newTestService(t)
	trig := newAdmissionTrigger(t, deps, OverflowWait, 1, 2)

	ids := enqueue(t, svc, deps, trig, 4)

	pending := deps.repo.pendingFor(trig.ID)
	if len(pending) > 3 {
		t.Fatalf("backlog = %d, want it bounded near max_queue_depth", len(pending))
	}

	// The oldest were evicted and recorded as dropped.
	dropped := 0
	for _, id := range ids {
		for _, r := range deps.repo.deliveryRunsFor(id) {
			if r.Status == RunStatusDropped {
				dropped++
			}
		}
	}
	if dropped == 0 {
		t.Error("an over-depth backlog should record evictions rather than silently discard them")
	}
}

// A fire that arrives at an auto-paused trigger is recorded as paused, so it is
// distinguishable from a trigger that was never listening.
func TestAdmission_PausedTrigger_RecordsPaused(t *testing.T) {
	svc, deps := newTestService(t)
	trig := newAdmissionTrigger(t, deps, OverflowWait, 4, 32)
	now := time.Now()
	trig.AutoPausedAt = &now

	ids := enqueue(t, svc, deps, trig, 1)

	runs := deps.repo.deliveryRunsFor(ids[0])
	if len(runs) != 1 || runs[0].Status != RunStatusPaused {
		t.Fatalf("outcomes = %+v, want one paused", runs)
	}
	if len(deps.repo.pendingFor(trig.ID)) != 0 {
		t.Error("a paused trigger must not accumulate a backlog")
	}
}

// Disabling a trigger drops what is waiting, visibly. Those fires would
// otherwise drain into a trigger the admin just turned off.
func TestAdmission_Drain_RecordsDroppedOnDisable(t *testing.T) {
	svc, deps := newTestService(t)
	trig := newAdmissionTrigger(t, deps, OverflowWait, 4, 32)
	ids := enqueue(t, svc, deps, trig, 2)

	n, err := svc.admission.Drain(context.Background(), trig, "the trigger was disabled")
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if n != 2 {
		t.Errorf("drained %d, want 2", n)
	}
	for _, id := range ids {
		runs := deps.repo.deliveryRunsFor(id)
		if len(runs) != 1 || runs[0].Status != RunStatusDropped {
			t.Errorf("delivery %s outcomes = %+v, want one dropped", id, runs)
		}
	}
}

// ─── Phase 3: auto-pause ─────────────────────────────────────────────────────

// A trigger failing repeatedly disables itself. A policy denial counts: a
// trigger whose grant was revoked would otherwise be denied forever and never
// pause, which is exactly the case auto-pause exists for.
func TestAutoPause_DisablesAfterConsecutiveFailures(t *testing.T) {
	svc, deps := newTestService(t)
	trig := newAdmissionTrigger(t, deps, OverflowWait, 4, 32)

	for range autoPauseThreshold {
		svc.noteFailure(context.Background(), trig, errTriggerSideFailure)
	}

	stored, err := deps.repo.GetTrigger(context.Background(), testTenant, trig.ID)
	if err != nil {
		t.Fatalf("get trigger: %v", err)
	}
	if stored.Enabled {
		t.Error("trigger should be disabled after sustained failure")
	}
	if stored.AutoPausedAt == nil {
		t.Error("auto_paused_at should record that the platform paused it, not a person")
	}
}

// Consecutive means consecutive: one success is enough to say it is working.
func TestAutoPause_SuccessResetsTheCount(t *testing.T) {
	svc, deps := newTestService(t)
	trig := newAdmissionTrigger(t, deps, OverflowWait, 4, 32)

	for range autoPauseThreshold - 1 {
		svc.noteFailure(context.Background(), trig, errTriggerSideFailure)
	}
	svc.noteSuccess(context.Background(), trig)
	svc.noteFailure(context.Background(), trig, errTriggerSideFailure)

	stored, _ := deps.repo.GetTrigger(context.Background(), testTenant, trig.ID)
	if !stored.Enabled {
		t.Error("a success should reset the counter, leaving the trigger enabled")
	}
}

// A platform fault is not the trigger's fault, and none of its forms may move a
// trigger toward auto-pause: orbit-agents erroring (5xx) or unreachable (a raw
// transport error), a dispatch that timed out, shutdown cancelling the call, or
// a token the identity service could not mint. Each fails every trigger in the
// tenant at once, so counting any of them would turn a transient blip into an
// estate-wide manual re-enable. The transport, timeout, cancel and mint cases
// arrive as errors with no shared sentinel, which is why the classifier is an
// allowlist: only a named trigger-side fault counts, everything else does not.
func TestAutoPause_IgnoresPlatformFaults(t *testing.T) {
	platformFaults := map[string]error{
		"upstream 5xx":      fmt.Errorf("start run: %w", agentsclient.ErrUpstreamUnavailable),
		"transport error":   errors.New("start run: dial tcp: connection refused"),
		"dispatch timeout":  fmt.Errorf("start run: %w", context.DeadlineExceeded),
		"shutdown cancel":   fmt.Errorf("start run: %w", context.Canceled),
		"token mint failed": errors.New("mint trigger token: identity unavailable"),
	}

	for name, fault := range platformFaults {
		t.Run(name, func(t *testing.T) {
			svc, deps := newTestService(t)
			trig := newAdmissionTrigger(t, deps, OverflowWait, 4, 32)

			for range autoPauseThreshold * 2 {
				svc.noteFailure(context.Background(), trig, fault)
			}

			stored, err := deps.repo.GetTrigger(context.Background(), testTenant, trig.ID)
			if err != nil {
				t.Fatalf("get trigger: %v", err)
			}
			if !stored.Enabled {
				t.Error("a platform fault should not auto-pause the trigger")
			}
			if stored.ConsecutiveFailures != 0 {
				t.Errorf("consecutive_failures = %d, want 0: a platform fault says nothing about this trigger",
					stored.ConsecutiveFailures)
			}
		})
	}
}

// Opting out means opting out.
func TestAutoPause_RespectsOptOut(t *testing.T) {
	svc, deps := newTestService(t)
	trig := newAdmissionTrigger(t, deps, OverflowWait, 4, 32)
	trig.AutopauseEnabled = false

	for range autoPauseThreshold * 2 {
		svc.noteFailure(context.Background(), trig, errTriggerSideFailure)
	}

	stored, _ := deps.repo.GetTrigger(context.Background(), testTenant, trig.ID)
	if !stored.Enabled {
		t.Error("a trigger with auto-pause off must stay enabled")
	}
}

// ─── Unknown trigger name stays honest ───────────────────────────────────────

// The one case where the name is genuinely unknown stays honest: a fire whose
// trigger could not be read records the id and no name, rather than inventing
// one.
func TestDispatchPending_UnreadableTrigger_RecordsNoName(t *testing.T) {
	svc, deps := newTestService(t)

	svc.DispatchPending(context.Background(), PendingDelivery{
		ID: "p-1", TenantID: testTenant, TriggerID: "gone", DeliveryID: "d-1",
	})

	runs := deps.repo.deliveryRunsFor("d-1")
	if len(runs) != 1 || runs[0].Status != RunStatusDropped {
		t.Fatalf("outcomes = %+v, want one dropped", runs)
	}
	if runs[0].TriggerName != "" {
		t.Errorf("trigger_name = %q, want it left empty when the trigger cannot be read", runs[0].TriggerName)
	}
}

// ─── Stuck-claim reaper ──────────────────────────────────────────────────────

// A pod that dies between claiming a fire and dispatching it strands the row:
// nothing re-claims it, so the fire never starts. The queue's whole promise is
// that an acknowledged fire always starts, so the worker takes it back.
func TestWorker_ReclaimsStrandedClaim(t *testing.T) {
	svc, deps := newTestService(t)
	trig := newAdmissionTrigger(t, deps, OverflowWait, 4, 32)
	ctx := context.Background()

	ids := enqueue(t, svc, deps, trig, 1)

	// A worker claims it and dies: no dispatch, no release.
	claimed, err := deps.repo.ClaimPending(ctx, 10)
	if err != nil {
		t.Fatalf("ClaimPending: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claimed %d fires, want 1", len(claimed))
	}
	if again, _ := deps.repo.ClaimPending(ctx, 10); len(again) != 0 {
		t.Fatal("a claimed fire must not be claimable again while the claim is fresh")
	}
	deps.repo.backdateClaim(claimed[0].ID, StaleClaimWindow+time.Minute)

	worker := NewWorker(svc, deps.repo, silentLogger())
	worker.drain(ctx)

	// Reclaimed, dispatched, and released — the fire finally ran.
	if pending := deps.repo.pendingFor(trig.ID); len(pending) != 0 {
		t.Errorf("pending rows = %d, want the reclaimed fire dispatched and released", len(pending))
	}
	runs := deps.repo.deliveryRunsFor(ids[0])
	if len(runs) != 1 || runs[0].Status != RunStatusDispatched {
		t.Fatalf("outcomes = %+v, want the reclaimed fire dispatched", runs)
	}
	if deps.agents.callCount() != 1 {
		t.Errorf("run starts = %d, want exactly one (reclaimed, not duplicated)", deps.agents.callCount())
	}
}

// A claim that is still within the window belongs to a worker that may yet
// dispatch it. Reclaiming it would start the same fire twice, which is worse
// than a stranded fire starting late.
func TestWorker_DoesNotReclaimALiveClaim(t *testing.T) {
	svc, deps := newTestService(t)
	trig := newAdmissionTrigger(t, deps, OverflowWait, 4, 32)
	ctx := context.Background()
	enqueue(t, svc, deps, trig, 1)

	claimed, _ := deps.repo.ClaimPending(ctx, 10)
	if len(claimed) != 1 {
		t.Fatalf("claimed %d, want 1", len(claimed))
	}

	reclaimed, err := deps.repo.ReclaimStalePending(ctx, StaleClaimWindow)
	if err != nil {
		t.Fatalf("ReclaimStalePending: %v", err)
	}
	if len(reclaimed) != 0 {
		t.Errorf("reclaimed %v, want a fresh claim left alone", reclaimed)
	}
}

// ─── Drain vs live claims ────────────────────────────────────────────────────

// Draining a trigger must not delete a row a worker is dispatching right now:
// the drain and the live worker would each record an outcome, and one fire would
// show up twice in the delivery history saying two different things.
func TestDrain_LeavesAClaimedFireToItsWorker(t *testing.T) {
	svc, deps := newTestService(t)
	trig := newAdmissionTrigger(t, deps, OverflowWait, 4, 32)
	ctx := context.Background()

	enqueue(t, svc, deps, trig, 2)
	claimed, err := deps.repo.ClaimPending(ctx, 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("ClaimPending = %v, %v; want one claim", claimed, err)
	}

	n, err := svc.admission.Drain(ctx, trig, "the trigger was disabled while this fire was waiting")
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if n != 1 {
		t.Errorf("drained %d fires, want only the one still waiting", n)
	}
	remaining := deps.repo.pendingFor(trig.ID)
	if len(remaining) != 1 || remaining[0].ID != claimed[0].ID {
		t.Errorf("remaining queue = %+v, want exactly the claimed row", remaining)
	}
}

// A fire admitted in the window between a disable committing and its drain
// running is claimable by nobody (claiming requires an enabled trigger) and
// reapable by nobody (the reaper only touches claimed rows). The worker's sweep
// is what stops it waiting forever.
func TestWorker_CollectsFiresWhoseTriggerCanNoLongerRunThem(t *testing.T) {
	svc, deps := newTestService(t)
	trig := newAdmissionTrigger(t, deps, OverflowWait, 4, 32)
	ctx := context.Background()

	ids := enqueue(t, svc, deps, trig, 1)
	disabled := trig
	disabled.Enabled = false
	if err := deps.repo.UpdateTrigger(ctx, &disabled, true); err != nil {
		t.Fatalf("UpdateTrigger: %v", err)
	}

	NewWorker(svc, deps.repo, silentLogger()).drain(ctx)

	if got := len(deps.repo.pendingFor(trig.ID)); got != 0 {
		t.Errorf("orphaned fires still queued = %d, want 0", got)
	}
	runs := deps.repo.deliveryRunsFor(ids[0])
	if len(runs) != 1 || runs[0].Status != RunStatusDropped {
		t.Fatalf("outcomes = %+v, want one 'dropped' so the work is visibly accounted for", runs)
	}
}

// Once shutdown is signalled the worker must stop taking new rows from a batch:
// starting them would race the process exit, and the untouched claims are
// picked up on the next tick or by another replica. (A dispatch already in
// flight finishes on its own detached context, which the ingest-side pattern
// covers.)
func TestWorker_ShutdownDoesNotStartNewDispatches(t *testing.T) {
	svc, deps := newTestService(t)
	trig := newAdmissionTrigger(t, deps, OverflowWait, 4, 32)
	enqueue(t, svc, deps, trig, 3)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // shutdown already signalled before this drain runs

	worker := NewWorker(svc, deps.repo, silentLogger())
	worker.drain(ctx)

	if n := deps.agents.callCount(); n != 0 {
		t.Errorf("a shutting-down worker started %d runs, want 0: it must not take new work", n)
	}
}

// The worker re-stamps the rows still waiting behind each dispatch, not just the
// one it is about to run. A batch is claimed in one statement and dispatched
// serially, so a row waiting behind a slow dispatch would otherwise keep its
// original claim and age past the stale window, letting another replica reclaim
// and double-dispatch it. Each dispatch should re-stamp the shrinking remainder.
func TestWorker_ReStampsTheRemainingBatchBeforeEachDispatch(t *testing.T) {
	svc, deps := newTestService(t)
	trig := newAdmissionTrigger(t, deps, OverflowWait, 4, 32)
	enqueue(t, svc, deps, trig, 3)

	worker := NewWorker(svc, deps.repo, silentLogger())
	worker.drain(context.Background())

	batches := deps.repo.touchedBatches()
	if len(batches) != 3 {
		t.Fatalf("re-stamp calls = %d, want one before each of the 3 dispatches", len(batches))
	}
	// The remainder shrinks by one each dispatch: 3, then 2, then 1.
	if len(batches[0]) != 3 || len(batches[1]) != 2 || len(batches[2]) != 1 {
		t.Errorf("re-stamp batch sizes = %d/%d/%d, want 3/2/1 (the shrinking remainder)",
			len(batches[0]), len(batches[1]), len(batches[2]))
	}
}
