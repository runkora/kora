package triggers

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// A fire that was due while the platform was down previously left nothing at
// all, so "did my nightly job run?" was unanswerable after an outage. It is now
// recorded as missed, without running.
func TestRecordMissedTick_RecordsWithoutRunning(t *testing.T) {
	svc, deps := newTestService(t)
	trig := Trigger{ID: "trig-sched", TenantID: testTenant, Kind: KindScheduled}
	due := time.Now().UTC().Add(-30 * time.Minute).Truncate(time.Minute)

	svc.RecordMissedTick(context.Background(), trig, due)

	all := deps.repo.allDeliveries()
	if len(all) != 1 {
		t.Fatalf("want one delivery recorded, got %d", len(all))
	}
	d := all[0]
	if d.Status != StatusMissed {
		t.Errorf("status = %q, want missed", d.Status)
	}
	if d.Reason == nil || *d.Reason == "" {
		t.Error("a missed fire should say why it was skipped")
	}
	if deps.agents.callCount() != 0 {
		t.Error("recording a missed fire must not start a run")
	}
}

// The same unique key that stops replicas double-firing stops a missed record
// from racing a real fire for the same minute.
func TestRecordMissedTick_DoesNotOverwriteARealFire(t *testing.T) {
	svc, deps := newTestService(t)
	trig := Trigger{ID: "trig-sched", TenantID: testTenant, Kind: KindScheduled}
	minute := time.Now().UTC().Add(-5 * time.Minute).Truncate(time.Minute)

	// A real fire claimed this minute first.
	triggerID := trig.ID
	_, _ = deps.repo.InsertScheduledDelivery(context.Background(), &Delivery{
		ID: "real", TenantID: testTenant, TriggerID: &triggerID,
		Kind: KindScheduled, Status: StatusDispatched, ScheduledAt: &minute,
	})

	svc.RecordMissedTick(context.Background(), trig, minute)

	all := deps.repo.allDeliveries()
	if len(all) != 1 {
		t.Fatalf("want the claimed minute to stay a single delivery, got %d", len(all))
	}
	// Seeded directly, not derived: the assertion is that RecordMissedTick left
	// the claimed row alone rather than overwriting it with a missed tick.
	if all[0].Status != StatusDispatched {
		t.Errorf("status = %q, want the real fire preserved", all[0].Status)
	}
}

// ─── Phase 3: admission control ──────────────────────────────────────────────

// A scheduled tick against an auto-paused schedule is recorded as paused rather
// than going silent, which is how "why did my nightly job stop?" gets an answer.
func TestTickScheduled_AutoPaused_RecordsPaused(t *testing.T) {
	svc, deps := newTestService(t)
	now := time.Now().UTC().Truncate(time.Minute)
	trig := &Trigger{
		ID: "sched-paused", TenantID: testTenant, Kind: KindScheduled,
		PrincipalID: "trigger:sched-paused", AgentTypeID: "at-1",
		Overflow: OverflowReplace, MaxParallel: 1, MaxQueueDepth: 8,
		AutoPausedAt: &now,
	}
	if err := deps.repo.CreateTrigger(context.Background(), trig); err != nil {
		t.Fatalf("CreateTrigger: %v", err)
	}

	svc.TickScheduled(context.Background(), ScheduledTick{Trigger: *trig, ScheduledAt: now})

	d := deps.repo.lastDelivery()
	runs := deps.repo.deliveryRunsFor(d.ID)
	if len(runs) != 1 || runs[0].Status != RunStatusPaused {
		t.Fatalf("outcomes = %+v, want one paused", runs)
	}
	if deps.agents.callCount() != 0 {
		t.Error("a paused schedule must not start a run")
	}
}

// Firing an auto-paused trigger is refused, and the refusal is recorded against
// the trigger — the fire happened, and an admin needs to see it did.
func TestFireTrigger_AutoPaused_RecordsPausedAndRefuses(t *testing.T) {
	svc, deps := newTestService(t)
	trig := mustManualTrigger(t, deps.triggers)
	if err := deps.repo.AutoPause(context.Background(), trig.ID); err != nil {
		t.Fatalf("AutoPause: %v", err)
	}

	_, err := svc.FireTrigger(context.Background(), testTenant, trig.ID, "user:1",
		FireTriggerRequest{Variables: map[string]string{"env": "prod"}})
	if !errors.Is(err, ErrTriggerAutoPaused) {
		t.Fatalf("err = %v, want ErrTriggerAutoPaused", err)
	}

	d := deps.repo.lastDelivery()
	if d == nil {
		t.Fatal("an auto-paused fire must still be recorded")
	}
	runs := deps.repo.deliveryRunsFor(d.ID)
	if len(runs) != 1 || runs[0].Status != RunStatusPaused {
		t.Errorf("outcomes = %+v, want one paused", runs)
	}
}

