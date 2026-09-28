package triggers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestIngest_SourceNotFound(t *testing.T) {
	svc, _ := newTestService(t)
	if _, err := svc.Ingest(context.Background(), "nope", http.Header{}, []byte(`{}`)); !errors.Is(err, ErrSourceNotFound) {
		t.Errorf("got %v, want ErrSourceNotFound", err)
	}
}

func TestIngest_Disabled(t *testing.T) {
	svc, deps := newTestService(t)
	src := mustSource(t, deps.sources)
	disabled := false
	_, _ = deps.sources.UpdateSource(context.Background(), testTenant, src.ID, UpdateSourceRequest{Enabled: &disabled})

	body := prBody("opened", "main")
	if _, err := svc.Ingest(context.Background(), src.ID, sign(src.Secret, body), body); !errors.Is(err, ErrSourceDisabled) {
		t.Errorf("got %v, want ErrSourceDisabled", err)
	}
}

func TestIngest_SignatureInvalid_NoRow(t *testing.T) {
	svc, deps := newTestService(t)
	src := mustSource(t, deps.sources)
	body := prBody("opened", "main")
	h := http.Header{"X-Hub-Signature-256": {"sha256=bad"}, "X-Github-Event": {"pull_request"}}

	if _, err := svc.Ingest(context.Background(), src.ID, h, body); !errors.Is(err, ErrSignatureInvalid) {
		t.Errorf("got %v, want ErrSignatureInvalid", err)
	}
	if len(deps.repo.deliveries) != 0 {
		t.Error("an unverified delivery must not be recorded")
	}
}

func TestIngest_MissingDeliveryID_NoRow(t *testing.T) {
	// A validly-signed body with the delivery-id header stripped must be rejected
	// fail-closed: the id is the sole dedup key and is not covered by the
	// signature, so accepting it would re-open the replay hole.
	svc, deps := newTestService(t)
	src := mustSource(t, deps.sources)
	body := prBody("opened", "main")
	h := sign(src.Secret, body)
	h.Del("X-Github-Delivery")

	if _, err := svc.Ingest(context.Background(), src.ID, h, body); !errors.Is(err, ErrMissingDeliveryID) {
		t.Errorf("got %v, want ErrMissingDeliveryID", err)
	}
	if len(deps.repo.deliveries) != 0 {
		t.Error("a delivery with no provider id must not be recorded")
	}
}

func TestIngest_Accepted_NoTriggerSkipped(t *testing.T) {
	svc, deps := newTestService(t)
	src := mustSource(t, deps.sources)
	body := prBody("opened", "main")

	out, err := svc.Ingest(context.Background(), src.ID, sign(src.Secret, body), body)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if out != IngestAccepted {
		t.Errorf("outcome = %v, want IngestAccepted", out)
	}
	// The fan-out re-derives the delivery when it finishes, which is the signal
	// that it ran at all. Nothing matched, so it recorded no outcomes.
	waitFor(t, deps.repo.anyDerived)
	if got := deps.repo.outcomeStatuses(deps.repo.lastDelivery().ID); len(got) != 0 {
		t.Errorf("outcomes = %v, want none: no trigger exists", got)
	}
	if deps.agents.callCount() != 0 {
		t.Error("no run should be started when nothing matches")
	}
}

