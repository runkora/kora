package triggers

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// instrument wires a real instrument set into the service and its admission
// controller, as cmd/kora-triggers does.
func instrument(t *testing.T, svc *Service, deps *testDeps) *sdkmetric.ManualReader {
	t.Helper()
	m, reader := newTestMetrics(t, deps.repo)
	svc.metrics = m
	svc.outcomes.metrics = m
	svc.admission.SetMetrics(m)
	return reader
}

// deliveryCount reads kora_triggers_deliveries_total for one provider/outcome,
// and fails if any series carries a label beyond those two.
func deliveryCount(t *testing.T, reader *sdkmetric.ManualReader, provider, outcome string) int64 {
	t.Helper()
	got, ok := collect(t, reader)["kora.triggers.deliveries"]
	if !ok {
		return 0
	}
	sum, ok := got.Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("deliveries is %T, want a sum", got.Data)
	}
	var total int64
	for _, dp := range sum.DataPoints {
		if dp.Attributes.Len() != 2 {
			t.Errorf("series has %d labels, want only provider and outcome", dp.Attributes.Len())
		}
		p, _ := dp.Attributes.Value(attribute.Key("provider"))
		o, _ := dp.Attributes.Value(attribute.Key("outcome"))
		if p.AsString() == provider && o.AsString() == outcome {
			total += dp.Value
		}
	}
	return total
}

func TestDeliveryMetrics_StartedSkippedAndFailed(t *testing.T) {
	ctx := context.Background()
	svc, deps := newTestService(t)
	reader := instrument(t, svc, deps)

	// started
	trig := newAdmissionTrigger(t, deps, OverflowWait, 4, 32)
	ids := enqueue(t, svc, deps, trig, 1)
	svc.DispatchPending(ctx, PendingDelivery{ID: "p-1", TenantID: testTenant, TriggerID: trig.ID, DeliveryID: ids[0]})
	if got := deliveryCount(t, reader, "generic", "started"); got != 1 {
		t.Errorf("started = %d, want 1", got)
	}

	// skipped: a paused trigger turns the fire away at admission
	paused := newAdmissionTrigger(t, deps, OverflowRefuse, 1, 1)
	now := time.Now()
	paused.AutoPausedAt = &now
	enqueue(t, svc, deps, paused, 1)
	if got := deliveryCount(t, reader, "generic", "skipped"); got != 1 {
		t.Errorf("skipped = %d, want 1", got)
	}

	// failed: the start errors
	failing := newAdmissionTrigger(t, deps, OverflowReplace, 4, 32)
	fids := enqueue(t, svc, deps, failing, 1)
	deps.minter.err = errors.New("mint call: boom")
	fctx, cancel := context.WithTimeout(ctx, 400*time.Millisecond)
	defer cancel()
	svc.DispatchPending(fctx, PendingDelivery{ID: "p-2", TenantID: testTenant, TriggerID: failing.ID, DeliveryID: fids[0]})
	if got := deliveryCount(t, reader, "generic", "failed"); got != 1 {
		t.Errorf("failed = %d, want 1", got)
	}
	if got := deliveryCount(t, reader, "generic", "started"); got != 1 {
		t.Errorf("started after failure = %d, want still 1", got)
	}
}

func TestFailingGauge_RisesOnFailureAndFallsOnSuccess(t *testing.T) {
	ctx := context.Background()
	svc, deps := newTestService(t)
	reader := instrument(t, svc, deps)
	trig := newAdmissionTrigger(t, deps, OverflowWait, 4, 32)

	failing := func() int64 {
		got, ok := collect(t, reader)["kora.triggers.failing"]
		if !ok {
			t.Fatal("kora.triggers.failing not exported")
		}
		g := got.Data.(metricdata.Gauge[int64])
		if len(g.DataPoints) != 1 || g.DataPoints[0].Attributes.Len() != 0 {
			t.Fatalf("failing gauge must be a single unlabelled series, got %+v", g.DataPoints)
		}
		return g.DataPoints[0].Value
	}

	if got := failing(); got != 0 {
		t.Fatalf("failing = %d, want 0", got)
	}
	svc.noteFailure(ctx, trig, errTriggerSideFailure)
	if got := failing(); got != 1 {
		t.Errorf("failing after a failure = %d, want 1", got)
	}

	ids := enqueue(t, svc, deps, trig, 1)
	svc.DispatchPending(ctx, PendingDelivery{ID: "p-1", TenantID: testTenant, TriggerID: trig.ID, DeliveryID: ids[0]})
	if got := failing(); got != 0 {
		t.Errorf("failing after a successful fire = %d, want 0", got)
	}
}