// A manually disabled trigger is refused with nothing recorded, and keeps its
// own error: the fix is a person turning it back on, not fixing failures.
func TestFireTrigger_ManuallyDisabled_RecordsNothing(t *testing.T) {
	svc, deps := newTestService(t)
	trig := mustManualTrigger(t, deps.triggers)
	enabled := false
	if _, err := deps.triggers.UpdateTrigger(context.Background(), testTenant, trig.ID, nil, UpdateTriggerRequest{Enabled: &enabled}); err != nil {
		t.Fatalf("UpdateTrigger: %v", err)
	}

	_, err := svc.FireTrigger(context.Background(), testTenant, trig.ID, "user:1",
		FireTriggerRequest{Variables: map[string]string{"env": "prod"}})
	if !errors.Is(err, ErrTriggerDisabled) {
		t.Fatalf("err = %v, want ErrTriggerDisabled", err)
	}
	if len(deps.repo.allDeliveries()) != 0 {
		t.Error("a manually disabled trigger must record nothing")
	}
}

// ─── Redelivery ──────────────────────────────────────────────────────────────

// A webhook replay is a new delivery naming the original, matched against the
// triggers as they are configured now.
func TestRedeliver_Webhook_ReplaysAgainstCurrentTriggers(t *testing.T) {
	svc, deps := newTestService(t)
	src := mustSource(t, deps.sources)
	body := prBody("opened", "main")
	if _, err := svc.Ingest(context.Background(), src.ID, sign(src.Secret, body), body); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	// Nothing listened for it yet, which is why it is worth replaying later. The
	// re-derive is the signal the fan-out finished.
	waitFor(t, deps.repo.anyDerived)
	original := deps.repo.lastDelivery()

	// The trigger that was missing when the event first arrived now exists —
	// the usual reason someone replays a delivery.
	trig := mustWebhookTrigger(t, deps.triggers, src.ID, "added-later", true)

	resp, err := svc.Redeliver(context.Background(), testTenant, original.ID, "user:1")
	if err != nil {
		t.Fatalf("Redeliver: %v", err)
	}
	if resp.RedeliveryOf != original.ID {
		t.Errorf("redelivery_of = %q, want %q", resp.RedeliveryOf, original.ID)
	}

	waitFor(t, func() bool { return len(deps.repo.pendingFor(trig.ID)) == 1 })
	drainPending(t, svc, deps.repo)

	if deps.agents.callCount() != 1 {
		t.Fatalf("replay should have started the newly added trigger, got %d runs", deps.agents.callCount())
	}
	replay := deps.repo.deliveryByID(resp.DeliveryID)
	if replay.RedeliveryOf == nil || *replay.RedeliveryOf != original.ID {
		t.Errorf("replay redelivery_of = %v, want %q", replay.RedeliveryOf, original.ID)
	}
	if replay.ProviderDeliveryID != original.ProviderDeliveryID {
		t.Error("a replay carries the original's provider delivery id")
	}
	if got := deps.repo.outcomeStatuses(original.ID); len(got) != 0 {
		t.Errorf("original outcomes = %v, want none: the replay is its own delivery", got)
	}
}

// A manual replay re-runs with the values the original was given, validated
// against the declaration as it stands now.
func TestRedeliver_Manual_ReusesOriginalVariables(t *testing.T) {
	svc, deps := newTestService(t)
	trig := mustManualTrigger(t, deps.triggers)

	fired, err := svc.FireTrigger(context.Background(), testTenant, trig.ID, "user:1",
		FireTriggerRequest{Variables: map[string]string{"env": "prod"}})
	if err != nil {
		t.Fatalf("FireTrigger: %v", err)
	}
	drainPending(t, svc, deps.repo)

	resp, err := svc.Redeliver(context.Background(), testTenant, fired.DeliveryID, "user:2")
	if err != nil {
		t.Fatalf("Redeliver: %v", err)
	}
	drainPending(t, svc, deps.repo)

	if deps.agents.callCount() != 2 {
		t.Fatalf("run starts = %d, want the replay to have started one", deps.agents.callCount())
	}
	call, _ := deps.agents.lastCall()
	if !strings.Contains(call.initialMessage, "deploy prod") {
		t.Errorf("replay prompt = %q, want the original variables", call.initialMessage)
	}
	replay := deps.repo.deliveryByID(resp.DeliveryID)
	if replay.RedeliveryOf == nil || *replay.RedeliveryOf != fired.DeliveryID {
		t.Errorf("replay redelivery_of = %v, want %q", replay.RedeliveryOf, fired.DeliveryID)
	}
	if replay.FiredBy != "user:2" {
		t.Errorf("fired_by = %q, want the caller who asked for the replay", replay.FiredBy)
	}
}