// A repeat arrival is recorded as its own delivery pointing at the original.
// "The provider sent this five times" is a question the history has to answer,
// and the repeats used to leave nothing at all.
func TestIngest_Duplicate_RecordsLinkedRow(t *testing.T) {
	svc, deps := newTestService(t)
	src := mustSource(t, deps.sources)
	body := prBody("opened", "main")
	h := sign(src.Secret, body)

	if out, _ := svc.Ingest(context.Background(), src.ID, h, body); out != IngestAccepted {
		t.Fatalf("first delivery should be accepted")
	}
	original := deps.repo.lastDelivery()

	out, err := svc.Ingest(context.Background(), src.ID, h, body)
	if err != nil {
		t.Fatalf("Ingest(dup): %v", err)
	}
	if out != IngestDuplicate {
		t.Errorf("redelivery outcome = %v, want IngestDuplicate", out)
	}

	all := deps.repo.allDeliveries()
	if len(all) != 2 {
		t.Fatalf("want the repeat recorded as its own row, got %d deliveries", len(all))
	}
	dup := all[1]
	if dup.Status != StatusDuplicate {
		t.Errorf("duplicate status = %q, want %q", dup.Status, StatusDuplicate)
	}
	if dup.RedeliveryOf == nil || *dup.RedeliveryOf != original.ID {
		t.Errorf("duplicate redelivery_of = %v, want the original (%s)", dup.RedeliveryOf, original.ID)
	}
	if dup.Reason == nil || *dup.Reason == "" {
		t.Error("a duplicate should say why it was not re-run")
	}
	if len(dup.Payload) != 0 {
		t.Error("a duplicate must not capture the payload again; the original holds it")
	}

	// The original is history: a retry days later must not rewrite it. Its own
	// status is settled by the first arrival's fan-out, which runs detached, so
	// what is asserted here is the part the repeat could have overwritten —
	// never that the original became lineage of its own retry, and never that
	// the retry took its payload.
	got := deps.repo.deliveryByID(original.ID)
	if got.RedeliveryOf != nil {
		t.Errorf("the repeat rewrote the original as lineage: redelivery_of = %v", got.RedeliveryOf)
	}
	if got.Status == StatusDuplicate {
		t.Error("the repeat rewrote the original's status to 'duplicate'")
	}
	if len(got.Payload) != len(original.Payload) {
		t.Errorf("the repeat changed the original's payload: %d bytes, want %d", len(got.Payload), len(original.Payload))
	}
	if len(deps.repo.deliveryRunsFor(dup.ID)) != 0 {
		t.Error("a duplicate must not start a run for a trigger that dedupes")
	}
}

// dedupe_enabled is stored per trigger, so whether a repeat is suppressed or
// re-run is a per-trigger decision. The unique index cannot express that; this
// is the level that can.
func TestIngest_Duplicate_HonoursPerTriggerDedupe(t *testing.T) {
	svc, deps := newTestService(t)
	src := mustSource(t, deps.sources)
	dedupes := mustWebhookTrigger(t, deps.triggers, src.ID, "dedupes", true)
	reruns := mustWebhookTrigger(t, deps.triggers, src.ID, "re-runs", false)

	body := prBody("opened", "main")
	h := sign(src.Secret, body)

	if _, err := svc.Ingest(context.Background(), src.ID, h, body); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	waitFor(t, func() bool { return len(deps.repo.pendingFor(dedupes.ID))+len(deps.repo.pendingFor(reruns.ID)) == 2 })
	drainPending(t, svc, deps.repo)
	if deps.agents.callCount() != 2 {
		t.Fatalf("first arrival should start both runs, got %d", deps.agents.callCount())
	}

	if _, err := svc.Ingest(context.Background(), src.ID, h, body); err != nil {
		t.Fatalf("Ingest(dup): %v", err)
	}
	dup := deps.repo.lastDelivery()
	waitFor(t, func() bool { return len(deps.repo.pendingFor(reruns.ID)) == 1 })
	drainPending(t, svc, deps.repo)

	if deps.agents.callCount() != 3 {
		t.Errorf("run starts = %d, want 3 (only the dedupe-off trigger re-runs)", deps.agents.callCount())
	}
	runs := deps.repo.deliveryRunsFor(dup.ID)
	if len(runs) != 1 {
		t.Fatalf("duplicate fan-out = %+v, want exactly one outcome", runs)
	}
	if runs[0].TriggerID != reruns.ID || runs[0].Status != RunStatusDispatched {
		t.Errorf("duplicate outcome = %+v, want dispatched for the dedupe-off trigger", runs[0])
	}
	// The arrival is still a duplicate, whatever a trigger then did with it.
	if got := deps.repo.deliveryByID(dup.ID); got.Status != StatusDuplicate {
		t.Errorf("duplicate status = %q after re-run, want it preserved", got.Status)
	}
}

// ── Dispatch ────────────────────────────────────────────────────────────────

