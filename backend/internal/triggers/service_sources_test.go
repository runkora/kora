package triggers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"testing"
)

func TestCreateSource_Success(t *testing.T) {
	_, deps := newTestService(t)
	resp := mustSource(t, deps.sources)

	if resp.Secret == "" {
		t.Error("expected one-time secret in response")
	}
	if resp.WebhookPath != "/api/v1/webhooks/"+resp.ID {
		t.Errorf("WebhookPath = %q", resp.WebhookPath)
	}
	if resp.SecretCiphertext != "" {
		t.Error("ciphertext must be redacted from the response")
	}
	// The response is the stored row, not the struct the create was built from,
	// so the timestamps a console renders are the database's.
	if resp.CreatedAt.IsZero() {
		t.Error("CreatedAt should come from the stored row, not the request struct")
	}

	stored := deps.repo.sources[resp.ID]
	if stored == nil {
		t.Fatal("source not persisted")
	}
	if !stored.Enabled {
		t.Error("source should be enabled on create")
	}
	if stored.GrantID == nil || *stored.GrantID == "" {
		t.Error("source should reference a created grant")
	}
	if stored.PrincipalID != "trigger-source:"+resp.ID {
		t.Errorf("PrincipalID = %q", stored.PrincipalID)
	}
	// The stored ciphertext must decrypt back to the one-time secret.
	c, _ := newSecretCipher(testCipherKey)
	dec, err := c.decrypt(stored.SecretCiphertext)
	if err != nil || dec != resp.Secret {
		t.Errorf("stored ciphertext did not round-trip: dec=%q err=%v", dec, err)
	}
}

func TestCreateSource_Validation(t *testing.T) {
	_, deps := newTestService(t)
	if _, err := deps.sources.CreateSource(context.Background(), testTenant, CreateSourceRequest{Provider: ProviderGitHub}); !errors.Is(err, ErrValidation) {
		t.Errorf("empty name: got %v, want ErrValidation", err)
	}
	if _, err := deps.sources.CreateSource(context.Background(), testTenant, CreateSourceRequest{Name: "x", Provider: "gitlab"}); !errors.Is(err, ErrUnknownProvider) {
		t.Errorf("unknown provider: got %v, want ErrUnknownProvider", err)
	}
}

func TestCreateSource_GrantCleanupOnRepoFailure(t *testing.T) {
	_, deps := newTestService(t)
	deps.repo.createSourceErr = errors.New("db down")

	if _, err := deps.sources.CreateSource(context.Background(), testTenant, CreateSourceRequest{Name: "gh", Provider: ProviderGitHub}); err == nil {
		t.Fatal("expected error when persistence fails")
	}
	if deps.policy.deletedCount() != 1 {
		t.Errorf("expected the orphaned grant to be cleaned up, deleted=%d", deps.policy.deletedCount())
	}
}

func TestListSources_RedactsSecret(t *testing.T) {
	_, deps := newTestService(t)
	mustSource(t, deps.sources)
	list, err := deps.sources.ListSources(context.Background(), testTenant)
	if err != nil {
		t.Fatalf("ListSources: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 source, got %d", len(list))
	}
	if list[0].SecretCiphertext != "" {
		t.Error("ListSources must redact the ciphertext")
	}
}

func TestUpdateSource_DisableAndRotate(t *testing.T) {
	_, deps := newTestService(t)
	resp := mustSource(t, deps.sources)

	disabled := false
	if _, err := deps.sources.UpdateSource(context.Background(), testTenant, resp.ID, UpdateSourceRequest{Enabled: &disabled}); err != nil {
		t.Fatalf("UpdateSource disable: %v", err)
	}
	if deps.repo.sources[resp.ID].Enabled {
		t.Error("source should be disabled")
	}

	rot, err := deps.sources.UpdateSource(context.Background(), testTenant, resp.ID, UpdateSourceRequest{RotateSecret: true})
	if err != nil {
		t.Fatalf("UpdateSource rotate: %v", err)
	}
	if rot.Secret == "" || rot.Secret == resp.Secret {
		t.Error("rotate should return a new, different secret")
	}
}

func TestDeleteSource_CleansGrants(t *testing.T) {
	_, deps := newTestService(t)
	resp := mustSource(t, deps.sources)
	mustTrigger(t, deps.triggers, resp.ID, nil, "review")

	if err := deps.sources.DeleteSource(context.Background(), testTenant, resp.ID); err != nil {
		t.Fatalf("DeleteSource: %v", err)
	}
	if _, ok := deps.repo.sources[resp.ID]; ok {
		t.Error("source should be deleted")
	}
	// Source grant + trigger grant both attached to the principal → both deleted.
	if deps.policy.deletedCount() < 1 {
		t.Errorf("expected source principal grants to be revoked, deleted=%d", deps.policy.deletedCount())
	}
}

// ── Triggers ────────────────────────────────────────────────────────────────