// A declaration that changed since the original fire is the current contract.
func TestRedeliver_Manual_RevalidatesAgainstCurrentInputs(t *testing.T) {
	svc, deps := newTestService(t)
	trig := mustManualTrigger(t, deps.triggers)
	fired, err := svc.FireTrigger(context.Background(), testTenant, trig.ID, "user:1",
		FireTriggerRequest{Variables: map[string]string{"env": "prod"}})
	if err != nil {
		t.Fatalf("FireTrigger: %v", err)
	}

	// A new required input the original fire could not have supplied. Written
	// through the repository: UpdateTrigger does not yet apply manual_config
	// (a separate gap), and this test is about the replay path, not that one.
	stored, _ := deps.repo.GetTrigger(context.Background(), testTenant, trig.ID)
	stored.ManualConfig = &ManualConfig{Inputs: []InputField{
		{Name: "env", Type: InputTypeText, Required: true},
		{Name: "ticket", Type: InputTypeText, Required: true},
	}}
	if err := deps.repo.UpdateTrigger(context.Background(), stored, false); err != nil {
		t.Fatalf("UpdateTrigger: %v", err)
	}

	_, err = svc.Redeliver(context.Background(), testTenant, fired.DeliveryID, "user:2")
	if !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("err = %v, want an input validation failure", err)
	}
	if n := len(deps.repo.allDeliveries()); n != 1 {
		t.Errorf("a refused replay must record nothing, got %d deliveries", n)
	}
}

// An auto-paused trigger is replayable — fixing the cause and replaying is the
// point — and admission records whatever it then decides.
func TestRedeliver_Manual_AutoPausedRecordsPaused(t *testing.T) {
	svc, deps := newTestService(t)
	trig := mustManualTrigger(t, deps.triggers)
	fired, err := svc.FireTrigger(context.Background(), testTenant, trig.ID, "user:1",
		FireTriggerRequest{Variables: map[string]string{"env": "prod"}})
	if err != nil {
		t.Fatalf("FireTrigger: %v", err)
	}
	if err := deps.repo.AutoPause(context.Background(), trig.ID); err != nil {
		t.Fatalf("AutoPause: %v", err)
	}

	resp, err := svc.Redeliver(context.Background(), testTenant, fired.DeliveryID, "user:2")
	if err != nil {
		t.Fatalf("Redeliver: %v", err)
	}
	runs := deps.repo.deliveryRunsFor(resp.DeliveryID)
	if len(runs) != 1 || runs[0].Status != RunStatusPaused {
		t.Errorf("replay outcomes = %+v, want one paused", runs)
	}
}