func TestDispatch_Match_StartsRun(t *testing.T) {
	svc, deps, src, delID := dispatchSetup(t, map[string][]string{"action": {"opened"}}, "Review PR #{{ pull_request.number }}")
	adapter, _ := mustRegistry().Get(ProviderGitHub)

	svc.dispatch(context.Background(), src, adapter, arrival{deliveryID: delID, eventType: "pull_request", body: prBody("opened", "main")})
	drainPending(t, svc, deps.repo)

	if deps.agents.callCount() != 1 {
		t.Fatalf("expected exactly one run start, got %d", deps.agents.callCount())
	}
	call, _ := deps.agents.lastCall()
	if call.bearer != "Bearer minted-token" {
		t.Errorf("run started with bearer %q, want the minted source token", call.bearer)
	}
	if call.agentTypeID != "at-1" {
		t.Errorf("run agentTypeID = %q", call.agentTypeID)
	}
	if !strings.HasPrefix(call.initialMessage, "Review PR #{{ pull_request.number }}\n\n<delivery-data>") ||
		!strings.Contains(call.initialMessage, `"pull_request.number": "42"`) {
		t.Errorf("prompt not rendered with its data block: %q", call.initialMessage)
	}
	// Minted token must be for the source's identity.
	if len(deps.minter.calls) != 1 || deps.minter.calls[0].principalID != src.PrincipalID {
		t.Errorf("minter calls = %+v, want principal %q", deps.minter.calls, src.PrincipalID)
	}
	// Delivery recorded as dispatched with the run id.
	d := deps.repo.deliveryByID(delID)
	if d.Status != StatusDispatched || d.RunID == nil || *d.RunID != "run-1" {
		t.Errorf("delivery outcome = %+v, want dispatched/run-1", d)
	}
}

func TestDispatch_MultipleTriggers_RecordsAllRuns(t *testing.T) {
	// Two triggers match the same delivery: every run must be recorded in the
	// join table, and ListDeliveries must surface the full fan-out on the delivery.
	svc, deps := newTestService(t)
	src := mustSource(t, deps.sources)
	cond := map[string][]string{"action": {"opened"}}
	mustTrigger(t, deps.triggers, src.ID, cond, "p1")
	mustTrigger(t, deps.triggers, src.ID, cond, "p2")
	stored, _ := deps.repo.GetSourceByID(context.Background(), src.ID)
	delID := "deliv-multi"
	_, _ = deps.repo.InsertDelivery(context.Background(), &Delivery{
		ID: delID, TenantID: testTenant, SourceID: src.ID, EventType: "pull_request", Status: StatusVerified,
	})
	deps.minter.reset()

	adapter, _ := mustRegistry().Get(ProviderGitHub)
	svc.dispatch(context.Background(), *stored, adapter, arrival{deliveryID: delID, eventType: "pull_request", body: prBody("opened", "main")})
	drainPending(t, svc, deps.repo)

	if deps.agents.callCount() != 2 {
		t.Fatalf("expected 2 runs started, got %d", deps.agents.callCount())
	}
	runs := deps.repo.deliveryRunsFor(delID)
	if len(runs) != 2 {
		t.Fatalf("expected 2 delivery-run records, got %d", len(runs))
	}
	for _, r := range runs {
		if r.Status != RunStatusDispatched || r.RunID == nil {
			t.Errorf("run record = %+v, want dispatched with a run id", r)
		}
	}

	// Delivery summary points at the first dispatch; ListDeliveries attaches all runs.
	list, err := svc.ListDeliveries(context.Background(), testTenant, DeliveryFilter{SourceID: src.ID})
	if err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	var got *Delivery
	for i := range list {
		if list[i].ID == delID {
			got = &list[i]
		}
	}
	if got == nil {
		t.Fatal("delivery not found in listing")
	}
	if got.Status != StatusDispatched || got.RunID == nil {
		t.Errorf("delivery summary = %+v, want dispatched with a run id", got)
	}
	if len(got.Runs) != 2 {
		t.Errorf("delivery.Runs = %d, want 2 (full fan-out)", len(got.Runs))
	}
}

// A webhook trigger names exactly one event type.

func TestDispatch_EventTypeMismatch_Skipped(t *testing.T) {
	svc, deps, src, delID := dispatchSetup(t, nil, "p")
	adapter, _ := mustRegistry().Get(ProviderGitHub)

	// Trigger is for pull_request; deliver a push.
	svc.dispatch(context.Background(), src, adapter, arrival{deliveryID: delID, eventType: "push", body: []byte(`{"ref":"refs/heads/main"}`)})

	if deps.agents.callCount() != 0 {
		t.Error("event-type mismatch should not start a run")
	}
	if got := deps.repo.outcomeStatuses(delID); len(got) != 0 {
		t.Errorf("outcomes = %v, want none", got)
	}
}

