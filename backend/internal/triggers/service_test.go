package triggers

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
)

const testTenant = "t-1"

func prBody(action, baseRef string) []byte {
	return fmt.Appendf(nil, `{"action":%q,
		"pull_request":{"number":42,"title":"Add widget","html_url":"https://x/pull/42",
			"head":{"ref":"feature"},"base":{"ref":%q},"user":{"login":"alice"}},
		"repository":{"full_name":"acme/widgets"},"sender":{"login":"bob"}}`, action, baseRef)
}

func mustSource(t *testing.T, svc *Service) *SourceResponse {
	t.Helper()
	resp, err := svc.CreateSource(context.Background(), testTenant, CreateSourceRequest{Name: "gh", Provider: ProviderGitHub})
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	return resp
}

func mustTrigger(t *testing.T, svc *Service, sourceID string, cond map[string][]string, prompt string) *Trigger {
	t.Helper()
	tr, err := svc.CreateTrigger(context.Background(), testTenant, CreateTriggerRequest{
		SourceID:       sourceID,
		Name:           "rev",
		AgentTypeID:    "at-1",
		AgentSlug:      "code-reviewer",
		EventType:      "pull_request",
		MatchCondition: cond,
		PromptTemplate: prompt,
	})
	if err != nil {
		t.Fatalf("CreateTrigger: %v", err)
	}
	return tr
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}

// ── Sources ─────────────────────────────────────────────────────────────────

//go:fix inline
func admissionPtr[T any](v T) *T { return new(v) }

func sign(secret string, body []byte) http.Header {
	return http.Header{
		"X-Hub-Signature-256": {githubSig(secret, body)},
		"X-Github-Event":      {"pull_request"},
		"X-Github-Delivery":   {"delivery-1"},
	}
}

func dispatchSetup(t *testing.T, cond map[string][]string, prompt string) (*Service, *testDeps, WebhookSource, string) {
	t.Helper()
	svc, deps := newTestService(t)
	src := mustSource(t, svc)
	mustTrigger(t, svc, src.ID, cond, prompt)
	stored, _ := deps.repo.GetSourceByID(context.Background(), src.ID)
	delID := "deliv-x"
	_, _ = deps.repo.InsertDelivery(context.Background(), &Delivery{
		ID: delID, TenantID: testTenant, SourceID: src.ID, EventType: "pull_request", Status: StatusVerified,
	})
	// CreateTrigger mints a source token to resolve the agent type; clear that so
	// dispatch tests observe only the dispatch-time mint.
	deps.minter.reset()
	return svc, deps, *stored, delID
}

// newAdmissionTrigger stores a trigger with the given admission settings and
// returns it, so admission tests do not depend on the creation path.
func newAdmissionTrigger(t *testing.T, deps *testDeps, overflow string, maxParallel, depth int16) Trigger {
	t.Helper()
	trig := &Trigger{
		ID: "adm-" + overflow, TenantID: testTenant, Kind: KindManual,
		PrincipalID: "trigger:adm", AgentTypeID: "at-1", Enabled: true,
		Overflow: overflow, MaxParallel: maxParallel, MaxQueueDepth: depth,
		AutopauseEnabled: true,
	}
	if err := deps.repo.CreateTrigger(context.Background(), trig); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	return *trig
}

// enqueue records a delivery and admits it, returning the delivery id.
func enqueue(t *testing.T, svc *Service, deps *testDeps, trig Trigger, n int) []string {
	t.Helper()
	var ids []string
	for i := range n {
		id := fmt.Sprintf("d-%s-%d", trig.ID, i)
		tid := trig.ID
		if _, err := deps.repo.InsertDelivery(context.Background(), &Delivery{
			ID: id, TenantID: testTenant, TriggerID: &tid, Kind: trig.Kind, Status: StatusVerified,
		}); err != nil {
			t.Fatalf("insert delivery: %v", err)
		}
		svc.admit(context.Background(), trig, id, nil)
		ids = append(ids, id)
	}
	return ids
}

// mustWebhookTrigger stores a webhook trigger with dedupe on or off, which is
// the setting that decides whether a repeat arrival is suppressed or re-run.
func mustWebhookTrigger(t *testing.T, svc *Service, sourceID, name string, dedupe bool) *Trigger {
	t.Helper()
	tr, err := svc.CreateTrigger(context.Background(), testTenant, CreateTriggerRequest{
		SourceID:       sourceID,
		Name:           name,
		AgentTypeID:    "at-1",
		EventType:      "pull_request",
		PromptTemplate: "review {{ pr.number }}",

		AdmissionSettings: AdmissionSettings{DedupeEnabled: &dedupe},
	})
	if err != nil {
		t.Fatalf("CreateTrigger(%s): %v", name, err)
	}
	return tr
}

// mustManualTrigger stores a manual trigger with one declared input.
func mustManualTrigger(t *testing.T, svc *Service) *Trigger {
	t.Helper()
	tr, err := svc.CreateTrigger(context.Background(), testTenant, CreateTriggerRequest{
		Kind:           KindManual,
		Name:           "deploy",
		AgentTypeID:    "at-1",
		PromptTemplate: "deploy {{ var.env }}",
		ManualConfig:   &ManualConfig{Inputs: []InputField{{Name: "env", Type: InputTypeText, Required: true}}},
	})
	if err != nil {
		t.Fatalf("CreateTrigger(manual): %v", err)
	}
	return tr
}