func TestRedeliver_Refusals(t *testing.T) {
	t.Run("unknown delivery", func(t *testing.T) {
		svc, _ := newTestService(t)
		if _, err := svc.Redeliver(context.Background(), testTenant, "nope", "user:1"); !errors.Is(err, ErrDeliveryNotFound) {
			t.Errorf("err = %v, want ErrDeliveryNotFound", err)
		}
	})

	t.Run("another tenant's delivery", func(t *testing.T) {
		svc, deps := newTestService(t)
		_, _ = deps.repo.InsertDelivery(context.Background(), &Delivery{
			ID: "other", TenantID: "t-2", Kind: KindManual, Status: StatusDispatched,
		})
		if _, err := svc.Redeliver(context.Background(), testTenant, "other", "user:1"); !errors.Is(err, ErrDeliveryNotFound) {
			t.Errorf("err = %v, want ErrDeliveryNotFound", err)
		}
	})

	t.Run("scheduled tick", func(t *testing.T) {
		svc, deps := newTestService(t)
		at := time.Now().UTC().Truncate(time.Minute)
		tid := "sched-1"
		_, _ = deps.repo.InsertDelivery(context.Background(), &Delivery{
			ID: "tick", TenantID: testTenant, TriggerID: &tid, Kind: KindScheduled,
			Status: StatusDispatched, ScheduledAt: &at,
		})
		if _, err := svc.Redeliver(context.Background(), testTenant, "tick", "user:1"); !errors.Is(err, ErrNotRedeliverable) {
			t.Errorf("err = %v, want ErrNotRedeliverable", err)
		}
	})

	t.Run("manually disabled trigger", func(t *testing.T) {
		svc, deps := newTestService(t)
		trig := mustManualTrigger(t, deps.triggers)
		fired, err := svc.FireTrigger(context.Background(), testTenant, trig.ID, "user:1",
			FireTriggerRequest{Variables: map[string]string{"env": "prod"}})
		if err != nil {
			t.Fatalf("FireTrigger: %v", err)
		}
		enabled := false
		if _, err := deps.triggers.UpdateTrigger(context.Background(), testTenant, trig.ID, nil, UpdateTriggerRequest{Enabled: &enabled}); err != nil {
			t.Fatalf("UpdateTrigger: %v", err)
		}
		if _, err := svc.Redeliver(context.Background(), testTenant, fired.DeliveryID, "user:2"); !errors.Is(err, ErrNotRedeliverable) {
			t.Errorf("err = %v, want ErrNotRedeliverable", err)
		}
		_ = deps
	})

	t.Run("purged payload", func(t *testing.T) {
		svc, deps := newTestService(t)
		src := mustSource(t, deps.sources)
		_, _ = deps.repo.InsertDelivery(context.Background(), &Delivery{
			ID: "purged", TenantID: testTenant, SourceID: src.ID, Kind: KindWebhook,
			ProviderDeliveryID: "gh-1", Status: StatusDispatched, PayloadPurged: true,
		})
		if _, err := svc.Redeliver(context.Background(), testTenant, "purged", "user:1"); !errors.Is(err, ErrNotRedeliverable) {
			t.Errorf("err = %v, want ErrNotRedeliverable", err)
		}
	})
}

// ─── Payload retention ───────────────────────────────────────────────────────

// A tenant who has never chosen inherits the default, and says so: is_default is
// the difference between "we picked 30" and "we picked nothing", which decides
// whether a later change to the platform default moves this tenant.
func TestRetention_DefaultUntilChosen(t *testing.T) {
	svc, _ := newTestService(t)

	got, err := svc.GetRetention(context.Background(), testTenant)
	if err != nil {
		t.Fatalf("GetRetention: %v", err)
	}
	if got.PayloadDays != DefaultPayloadRetentionDays || !got.IsDefault {
		t.Errorf("unset retention = %+v, want the inherited default", got)
	}

	if _, err := svc.SetRetention(context.Background(), testTenant, UpdateRetentionRequest{PayloadDays: 7}); err != nil {
		t.Fatalf("SetRetention: %v", err)
	}

	got, err = svc.GetRetention(context.Background(), testTenant)
	if err != nil {
		t.Fatalf("GetRetention after set: %v", err)
	}
	if got.PayloadDays != 7 || got.IsDefault {
		t.Errorf("chosen retention = %+v, want 7 and not default", got)
	}
}

// A tenant may shorten the window but never extend it: retention is a promise
// about payload data the platform is holding, so the ceiling is the platform's
// to set, not a tenant's to raise.
func TestRetention_Bounds(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	for _, days := range []int{0, -1, DefaultPayloadRetentionDays + 1, 365} {
		if _, err := svc.SetRetention(ctx, testTenant, UpdateRetentionRequest{PayloadDays: days}); !errors.Is(err, ErrValidation) {
			t.Errorf("SetRetention(%d) = %v, want ErrValidation", days, err)
		}
	}
	for _, days := range []int{MinPayloadRetentionDays, 15, DefaultPayloadRetentionDays} {
		if _, err := svc.SetRetention(ctx, testTenant, UpdateRetentionRequest{PayloadDays: days}); err != nil {
			t.Errorf("SetRetention(%d) = %v, want it accepted", days, err)
		}
	}
}

// Choosing the default value explicitly is still a choice, and is recorded as
// one: a tenant who pinned 30 should not silently move when the platform's
// default changes.
func TestRetention_ExplicitDefaultIsNotInherited(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	if _, err := svc.SetRetention(ctx, testTenant, UpdateRetentionRequest{PayloadDays: DefaultPayloadRetentionDays}); err != nil {
		t.Fatalf("SetRetention: %v", err)
	}
	got, err := svc.GetRetention(ctx, testTenant)
	if err != nil {
		t.Fatalf("GetRetention: %v", err)
	}
	if got.IsDefault {
		t.Error("an explicitly chosen 30 must not report as the inherited default")
	}
}