func TestDispatch_ConditionMismatch_Skipped(t *testing.T) {
	svc, deps, src, delID := dispatchSetup(t, map[string][]string{"pr.base_ref": {"main"}}, "p")
	adapter, _ := mustRegistry().Get(ProviderGitHub)

	// base_ref is "release", condition requires "main".
	svc.dispatch(context.Background(), src, adapter, arrival{deliveryID: delID, eventType: "pull_request", body: prBody("opened", "release")})
	drainPending(t, svc, deps.repo)

	if deps.agents.callCount() != 0 {
		t.Error("condition mismatch should not start a run")
	}
	// A trigger that declined the event records that it declined, which is what
	// keeps a filtered delivery distinguishable from one nothing listened for.
	if got := deps.repo.outcomeStatuses(delID); len(got) != 1 || got[0] != RunStatusFiltered {
		t.Errorf("outcomes = %v, want one %q", got, RunStatusFiltered)
	}
}

func TestDispatch_StartRunError_Failed(t *testing.T) {
	svc, deps, src, delID := dispatchSetup(t, map[string][]string{"action": {"opened"}}, "p")
	deps.agents.status = http.StatusInternalServerError
	adapter, _ := mustRegistry().Get(ProviderGitHub)

	svc.dispatch(context.Background(), src, adapter, arrival{deliveryID: delID, eventType: "pull_request", body: prBody("opened", "main")})
	drainPending(t, svc, deps.repo)

	d := deps.repo.deliveryByID(delID)
	if d.Status != StatusFailed {
		t.Errorf("status = %q, want failed", d.Status)
	}
	if d.Error == nil || *d.Error == "" {
		t.Error("failed delivery should record an error message")
	}
}

func TestDispatch_DelegatedStartRefusal_RecordsReason(t *testing.T) {
	// Defense in depth for a trigger that predates the identity model (or a
	// race): if orbit-agents' run-start principal gate refuses the SERVICE
	// start with the delegated-identity 403, the delivery must record that
	// reason legibly — the upstream body flows into the delivery error via the
	// agents client's error wrapping, not as an opaque 403.
	svc, deps, src, delID := dispatchSetup(t, map[string][]string{"action": {"opened"}}, "p")
	// The scenario is a trigger that predates the identity model, so it is old:
	// a young trigger's 403 is retried as grant propagation instead.
	deps.repo.mu.Lock()
	for _, tr := range deps.repo.triggers {
		tr.CreatedAt = time.Now().Add(-2 * time.Hour)
	}
	deps.repo.mu.Unlock()
	deps.agents.status = http.StatusForbidden
	deps.agents.runErrBody = `{"code":"forbidden","message":"this agent uses the delegated identity model: runs can only be started by a person (HUMAN or PAT principal)"}`
	adapter, _ := mustRegistry().Get(ProviderGitHub)

	svc.dispatch(context.Background(), src, adapter, arrival{deliveryID: delID, eventType: "pull_request", body: prBody("opened", "main")})
	drainPending(t, svc, deps.repo)

	d := deps.repo.deliveryByID(delID)
	if d.Status != StatusFailed {
		t.Errorf("status = %q, want failed", d.Status)
	}
	if d.Error == nil || !strings.Contains(*d.Error, "delegated identity model") {
		t.Errorf("delivery error = %v, want the delegated-identity reason recorded verbatim", d.Error)
	}
	// A 403 from the run-start gate is a policy refusal, not a malfunction: the
	// trigger works, its grant does not. It records as denied so an admin
	// reaches for the grant rather than the agent.
	runs := deps.repo.deliveryRunsFor(delID)
	if len(runs) != 1 || runs[0].Status != RunStatusDenied {
		t.Fatalf("delivery runs = %+v, want one denied row", runs)
	}
	if runs[0].Error == nil || !strings.Contains(*runs[0].Error, "delegated identity model") {
		t.Errorf("delivery run error = %v, want the delegated-identity reason recorded verbatim", runs[0].Error)
	}
}