// Declared is what the platform models; observed is what this source has
// actually sent. Neither alone answers "why does my trigger never fire".
func TestSourceEventTypes_DeclaredAndObserved(t *testing.T) {
	_, deps := newTestService(t)
	src := mustSource(t, deps.sources)
	ctx := context.Background()

	// One modeled event and one the adapter does not model, both delivered.
	for i, et := range []string{"pull_request", "release"} {
		_, _ = deps.repo.InsertDelivery(ctx, &Delivery{
			ID: fmt.Sprintf("d-%d", i), TenantID: testTenant, SourceID: src.ID,
			Kind: KindWebhook, ProviderDeliveryID: fmt.Sprintf("gh-%d", i),
			EventType: et, Status: StatusSkipped,
		})
	}
	// An empty event type is not a catalog entry.
	_, _ = deps.repo.InsertDelivery(ctx, &Delivery{
		ID: "d-blank", TenantID: testTenant, SourceID: src.ID, Kind: KindWebhook,
		ProviderDeliveryID: "gh-blank", Status: StatusSkipped,
	})

	got, err := deps.sources.SourceEventTypes(ctx, testTenant, src.ID)
	if err != nil {
		t.Fatalf("SourceEventTypes: %v", err)
	}
	for _, want := range []string{"issue_comment", "pull_request", "push"} {
		if !slices.Contains(got.Declared, want) {
			t.Errorf("declared = %v, want it to include %s from the schema", got.Declared, want)
		}
	}
	if !slices.Equal(got.Associations, []string{"OWNER", "MEMBER", "COLLABORATOR"}) {
		t.Errorf("associations = %v, want github's", got.Associations)
	}
	if !slices.Equal(got.Observed, []string{"pull_request", "release"}) {
		t.Errorf("observed = %v, want what this source actually sent", got.Observed)
	}
}

// A source with no history reports empty lists, not null: the console renders a
// list either way.
func TestSourceEventTypes_NoHistory(t *testing.T) {
	_, deps := newTestService(t)
	src := mustSource(t, deps.sources)

	got, err := deps.sources.SourceEventTypes(context.Background(), testTenant, src.ID)
	if err != nil {
		t.Fatalf("SourceEventTypes: %v", err)
	}
	if got.Observed == nil || len(got.Observed) != 0 {
		t.Errorf("observed = %v, want an empty list", got.Observed)
	}
	if len(got.Declared) == 0 {
		t.Error("declared must not be empty for a registered provider")
	}
}

func TestSourceEventTypes_TenantScoped(t *testing.T) {
	_, deps := newTestService(t)
	src := mustSource(t, deps.sources)

	if _, err := deps.sources.SourceEventTypes(context.Background(), "t-other", src.ID); !errors.Is(err, ErrSourceNotFound) {
		t.Errorf("cross-tenant = %v, want ErrSourceNotFound", err)
	}
}

// ─── Editable input declaration ──────────────────────────────────────────────

// A rotated secret or a re-enabled source lets deliveries start every trigger on it, so both need run access on each bound agent.
func TestUpdateSource_RunAccess(t *testing.T) {
	off, on := false, true
	tests := []struct {
		name       string
		disabled   bool
		req        UpdateSourceRequest
		wantCheck  bool
		noTriggers bool
	}{
		{"rotate secret", false, UpdateSourceRequest{RotateSecret: true}, true, false},
		{"re-enable", true, UpdateSourceRequest{Enabled: &on}, true, false},
		{"disable", false, UpdateSourceRequest{Enabled: &off}, true, false},
		{"disable a disabled source", true, UpdateSourceRequest{Enabled: &off}, false, false},
		{"enable an enabled source", false, UpdateSourceRequest{Enabled: &on}, false, false},
		{"rotate with no triggers", false, UpdateSourceRequest{RotateSecret: true}, false, true},
	}
	for _, tc := range tests {
		for _, allow := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/allow=%v", tc.name, allow), func(t *testing.T) {
				_, deps := newTestService(t)
				src := mustSource(t, deps.sources)
				if !tc.noTriggers {
					mustWebhookTrigger(t, deps.triggers, src.ID, "on-source", true)
				}
				if tc.disabled {
					if _, err := deps.sources.UpdateSource(context.Background(), testTenant, src.ID, UpdateSourceRequest{Enabled: &off}); err != nil {
						t.Fatalf("disable: %v", err)
					}
				}
				deps.agents.atNoRun = !allow
				deps.agents.callerReadIDs()
				before, _ := deps.repo.GetSource(context.Background(), testTenant, src.ID)
				snapshot := *before

				resp, err := deps.sources.UpdateSource(context.Background(), testTenant, src.ID, tc.req)

				if reads := deps.agents.callerReadIDs(); tc.wantCheck != slices.Equal(reads, []string{"at-1"}) {
					t.Fatalf("caller reads %v, want check=%v", reads, tc.wantCheck)
				}
				if tc.wantCheck && !allow {
					if !errors.Is(err, ErrBindForbidden) {
						t.Fatalf("got %v, want ErrBindForbidden", err)
					}
					if resp != nil {
						t.Error("a refused update must not return a secret")
					}
					if stored, _ := deps.repo.GetSource(context.Background(), testTenant, src.ID); stored.Enabled != snapshot.Enabled || stored.SecretCiphertext != snapshot.SecretCiphertext {
						t.Error("a refused update must leave the source unchanged")
					}
					return
				}
				if err != nil {
					t.Fatalf("UpdateSource: %v", err)
				}
			})
		}
	}
}

// An agent the caller cannot see refuses a rotation as forbidden; the caller named a source, not the agent.
func TestUpdateSource_UnreadableAgentIsForbidden(t *testing.T) {
	_, deps := newTestService(t)
	src := mustSource(t, deps.sources)
	mustWebhookTrigger(t, deps.triggers, src.ID, "on-source", true)
	deps.agents.callerStatus = http.StatusNotFound

	_, err := deps.sources.UpdateSource(context.Background(), testTenant, src.ID, UpdateSourceRequest{RotateSecret: true})

	if !errors.Is(err, ErrBindForbidden) {
		t.Fatalf("got %v, want ErrBindForbidden", err)
	}
}