// ─── Event catalog ─────────────────────────────────────────────────────────

// A manual fire is a trigger start too, and carries the same link.
func TestFireTrigger_StartRunCarriesTheBacklink(t *testing.T) {
	svc, deps := newTestService(t)
	trig := mustManualTrigger(t, deps.triggers)

	fired, err := svc.FireTrigger(context.Background(), testTenant, trig.ID, "user:1",
		FireTriggerRequest{Variables: map[string]string{"env": "prod"}})
	if err != nil {
		t.Fatalf("FireTrigger: %v", err)
	}
	drainPending(t, svc, deps.repo)

	call, ok := deps.agents.lastCall()
	if !ok {
		t.Fatal("no run was started")
	}
	if call.triggerID != trig.ID || call.triggerName != trig.Name || call.deliveryID != fired.DeliveryID {
		t.Errorf("backlink = (%q, %q, %q), want (%q, %q, %q)",
			call.triggerID, call.triggerName, call.deliveryID, trig.ID, trig.Name, fired.DeliveryID)
	}
}

// ─── Trigger names on delivery history (US-004 AC4) ──────────────────────────

// The per-trigger fan-out records the trigger's name alongside its id, so a
// delivery stays readable after the trigger is deleted. The complaint this fixes
// is a console showing an orphaned id.
func TestDeliveryRuns_CaptureTriggerNameAndSurviveDeletion(t *testing.T) {
	svc, deps := newTestService(t)
	src := mustSource(t, deps.sources)
	trig := mustWebhookTrigger(t, deps.triggers, src.ID, "PR reviewer", true)
	ctx := context.Background()

	body := prBody("opened", "main")
	if _, err := svc.Ingest(ctx, src.ID, sign(src.Secret, body), body); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	waitFor(t, func() bool { return len(deps.repo.pendingFor(trig.ID)) == 1 })
	drainPending(t, svc, deps.repo)

	delivery := deps.repo.lastDelivery()
	runs := deps.repo.deliveryRunsFor(delivery.ID)
	if len(runs) != 1 {
		t.Fatalf("fan-out = %+v, want one outcome", runs)
	}
	if runs[0].TriggerName != "PR reviewer" {
		t.Errorf("trigger_name = %q, want it captured at write time", runs[0].TriggerName)
	}

	// Delete the trigger: the history must still name it. A join would return
	// nothing here, which is the whole reason the name is captured.
	if err := deps.triggers.DeleteTrigger(ctx, testTenant, trig.ID); err != nil {
		t.Fatalf("DeleteTrigger: %v", err)
	}
	listed, err := svc.ListDeliveries(ctx, testTenant, DeliveryFilter{SourceID: src.ID})
	if err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	var found bool
	for _, d := range listed {
		for _, r := range d.Runs {
			if r.TriggerID == trig.ID {
				found = true
				if r.TriggerName != "PR reviewer" {
					t.Errorf("after deletion trigger_name = %q, want the captured name", r.TriggerName)
				}
			}
		}
	}
	if !found {
		t.Error("the deleted trigger's outcome vanished from delivery history")
	}
}

// Deliveries that name a single trigger (manual, scheduled) carry the name too:
// the delivery list shows it without a per-row lookup that would fail anyway
// once the trigger is gone.
func TestDelivery_CapturesTriggerName(t *testing.T) {
	svc, deps := newTestService(t)
	trig := mustManualTrigger(t, deps.triggers)

	if _, err := svc.FireTrigger(context.Background(), testTenant, trig.ID, "user:1",
		FireTriggerRequest{Variables: map[string]string{"env": "prod"}}); err != nil {
		t.Fatalf("FireTrigger: %v", err)
	}

	d := deps.repo.lastDelivery()
	if d.TriggerName != trig.Name {
		t.Errorf("delivery trigger_name = %q, want %q", d.TriggerName, trig.Name)
	}
}

// A scheduled fire that was due during an outage names its trigger too: "which
// job missed its run?" is unanswerable from an id alone.
func TestRecordMissedTick_CapturesTriggerName(t *testing.T) {
	svc, deps := newTestService(t)
	trig := Trigger{ID: "trig-sched", TenantID: testTenant, Kind: KindScheduled, Name: "Nightly report"}

	svc.RecordMissedTick(context.Background(), trig, time.Now().UTC().Add(-30*time.Minute))

	d := deps.repo.lastDelivery()
	if d == nil || d.TriggerName != "Nightly report" {
		t.Errorf("missed delivery = %+v, want the trigger's name captured", d)
	}
}