func TestDispatch_MintError_Failed(t *testing.T) {
	svc, deps, src, delID := dispatchSetup(t, map[string][]string{"action": {"opened"}}, "p")
	deps.minter.err = errors.New("mint boom")
	adapter, _ := mustRegistry().Get(ProviderGitHub)

	svc.dispatch(context.Background(), src, adapter, arrival{deliveryID: delID, eventType: "pull_request", body: prBody("opened", "main")})
	drainPending(t, svc, deps.repo)

	if deps.agents.callCount() != 0 {
		t.Error("run must not start if token minting fails")
	}
	if d := deps.repo.deliveryByID(delID); d.Status != StatusFailed {
		t.Errorf("status = %q, want failed", d.Status)
	}
}

// ─── Phase 1: the two-level outcome model ────────────────────────────────────
//
// Outcomes live at two levels: what happened to the arrival, and what happened
// for each trigger it matched. Conflating them is the main way this design goes
// wrong, so these tests pin the distinction.

// The case the old model could not express. A delivery matching three triggers
// where one dispatches, one is denied and one is filtered must record all three
// per-trigger outcomes, and summarise as dispatched — not lose the denial to the
// first success.
func TestDispatch_FanOut_RecordsEveryTriggerOutcome(t *testing.T) {
	svc, deps := newTestService(t)
	src := mustSource(t, deps.sources)

	// Two triggers on the same event; one has a condition this event fails.
	mustTrigger(t, deps.triggers, src.ID, map[string][]string{"action": {"opened"}}, "dispatches")
	mustTrigger(t, deps.triggers, src.ID, map[string][]string{"pr.base_ref": {"never-matches"}}, "filtered")

	stored, _ := deps.repo.GetSourceByID(context.Background(), src.ID)
	delID := "deliv-fanout"
	_, _ = deps.repo.InsertDelivery(context.Background(), &Delivery{
		ID: delID, TenantID: testTenant, SourceID: src.ID,
		EventType: "pull_request", Status: StatusVerified,
	})
	deps.minter.reset()
	adapter, _ := mustRegistry().Get(ProviderGitHub)

	svc.dispatch(context.Background(), *stored, adapter, arrival{deliveryID: delID, eventType: "pull_request", body: prBody("opened", "main")})
	drainPending(t, svc, deps.repo)

	runs := deps.repo.deliveryRunsFor(delID)
	if len(runs) != 2 {
		t.Fatalf("want an outcome per matched trigger, got %d: %+v", len(runs), runs)
	}

	got := map[string]int{}
	for _, r := range runs {
		got[r.Status]++
	}
	if got[RunStatusDispatched] != 1 {
		t.Errorf("want 1 dispatched outcome, got %d (%v)", got[RunStatusDispatched], got)
	}
	if got[RunStatusFiltered] != 1 {
		t.Errorf("want 1 filtered outcome, got %d (%v)", got[RunStatusFiltered], got)
	}

	// The summary reflects that something ran, without erasing the filtered row.
	if d := deps.repo.deliveryByID(delID); d.Status != StatusDispatched {
		t.Errorf("delivery status = %q, want dispatched", d.Status)
	}
}

// A trigger that declined an event via its own match_condition worked exactly as
// configured. That is not a failure, and an admin must be able to tell it apart
// from "nothing was listening".
func TestDispatch_OnlyFiltered_IsSkippedNotFailed(t *testing.T) {
	svc, deps, src, delID := dispatchSetup(t, map[string][]string{"pr.base_ref": {"main"}}, "p")
	adapter, _ := mustRegistry().Get(ProviderGitHub)

	svc.dispatch(context.Background(), src, adapter, arrival{deliveryID: delID, eventType: "pull_request", body: prBody("opened", "release")})
	drainPending(t, svc, deps.repo)

	runs := deps.repo.deliveryRunsFor(delID)
	if len(runs) != 1 || runs[0].Status != RunStatusFiltered {
		t.Fatalf("want one filtered outcome, got %+v", runs)
	}
	// The delivery reading as skipped rather than failed is the derivation's
	// job, asserted in TestPostgres_DeriveDeliveryStatus. What matters here is
	// that the outcome recorded is 'filtered' and not a failure.
}

