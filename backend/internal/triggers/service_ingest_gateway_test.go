package triggers

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"sync"
	"testing"
)

// gatewayCapAdapter reads the delivery id and event type from the body, as a
// Gateway adapter does: there are no headers.
type gatewayCapAdapter struct{ capAdapter }

func (gatewayCapAdapter) DeliveryID(_ http.Header, body []byte) string {
	return resolvePath(decodeBody(body), "id")
}

func (gatewayCapAdapter) EventType(_ http.Header, body []byte) string {
	return resolvePath(decodeBody(body), "type")
}

func newGatewayService(t *testing.T) (*Service, *testDeps, WebhookSource) {
	t.Helper()
	svc, deps := newTestService(t)
	base := mustRegistry()
	reg := &Registry{adapters: maps.Clone(base.adapters), catalogs: maps.Clone(base.catalogs)}
	if err := reg.register(gatewayCapAdapter{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	svc.registry, deps.sources.registry, deps.triggers.registry = reg, reg, reg
	return svc, deps, capSource(t, deps)
}

const gatewayBody = `{"id":"m1","type":"mention","user":"u1","channel":"C1","ts":"100","text":"hi"}`

func TestIngestGateway_TwoSessionsOneDeliveryOneRun(t *testing.T) {
	svc, deps, src := newGatewayService(t)
	dedupe := false
	tr, err := deps.triggers.CreateTrigger(context.Background(), testTenant, nil, CreateTriggerRequest{
		SourceID: src.ID, Name: "t", AgentTypeID: "at-1", EventType: "mention", PromptTemplate: "{{ text }}",
		AdmissionSettings: AdmissionSettings{DedupeEnabled: &dedupe},
	})
	if err != nil {
		t.Fatalf("CreateTrigger: %v", err)
	}

	// Two replicas' sessions receive the same message at once.
	var wg sync.WaitGroup
	outcomes := make(chan IngestOutcome, 2)
	for range 2 {
		wg.Go(func() {
			res, err := svc.IngestGateway(context.Background(), src.ID, []byte(gatewayBody))
			if err != nil {
				t.Errorf("IngestGateway: %v", err)
			}
			outcomes <- res.Outcome
		})
	}
	wg.Wait()
	close(outcomes)
	var accepted, duplicate int
	for o := range outcomes {
		switch o {
		case IngestAccepted:
			accepted++
		case IngestDuplicate:
			duplicate++
		}
	}
	if accepted != 1 || duplicate != 1 {
		t.Fatalf("outcomes: %d accepted, %d duplicate, want 1 and 1", accepted, duplicate)
	}

	if n := len(deps.repo.allDeliveries()); n != 1 {
		t.Fatalf("delivery rows = %d, want 1: the second session's copy writes none", n)
	}
	waitFor(t, func() bool { return len(deps.repo.pendingFor(tr.ID)) == 1 })
	drainPending(t, svc, deps.repo)
	if n := deps.agents.callCount(); n != 1 {
		t.Errorf("runs started = %d, want 1 even with deduplication off", n)
	}
	if got := deps.repo.sources[src.ID].AcceptedCount; got != 1 {
		t.Errorf("accepted_count = %d, want 1", got)
	}
}

func TestIngestGateway_Refusals(t *testing.T) {
	svc, deps, src := newGatewayService(t)
	ctx := context.Background()

	if _, err := svc.IngestGateway(ctx, src.ID, []byte(`{"type":"mention"}`)); !errors.Is(err, ErrMissingDeliveryID) {
		t.Errorf("no delivery id: err = %v, want ErrMissingDeliveryID", err)
	}
	if _, err := svc.IngestGateway(ctx, "nope", []byte(gatewayBody)); !errors.Is(err, ErrSourceNotFound) {
		t.Errorf("unknown source: err = %v, want ErrSourceNotFound", err)
	}
	if _, err := deps.sources.UpdateSource(ctx, testTenant, src.ID, UpdateSourceRequest{Enabled: new(false)}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.IngestGateway(ctx, src.ID, []byte(gatewayBody)); !errors.Is(err, ErrSourceDisabled) {
		t.Errorf("disabled source: err = %v, want ErrSourceDisabled", err)
	}
	if n := len(deps.repo.allDeliveries()); n != 0 {
		t.Errorf("delivery rows = %d after refusals, want none", n)
	}
}