// The drawer asks "how many fires are waiting". Waiting means unclaimed: a
// claimed fire is already being dispatched, and counting it would promise work
// that has started.
func TestTrigger_PendingCount(t *testing.T) {
	svc, deps := newTestService(t)
	trig := newAdmissionTrigger(t, deps, OverflowWait, 1, 32)
	ctx := context.Background()

	got, err := deps.triggers.GetTrigger(ctx, testTenant, trig.ID)
	if err != nil {
		t.Fatalf("GetTrigger: %v", err)
	}
	if got.PendingCount != 0 {
		t.Errorf("pending_count = %d, want 0 for an idle trigger", got.PendingCount)
	}

	enqueue(t, svc, deps, trig, 3)

	got, err = deps.triggers.GetTrigger(ctx, testTenant, trig.ID)
	if err != nil {
		t.Fatalf("GetTrigger: %v", err)
	}
	if got.PendingCount != 3 {
		t.Errorf("pending_count = %d, want 3 waiting", got.PendingCount)
	}

	listed, err := deps.triggers.ListTriggers(ctx, testTenant, "", "")
	if err != nil {
		t.Fatalf("ListTriggers: %v", err)
	}
	if len(listed) != 1 || listed[0].PendingCount != 3 {
		t.Errorf("listed pending_count = %+v, want 3 on the list too", listed)
	}

	// Claiming one takes it out of "waiting": it is mid-dispatch, not queued.
	if _, err := deps.repo.ClaimPending(ctx, 1); err != nil {
		t.Fatalf("ClaimPending: %v", err)
	}
	got, err = deps.triggers.GetTrigger(ctx, testTenant, trig.ID)
	if err != nil {
		t.Fatalf("GetTrigger: %v", err)
	}
	if got.PendingCount != 2 {
		t.Errorf("pending_count = %d after a claim, want 2 still waiting", got.PendingCount)
	}
}

// ─── Rejected manual input is history (US-005) ───────────────────────────────

// A fire the platform refused is still a fire someone attempted. Recorded with
// the per-field reason, so "why did nothing happen?" has an answer.
func TestFireTrigger_InvalidInput_RecordedAsFailedDelivery(t *testing.T) {
	svc, deps := newTestService(t)
	trig := mustManualTrigger(t, deps.triggers)

	_, err := svc.FireTrigger(context.Background(), testTenant, trig.ID, "user:1",
		FireTriggerRequest{Variables: map[string]string{}})
	if !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("err = %v, want an input validation failure", err)
	}

	// The caller still gets per-field detail: that is what they can act on.
	var ive *InputValidationError
	if !errors.As(err, &ive) || ive.Problems["env"] != "required" {
		t.Errorf("problems = %+v, want env: required", ive)
	}

	all := deps.repo.allDeliveries()
	if len(all) != 1 {
		t.Fatalf("deliveries = %d, want the rejected fire recorded", len(all))
	}
	d := all[0]
	if d.Status != StatusFailed {
		t.Errorf("status = %q, want %q", d.Status, StatusFailed)
	}
	if d.Reason == nil || *d.Reason != "invalid input: env: required" {
		t.Errorf("reason = %v, want the per-field summary", d.Reason)
	}
	if d.FiredBy != "user:1" {
		t.Errorf("fired_by = %q, want the caller who attempted it", d.FiredBy)
	}

	// Recorded, never admitted: a refused fire must not reach the queue.
	if pending := deps.repo.pendingFor(trig.ID); len(pending) != 0 {
		t.Errorf("a rejected fire queued %d pending rows, want none", len(pending))
	}
	if len(deps.repo.deliveryRunsFor(d.ID)) != 0 {
		t.Error("a rejected fire must record no per-trigger outcome")
	}
	drainPending(t, svc, deps.repo)
	if deps.agents.callCount() != 0 {
		t.Error("a rejected fire must not start a run")
	}
}

// Several bad fields produce one compact, stable line, in field order.
func TestInputProblemSummary_IsCompactAndSorted(t *testing.T) {
	t.Parallel()
	got := inputProblemSummary(map[string]string{
		"row_limit": "must be a number",
		"env":       "required",
	})
	if got != "invalid input: env: required; row_limit: must be a number" {
		t.Errorf("summary = %q", got)
	}
}