// An event no trigger is listening for produces no per-trigger rows at all.
// Recording one per uninterested trigger would turn a noisy source into a write
// multiplier, and "nobody wanted this" is not a filter decision.
func TestDispatch_UnmatchedEventType_RecordsNoTriggerOutcomes(t *testing.T) {
	svc, deps, src, delID := dispatchSetup(t, map[string][]string{"action": {"opened"}}, "p")
	adapter, _ := mustRegistry().Get(ProviderGitHub)

	svc.dispatch(context.Background(), src, adapter, arrival{deliveryID: delID, eventType: "issues", body: prBody("opened", "main")})
	drainPending(t, svc, deps.repo)

	if runs := deps.repo.deliveryRunsFor(delID); len(runs) != 0 {
		t.Errorf("want no per-trigger outcomes for an unlistened event, got %+v", runs)
	}
	if deps.repo.derivedCount(delID) == 0 {
		t.Error("the delivery was never re-derived, so its status would stay 'verified'")
	}
}

// ─── Phase 1: failed authentication never becomes a delivery ─────────────────

// A bad signature is an unauthenticated request. Recording one as a delivery
// would let anyone on the internet grow the table, so it counts against the
// source instead — which still tells an admin their signing secret is wrong.
func TestIngest_InvalidSignature_CountsOnSourceWithoutCreatingDelivery(t *testing.T) {
	svc, deps := newTestService(t)
	src := mustSource(t, deps.sources)

	before := len(deps.repo.allDeliveries())

	_, err := svc.Ingest(context.Background(), src.ID, http.Header{
		"X-Github-Event":      []string{"pull_request"},
		"X-Github-Delivery":   []string{"d-1"},
		"X-Hub-Signature-256": []string{"sha256=deadbeef"},
	}, prBody("opened", "main"))

	if !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("err = %v, want ErrSignatureInvalid", err)
	}
	if after := len(deps.repo.allDeliveries()); after != before {
		t.Errorf("an unauthenticated request created %d delivery rows; it must create none", after-before)
	}

	stored, _ := deps.repo.GetSourceByID(context.Background(), src.ID)
	if stored.RejectionCount != 1 {
		t.Errorf("source rejection_count = %d, want 1", stored.RejectionCount)
	}
	if stored.LastRejectionReason != "invalid signature" {
		t.Errorf("last_rejection_reason = %q, want the specific cause", stored.LastRejectionReason)
	}
	if stored.LastRejectionAt == nil {
		t.Error("last_rejection_at should be set so an admin can see how recent the failures are")
	}
}

// Repeated rejections are bounded by construction: they update one existing row
// rather than inserting, so volume is fixed no matter what an attacker sends.
func TestIngest_RepeatedRejections_DoNotGrowAnyTable(t *testing.T) {
	svc, deps := newTestService(t)
	src := mustSource(t, deps.sources)

	for range 25 {
		_, _ = svc.Ingest(context.Background(), src.ID, http.Header{
			"X-Github-Event":      []string{"pull_request"},
			"X-Github-Delivery":   []string{"d-x"},
			"X-Hub-Signature-256": []string{"sha256=bad"},
		}, prBody("opened", "main"))
	}

	if n := len(deps.repo.allDeliveries()); n != 0 {
		t.Errorf("25 unauthenticated requests created %d rows; they must create none", n)
	}
	stored, _ := deps.repo.GetSourceByID(context.Background(), src.ID)
	if stored.RejectionCount != 25 {
		t.Errorf("rejection_count = %d, want 25", stored.RejectionCount)
	}
}

// ─── Auto-pause is visible, manual disable is not ────────────────────────────

// An auto-paused webhook trigger stays visible in the delivery history: its
// fires are recorded as paused. With enabled = false alone it was
// indistinguishable from a trigger that had been deleted.
func TestDispatch_AutoPausedTrigger_RecordsPaused(t *testing.T) {
	svc, deps := newTestService(t)
	src := mustSource(t, deps.sources)
	paused := mustWebhookTrigger(t, deps.triggers, src.ID, "paused", true)
	if err := deps.repo.AutoPause(context.Background(), paused.ID); err != nil {
		t.Fatalf("AutoPause: %v", err)
	}

	body := prBody("opened", "main")
	if _, err := svc.Ingest(context.Background(), src.ID, sign(src.Secret, body), body); err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	var runs []DeliveryRun
	waitFor(t, func() bool {
		d := deps.repo.lastDelivery()
		runs = deps.repo.deliveryRunsFor(d.ID)
		return len(runs) == 1
	})
	if runs[0].TriggerID != paused.ID || runs[0].Status != RunStatusPaused {
		t.Errorf("outcome = %+v, want paused for the auto-paused trigger", runs[0])
	}
	if deps.agents.callCount() != 0 {
		t.Error("an auto-paused trigger must not start a run")
	}
}

