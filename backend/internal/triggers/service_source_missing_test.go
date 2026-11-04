package triggers

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// sourcelessTrigger creates a webhook trigger and deletes its source.
func sourcelessTrigger(t *testing.T, deps *testDeps) *Trigger {
	t.Helper()
	src := mustSource(t, deps.sources)
	tr := mustTrigger(t, deps.triggers, src.ID, nil, "review {{ pr.number }}")
	if err := deps.sources.DeleteSource(context.Background(), testTenant, src.ID); err != nil {
		t.Fatalf("DeleteSource: %v", err)
	}
	return tr
}

func TestUpdateTrigger_SourceMissing_RefusesEdits(t *testing.T) {
	_, deps := newTestService(t)
	tr := sourcelessTrigger(t, deps)

	for name, req := range map[string]UpdateTriggerRequest{
		"rename":         {Name: new("renamed")},
		"prompt":         {PromptTemplate: new("other")},
		"enable":         {Enabled: new(true)},
		"disable+rename": {Enabled: new(false), Name: new("renamed")},
	} {
		if _, err := deps.triggers.UpdateTrigger(context.Background(), testTenant, tr.ID, nil, req); !errors.Is(err, ErrValidation) {
			t.Errorf("%s: err = %v, want ErrValidation", name, err)
		}
	}
}

func TestUpdateTrigger_SourceMissing_AllowsDisable(t *testing.T) {
	_, deps := newTestService(t)
	tr := sourcelessTrigger(t, deps)

	got, err := deps.triggers.UpdateTrigger(context.Background(), testTenant, tr.ID, nil, UpdateTriggerRequest{Enabled: new(false)})
	if err != nil {
		t.Fatalf("disable: %v", err)
	}
	if got.Enabled {
		t.Error("trigger should be disabled")
	}
}

func TestDeleteTrigger_SourceMissing(t *testing.T) {
	_, deps := newTestService(t)
	tr := sourcelessTrigger(t, deps)

	if err := deps.triggers.DeleteTrigger(context.Background(), testTenant, tr.ID); err != nil {
		t.Fatalf("DeleteTrigger: %v", err)
	}
	if _, err := deps.triggers.GetTrigger(context.Background(), testTenant, tr.ID); !errors.Is(err, ErrTriggerNotFound) {
		t.Errorf("GetTrigger after delete = %v, want ErrTriggerNotFound", err)
	}
}

// A fire queued before the source was deleted is dropped, not started.
func TestDispatchPending_SourceMissing_Drops(t *testing.T) {
	svc, deps := newTestService(t)
	tr := sourcelessTrigger(t, deps)

	svc.DispatchPending(context.Background(), PendingDelivery{
		ID: "p-1", TenantID: testTenant, TriggerID: tr.ID, DeliveryID: "d-1",
	})

	runs := deps.repo.deliveryRunsFor("d-1")
	if len(runs) != 1 || runs[0].Status != RunStatusDropped {
		t.Fatalf("outcomes = %+v, want one dropped", runs)
	}
	if runs[0].Error == nil || !strings.Contains(*runs[0].Error, "source was deleted") {
		t.Errorf("error = %v, want the source-deleted reason", runs[0].Error)
	}
}

func TestUpdateTrigger_SourceMissing_DisablesRoutedTrigger(t *testing.T) {
	_, deps, _, src := routedHarness(t)
	tr := mustRouted(t, deps, src.ID, nil)
	if err := deps.sources.DeleteSource(context.Background(), testTenant, src.ID); err != nil {
		t.Fatalf("DeleteSource: %v", err)
	}

	got, err := deps.triggers.UpdateTrigger(context.Background(), testTenant, tr.ID, routingClaims, UpdateTriggerRequest{Enabled: new(false)})
	if err != nil {
		t.Fatalf("disable: %v", err)
	}
	if got.Enabled {
		t.Error("routed trigger should be disabled")
	}
}