// Replaying starts real work under the trigger's own authority from a body the
// platform does not re-verify. Unbounded, the endpoint is a run generator.
func TestRedeliver_IsBoundedPerOriginal(t *testing.T) {
	svc, deps := newTestService(t)
	src := mustSource(t, deps.sources)
	body := prBody("opened", "main")
	if _, err := svc.Ingest(context.Background(), src.ID, sign(src.Secret, body), body); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	original := deps.repo.lastDelivery()

	for i := range maxRedeliveriesPerOriginal {
		if _, err := svc.Redeliver(context.Background(), testTenant, original.ID, "user:1"); err != nil {
			t.Fatalf("replay %d: %v", i, err)
		}
	}
	if _, err := svc.Redeliver(context.Background(), testTenant, original.ID, "user:1"); !errors.Is(err, ErrNotRedeliverable) {
		t.Errorf("replay past the limit = %v, want ErrNotRedeliverable", err)
	}
}

// Replaying a replay must not reset the budget: the count is against the lineage
// root, so a caller cannot walk down the chain to get unlimited replays of one
// event.
func TestRedeliver_BudgetCountsAgainstLineageRoot(t *testing.T) {
	ctx := context.Background()
	svc, deps := newTestService(t)
	src := mustSource(t, deps.sources)
	body := prBody("opened", "main")
	if _, err := svc.Ingest(ctx, src.ID, sign(src.Secret, body), body); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	original := deps.repo.lastDelivery()

	// The first replay produces a child, which must be pinned to the root, not
	// to itself.
	resp, err := svc.Redeliver(ctx, testTenant, original.ID, "user:1")
	if err != nil {
		t.Fatalf("first replay: %v", err)
	}
	child, err := deps.repo.GetDelivery(ctx, testTenant, resp.DeliveryID)
	if err != nil {
		t.Fatalf("get child: %v", err)
	}
	if child.RedeliveryOf == nil || *child.RedeliveryOf != original.ID {
		t.Fatalf("a replay must pin to the lineage root %s, got %v", original.ID, child.RedeliveryOf)
	}

	// Exhaust the rest of the budget against the original.
	for i := 1; i < maxRedeliveriesPerOriginal; i++ {
		if _, err := svc.Redeliver(ctx, testTenant, original.ID, "user:1"); err != nil {
			t.Fatalf("replay %d: %v", i, err)
		}
	}

	// Replaying the child (a replay) must still be refused: it shares the root's
	// budget rather than starting a fresh chain with a count of zero.
	if _, err := svc.Redeliver(ctx, testTenant, child.ID, "user:1"); !errors.Is(err, ErrNotRedeliverable) {
		t.Errorf("replaying a replay must count against the root, got %v", err)
	}
}

// Rotating a signing secret is what an operator does when the old one may be in
// someone else's hands. Replaying a payload from before the rotation would
// re-run, under the platform's authority, exactly what the rotation was meant to
// render unusable.
func TestRedeliver_RefusesAPayloadOlderThanTheCurrentSecret(t *testing.T) {
	svc, deps := newTestService(t)
	src := mustSource(t, deps.sources)
	body := prBody("opened", "main")
	if _, err := svc.Ingest(context.Background(), src.ID, sign(src.Secret, body), body); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	original := deps.repo.lastDelivery()

	if _, err := deps.sources.UpdateSource(context.Background(), testTenant, src.ID, UpdateSourceRequest{RotateSecret: true}); err != nil {
		t.Fatalf("rotate: %v", err)
	}

	if _, err := svc.Redeliver(context.Background(), testTenant, original.ID, "user:1"); !errors.Is(err, ErrNotRedeliverable) {
		t.Errorf("replay after rotation = %v, want ErrNotRedeliverable", err)
	}
}

// A database fault during admission is a platform failure, not backpressure.
// FireTrigger must surface it as a 5xx-mapping error, never ErrTriggerAtCapacity
// (429 "busy, retry") — retrying a fire that failed because Postgres was down
// just fails again.
func TestFireTrigger_AdmissionInfraError_IsNotReportedAsCapacity(t *testing.T) {
	svc, deps := newTestService(t)
	trig := mustManualTrigger(t, deps.triggers)
	deps.repo.admissionErr = errors.New("connection refused")

	_, err := svc.FireTrigger(context.Background(), testTenant, trig.ID, "user:1",
		FireTriggerRequest{Variables: map[string]string{"env": "prod"}})
	if err == nil {
		t.Fatal("an admission infrastructure error must surface as an error")
	}
	if errors.Is(err, ErrTriggerAtCapacity) || errors.Is(err, ErrTriggerAutoPaused) {
		t.Errorf("an infra error must not read as a capacity/pause refusal, got %v", err)
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("the underlying fault should be wrapped, got %v", err)
	}
}