// A trigger a person turned off records nothing. That silence was asked for.
func TestDispatch_ManuallyDisabledTrigger_RecordsNothing(t *testing.T) {
	svc, deps := newTestService(t)
	src := mustSource(t, deps.sources)
	off := mustWebhookTrigger(t, deps.triggers, src.ID, "off", true)
	enabled := false
	if _, err := deps.triggers.UpdateTrigger(context.Background(), testTenant, off.ID, UpdateTriggerRequest{Enabled: &enabled}); err != nil {
		t.Fatalf("UpdateTrigger: %v", err)
	}

	body := prBody("opened", "main")
	if _, err := svc.Ingest(context.Background(), src.ID, sign(src.Secret, body), body); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	waitFor(t, deps.repo.anyDerived)

	if runs := deps.repo.deliveryRunsFor(deps.repo.lastDelivery().ID); len(runs) != 0 {
		t.Errorf("a manually disabled trigger recorded %+v, want nothing", runs)
	}
}

// The run start carries the trigger and delivery it came from. The forward link
// (delivery → run) already existed; without this one a person opening the run
// sees only the trigger's SERVICE principal and cannot tell what caused it.
func TestDispatch_StartRunCarriesTheBacklink(t *testing.T) {
	svc, deps, src, delID := dispatchSetup(t, map[string][]string{"action": {"opened"}}, "Review PR #{{ pull_request.number }}")
	adapter, _ := mustRegistry().Get(ProviderGitHub)

	svc.dispatch(context.Background(), src, adapter, arrival{deliveryID: delID, eventType: "pull_request", body: prBody("opened", "main")})
	drainPending(t, svc, deps.repo)

	call, ok := deps.agents.lastCall()
	if !ok {
		t.Fatal("no run was started")
	}
	triggers, _ := deps.repo.ListTriggers(context.Background(), testTenant, "")
	if len(triggers) != 1 {
		t.Fatalf("expected one trigger, got %d", len(triggers))
	}
	if call.triggerID != triggers[0].ID {
		t.Errorf("trigger_id = %q, want %q", call.triggerID, triggers[0].ID)
	}
	if call.triggerName != triggers[0].Name {
		t.Errorf("trigger_name = %q, want %q", call.triggerName, triggers[0].Name)
	}
	if call.deliveryID != delID {
		t.Errorf("delivery_id = %q, want %q", call.deliveryID, delID)
	}
}

// A captured, correctly-signed body replays forever if the only dedup key is a
// header the sender controls. The body is the part they cannot rewrite.
func TestIngest_RefusesAReplayedBodyUnderAFreshDeliveryID(t *testing.T) {
	svc, deps := newTestService(t)
	src := mustSource(t, deps.sources)
	body := prBody("opened", "main")

	h := sign(src.Secret, body)
	if out, err := svc.Ingest(context.Background(), src.ID, h, body); err != nil || out != IngestAccepted {
		t.Fatalf("first delivery = %v, %v; want accepted", out, err)
	}

	// Same bytes, same signature, a delivery id of the sender's choosing.
	replay := sign(src.Secret, body)
	replay.Set("X-GitHub-Delivery", "a-fresh-id")
	if _, err := svc.Ingest(context.Background(), src.ID, replay, body); !errors.Is(err, ErrReplayedPayload) {
		t.Fatalf("replay = %v, want ErrReplayedPayload", err)
	}
}

// A provider retrying an unacknowledged delivery re-sends the same body under
// the same id. That is an ordinary duplicate, and the replay window must not
// turn it into a refusal.
func TestIngest_AGenuineRetryIsStillADuplicateNotAReplay(t *testing.T) {
	svc, deps := newTestService(t)
	src := mustSource(t, deps.sources)
	body := prBody("opened", "main")
	h := sign(src.Secret, body)

	if _, err := svc.Ingest(context.Background(), src.ID, h, body); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	out, err := svc.Ingest(context.Background(), src.ID, h, body)
	if err != nil {
		t.Fatalf("retry = %v, want it handled as a duplicate", err)
	}
	if out != IngestDuplicate {
		t.Errorf("retry outcome = %v, want IngestDuplicate", out)
	}
}