// The delivery list carries the coalescing key each trigger rendered, and the
// pointer on a superseded outcome, so history explains a fire that did not run.
func TestListDeliveries_ReturnsCoalesceKeysAndSupersededBy(t *testing.T) {
	svc, deps := newTestService(t)
	src := mustSource(t, deps.sources)
	ctx := context.Background()

	var ids []string
	for _, pid := range []string{"d-1", "d-2"} {
		d := &Delivery{
			ID: "delivery-" + pid, TenantID: testTenant, SourceID: src.ID, Kind: KindWebhook,
			ProviderDeliveryID: pid, Status: StatusVerified,
		}
		if _, err := deps.repo.InsertDelivery(ctx, d); err != nil {
			t.Fatalf("InsertDelivery: %v", err)
		}
		ids = append(ids, d.ID)
	}
	if err := deps.repo.InsertDeliveryKey(ctx, &DeliveryKey{DeliveryID: ids[0], TenantID: testTenant, TriggerID: "t-1", Key: "org/repo#7"}); err != nil {
		t.Fatalf("InsertDeliveryKey: %v", err)
	}
	if err := deps.repo.InsertDeliveryRun(ctx, &DeliveryRun{
		ID: "r-1", TenantID: testTenant, DeliveryID: ids[0], TriggerID: "t-1",
		Status: RunStatusSuperseded, SupersededBy: &ids[1],
	}); err != nil {
		t.Fatalf("InsertDeliveryRun: %v", err)
	}

	listed, err := svc.ListDeliveries(ctx, testTenant, DeliveryFilter{SourceID: src.ID})
	if err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	byID := map[string]Delivery{}
	for _, d := range listed {
		byID[d.ID] = d
	}
	first := byID[ids[0]]
	if len(first.CoalesceKeys) != 1 || first.CoalesceKeys[0].Key != "org/repo#7" || first.CoalesceKeys[0].TriggerID != "t-1" {
		t.Errorf("coalesce keys = %+v, want the rendered key for t-1", first.CoalesceKeys)
	}
	if len(first.Runs) != 1 || first.Runs[0].SupersededBy == nil || *first.Runs[0].SupersededBy != ids[1] {
		t.Errorf("runs = %+v, want a superseded outcome pointing at %s", first.Runs, ids[1])
	}
	if got := byID[ids[1]].CoalesceKeys; len(got) != 0 {
		t.Errorf("the replacing delivery has keys %+v, want none recorded", got)
	}
}

// A replay starts runs under the trigger's principal, so the caller needs run authority on every agent it would start.
func TestRedeliver_NeedsRunAccess(t *testing.T) {
	for _, kind := range []string{KindManual, KindWebhook} {
		for _, allow := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/allow=%v", kind, allow), func(t *testing.T) {
				svc, deps := newTestService(t)
				var originalID string
				if kind == KindManual {
					trig := mustManualTrigger(t, deps.triggers)
					fired, err := svc.FireTrigger(context.Background(), testTenant, trig.ID, "user:1",
						FireTriggerRequest{Variables: map[string]string{"env": "prod"}})
					if err != nil {
						t.Fatalf("FireTrigger: %v", err)
					}
					originalID = fired.DeliveryID
				} else {
					src := mustSource(t, deps.sources)
					body := prBody("opened", "main")
					if _, err := svc.Ingest(context.Background(), src.ID, sign(src.Secret, body), body); err != nil {
						t.Fatalf("Ingest: %v", err)
					}
					waitFor(t, deps.repo.anyDerived)
					originalID = deps.repo.lastDelivery().ID
					mustWebhookTrigger(t, deps.triggers, src.ID, "added-later", true)
				}
				deps.agents.atNoRun = !allow
				deps.agents.callerReadIDs()
				before := len(deps.repo.allDeliveries())

				_, err := svc.Redeliver(context.Background(), testTenant, originalID, "user:2")

				if got := deps.agents.callerReadIDs(); !slices.Equal(got, []string{"at-1"}) {
					t.Errorf("caller reads = %v, want one read of at-1", got)
				}
				if allow {
					if err != nil {
						t.Fatalf("Redeliver: %v", err)
					}
					return
				}
				if !errors.Is(err, ErrBindForbidden) {
					t.Fatalf("got %v, want ErrBindForbidden", err)
				}
				if after := len(deps.repo.allDeliveries()); after != before {
					t.Errorf("a refused replay must record nothing (deliveries %d → %d)", before, after)
				}
			})
		}
	}
}