// A rejection count is only readable next to the number of requests that got
// through: five rejections against a million is noise, five against five is a
// signing secret that is simply wrong.
func TestIngest_CountsWhatGotThroughAsWellAsWhatDidNot(t *testing.T) {
	svc, deps := newTestService(t)
	src := mustSource(t, deps.sources)
	body := prBody("opened", "main")

	if _, err := svc.Ingest(context.Background(), src.ID, sign(src.Secret, body), body); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	bad := sign("not-the-secret", body)
	if _, err := svc.Ingest(context.Background(), src.ID, bad, body); !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("bad signature = %v, want ErrSignatureInvalid", err)
	}

	stored, err := deps.sources.GetSource(context.Background(), testTenant, src.ID)
	if err != nil {
		t.Fatalf("GetSource: %v", err)
	}
	if stored.AcceptedCount != 1 || stored.RejectionCount != 1 {
		t.Errorf("accepted = %d, rejected = %d; want 1 and 1", stored.AcceptedCount, stored.RejectionCount)
	}
}

// A grant minted moments ago is not yet visible to orbit-agents; the fire that
// follows must not fail for that reason alone.
func TestStartRun_RetriesWhileAYoungGrantPropagates(t *testing.T) {
	svc, deps, src, delID := dispatchSetup(t, map[string][]string{"action": {"opened"}}, "p")
	deps.agents.statusOnce = http.StatusForbidden // one refusal, as an unpolled bundle gives
	adapter, _ := mustRegistry().Get(ProviderGitHub)

	svc.dispatch(context.Background(), src, adapter, arrival{deliveryID: delID, eventType: "pull_request", body: prBody("opened", "main")})
	drainPending(t, svc, deps.repo)

	if got := deps.agents.callCount(); got < 2 {
		t.Fatalf("call count = %d, want a retry after the refusal", got)
	}
	if got := deps.repo.deliveryByID(delID).Status; got != StatusDispatched {
		t.Errorf("status = %q, want dispatched once the grant landed", got)
	}
}

// A refusal on a trigger old enough to have propagated is a real denial: it
// must fail at once rather than hold a queue slot for the whole budget.
func TestStartRun_DoesNotRetryAnEstablishedTrigger(t *testing.T) {
	svc, deps, src, delID := dispatchSetup(t, map[string][]string{"action": {"opened"}}, "p")
	// Age every trigger past the propagation window so a refusal reads as policy.
	deps.repo.mu.Lock()
	for _, tr := range deps.repo.triggers {
		tr.CreatedAt = time.Now().Add(-2 * time.Hour)
	}
	deps.repo.mu.Unlock()
	deps.agents.status = http.StatusForbidden
	adapter, _ := mustRegistry().Get(ProviderGitHub)

	start := time.Now()
	svc.dispatch(context.Background(), src, adapter, arrival{deliveryID: delID, eventType: "pull_request", body: prBody("opened", "main")})
	drainPending(t, svc, deps.repo)

	if elapsed := time.Since(start); elapsed >= agentTypeRetryBudget {
		t.Errorf("established refusal took %s, want an immediate failure", elapsed)
	}
	if got := deps.agents.callCount(); got != 1 {
		t.Errorf("call count = %d, want exactly one attempt", got)
	}
}

// The fire is enqueued before Ingest acknowledges it, not in a detached
// goroutine afterwards: returning 202 while the pending row was still being
// written lost the fire if the pod died in that window, and a provider that
// saw the 2xx would never retry. This asserts the pending row exists the
// instant Ingest returns, with no waiting.
func TestIngest_EnqueuesBeforeAcknowledging(t *testing.T) {
	svc, deps := newTestService(t)
	src := mustSource(t, deps.sources)
	trig := mustWebhookTrigger(t, deps.triggers, src.ID, "reviewer", true)

	body := prBody("opened", "main")
	outcome, err := svc.Ingest(context.Background(), src.ID, sign(src.Secret, body), body)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if outcome != IngestAccepted {
		t.Fatalf("outcome = %v, want IngestAccepted", outcome)
	}

	// No waitFor: the enqueue is synchronous, so the durable pending row must
	// already be there. Were it still deferred to a goroutine, this would be
	// flaky — which is exactly the window a crash used to fall into.
	if n := len(deps.repo.pendingFor(trig.ID)); n != 1 {
		t.Errorf("pending rows the instant Ingest returned = %d, want 1: the fire must be durable before the 202", n)
	}
}
