package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/token"
)

// Compile-time interface check.
var _ DraftRepository = (*mockDraftRepository)(nil)

// ── mockDraftRepository ───────────────────────────────────────────────────────

type mockDraftRepository struct {
	mu     sync.Mutex
	drafts map[string]*AgentTypeDraft

	createDraftErr error
	getDraftErr    error
	listDraftsErr  error
	updateDraftErr error
	deleteDraftErr error
}

func newMockDraftRepository() *mockDraftRepository {
	return &mockDraftRepository{drafts: make(map[string]*AgentTypeDraft)}
}

func (m *mockDraftRepository) CreateDraft(_ context.Context, d AgentTypeDraft) error {
	if m.createDraftErr != nil {
		return m.createDraftErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := d
	m.drafts[d.ID] = &cp
	return nil
}

func (m *mockDraftRepository) GetDraft(_ context.Context, id string) (*AgentTypeDraft, error) {
	if m.getDraftErr != nil {
		return nil, m.getDraftErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.drafts[id]
	if !ok {
		return nil, ErrDraftNotFound
	}
	cp := *d
	return &cp, nil
}

func (m *mockDraftRepository) ListDrafts(_ context.Context, tenantID, createdBy string) ([]AgentTypeDraft, error) {
	if m.listDraftsErr != nil {
		return nil, m.listDraftsErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []AgentTypeDraft
	for _, d := range m.drafts {
		if d.TenantID == tenantID && d.CreatedBy == createdBy {
			out = append(out, *d)
		}
	}
	return out, nil
}

func (m *mockDraftRepository) UpdateDraft(_ context.Context, id string, payload json.RawMessage) error {
	if m.updateDraftErr != nil {
		return m.updateDraftErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.drafts[id]
	if !ok {
		return ErrDraftNotFound
	}
	d.Payload = payload
	d.UpdatedAt = time.Now().UTC()
	return nil
}

func (m *mockDraftRepository) DeleteDraft(_ context.Context, id string) error {
	if m.deleteDraftErr != nil {
		return m.deleteDraftErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.drafts[id]; !ok {
		return ErrDraftNotFound
	}
	delete(m.drafts, id)
	return nil
}

func (m *mockDraftRepository) DeleteDraftsOlderThan(_ context.Context, cutoff time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var deleted int64
	for id, d := range m.drafts {
		if d.UpdatedAt.Before(cutoff) {
			delete(m.drafts, id)
			deleted++
		}
	}
	return deleted, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func newDraftTestService(t *testing.T) (*DraftService, *mockDraftRepository) {
	t.Helper()
	repo := newMockDraftRepository()
	svc := NewDraftService(repo, NewAuthz(&allowAllEvaluator{}), DraftServiceConfig{}, logging.NewLoggerWithOutput(io.Discard))
	return svc, repo
}

// withUserCtx attaches PlatformClaims carrying both the tenant and the token
// subject, mirroring what AuthMiddleware stores for a human caller. Drafts
// scope to BOTH, unlike withTenantCtx which sets only the tenant.
func withUserCtx(ctx context.Context, tenantID, subject string) context.Context {
	return token.WithClaims(ctx, &token.PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: subject},
		Orbit:            token.OrbitClaims{TenantID: tenantID},
	})
}

// ── Ownership from claims, never the body ─────────────────────────────────────

func TestCreateDraft_OwnerFromClaimsNotBody(t *testing.T) {
	svc, repo := newDraftTestService(t)
	ctx := withUserCtx(context.Background(), "t-1", "user-1")

	// The payload smuggles tenant_id / created_by fields. They are opaque form
	// data — the draft's owner columns must come from the verified claims.
	payload := json.RawMessage(`{"tenant_id":"t-evil","created_by":"mallory","name":"My Bot"}`)
	d, err := svc.CreateDraft(ctx, CreateDraftRequest{Payload: payload})
	if err != nil {
		t.Fatalf("CreateDraft: %v", err)
	}
	if d.TenantID != "t-1" || d.CreatedBy != "user-1" {
		t.Errorf("owner = (%q, %q), want claims-derived (t-1, user-1)", d.TenantID, d.CreatedBy)
	}
	stored, err := repo.GetDraft(context.Background(), d.ID)
	if err != nil {
		t.Fatalf("GetDraft: %v", err)
	}
	if stored.TenantID != "t-1" || stored.CreatedBy != "user-1" {
		t.Errorf("stored owner = (%q, %q), want (t-1, user-1)", stored.TenantID, stored.CreatedBy)
	}
	// The smuggled fields survive only as opaque payload, not as ownership.
	if !bytes.Equal(stored.Payload, payload) {
		t.Errorf("payload not stored opaquely: %s", stored.Payload)
	}
}

func TestCreateDraft_RequiresVerifiedClaims(t *testing.T) {
	svc, _ := newDraftTestService(t)
	cases := map[string]context.Context{
		"no claims":       context.Background(),
		"missing subject": withUserCtx(context.Background(), "t-1", ""),
		"missing tenant":  withUserCtx(context.Background(), "", "user-1"),
	}
	for name, ctx := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := svc.CreateDraft(ctx, CreateDraftRequest{}); !errors.Is(err, errNoVerifiedClaims) {
				t.Errorf("CreateDraft = %v, want errNoVerifiedClaims", err)
			}
		})
	}
}

func TestCreateDraft_EmptyPayloadDefaultsToEmptyObject(t *testing.T) {
	svc, _ := newDraftTestService(t)
	ctx := withUserCtx(context.Background(), "t-1", "user-1")

	d, err := svc.CreateDraft(ctx, CreateDraftRequest{})
	if err != nil {
		t.Fatalf("CreateDraft: %v", err)
	}
	if string(d.Payload) != "{}" {
		t.Errorf("payload = %s, want {}", d.Payload)
	}
	if d.ID == "" {
		t.Error("ID must be set")
	}
}

// ── Cross-user / cross-tenant access surfaces as not-found ────────────────────

func TestDraftAccess_OtherCallersSeeNotFound(t *testing.T) {
	svc, _ := newDraftTestService(t)
	owner := withUserCtx(context.Background(), "t-1", "user-1")

	d, err := svc.CreateDraft(owner, CreateDraftRequest{Payload: json.RawMessage(`{"name":"mine"}`)})
	if err != nil {
		t.Fatalf("CreateDraft: %v", err)
	}

	tests := []struct {
		name string
		ctx  context.Context
	}{
		{"same tenant, different user", withUserCtx(context.Background(), "t-1", "user-2")},
		{"different tenant, same user", withUserCtx(context.Background(), "t-2", "user-1")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.GetDraft(tc.ctx, d.ID); !errors.Is(err, ErrDraftNotFound) {
				t.Errorf("GetDraft = %v, want ErrDraftNotFound (not forbidden — no existence leak)", err)
			}
			if _, err := svc.UpdateDraft(tc.ctx, d.ID, UpdateDraftRequest{Payload: json.RawMessage(`{}`)}); !errors.Is(err, ErrDraftNotFound) {
				t.Errorf("UpdateDraft = %v, want ErrDraftNotFound", err)
			}
			if err := svc.DeleteDraft(tc.ctx, d.ID); !errors.Is(err, ErrDraftNotFound) {
				t.Errorf("DeleteDraft = %v, want ErrDraftNotFound", err)
			}
		})
	}

	// The owner still reads, updates, and deletes it.
	if _, err := svc.GetDraft(owner, d.ID); err != nil {
		t.Fatalf("owner GetDraft: %v", err)
	}
	updated, err := svc.UpdateDraft(owner, d.ID, UpdateDraftRequest{Payload: json.RawMessage(`{"name":"renamed"}`)})
	if err != nil {
		t.Fatalf("owner UpdateDraft: %v", err)
	}
	if string(updated.Payload) != `{"name":"renamed"}` {
		t.Errorf("payload = %s, want replaced", updated.Payload)
	}
	if err := svc.DeleteDraft(owner, d.ID); err != nil {
		t.Fatalf("owner DeleteDraft: %v", err)
	}
}

func TestListDrafts_FiltersToCaller(t *testing.T) {
	svc, _ := newDraftTestService(t)
	alice := withUserCtx(context.Background(), "t-1", "alice")
	bob := withUserCtx(context.Background(), "t-1", "bob")
	otherTenant := withUserCtx(context.Background(), "t-2", "alice")

	for _, ctx := range []context.Context{alice, alice, bob, otherTenant} {
		if _, err := svc.CreateDraft(ctx, CreateDraftRequest{}); err != nil {
			t.Fatalf("CreateDraft: %v", err)
		}
	}

	got, err := svc.ListDrafts(alice)
	if err != nil {
		t.Fatalf("ListDrafts: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("alice sees %d drafts, want 2", len(got))
	}
	for _, d := range got {
		if d.TenantID != "t-1" || d.CreatedBy != "alice" {
			t.Errorf("leaked draft owned by (%q, %q)", d.TenantID, d.CreatedBy)
		}
	}
}

// ── Payload validation: JSON-valid and capped, nothing more ───────────────────

func TestDraftPayload_Validation(t *testing.T) {
	oversized := append(json.RawMessage(`{"pad":"`), bytes.Repeat([]byte("x"), maxDraftPayloadBytes)...)
	oversized = append(oversized, `"}`...)

	tests := []struct {
		name    string
		payload json.RawMessage
		wantErr error
	}{
		{"invalid JSON rejected", json.RawMessage(`{"name": unquoted}`), ErrDraftPayloadInvalid},
		{"truncated JSON rejected", json.RawMessage(`{"name":`), ErrDraftPayloadInvalid},
		{"over 64KB rejected", oversized, ErrDraftPayloadTooLarge},
		{"exactly at cap accepted", atCapPayload(), nil},
		{"no field validation — arbitrary shape accepted", json.RawMessage(`{"slug":"","bogus":[1,2,3]}`), nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := newDraftTestService(t)
			ctx := withUserCtx(context.Background(), "t-1", "user-1")

			_, createErr := svc.CreateDraft(ctx, CreateDraftRequest{Payload: tc.payload})
			if !errors.Is(createErr, tc.wantErr) {
				t.Errorf("CreateDraft = %v, want %v", createErr, tc.wantErr)
			}

			// The same rules apply on update.
			d, err := svc.CreateDraft(ctx, CreateDraftRequest{})
			if err != nil {
				t.Fatalf("CreateDraft(seed): %v", err)
			}
			_, updateErr := svc.UpdateDraft(ctx, d.ID, UpdateDraftRequest{Payload: tc.payload})
			if !errors.Is(updateErr, tc.wantErr) {
				t.Errorf("UpdateDraft = %v, want %v", updateErr, tc.wantErr)
			}
		})
	}
}

// atCapPayload builds a valid JSON payload of exactly maxDraftPayloadBytes.
func atCapPayload() json.RawMessage {
	const overhead = len(`{"pad":""}`)
	p := append(json.RawMessage(`{"pad":"`), bytes.Repeat([]byte("x"), maxDraftPayloadBytes-overhead)...)
	return append(p, `"}`...)
}

// ── Reaper ────────────────────────────────────────────────────────────────────

func TestDraftReaper_DeletesOnlyStaleDrafts(t *testing.T) {
	svc, repo := newDraftTestService(t)
	ctx := withUserCtx(context.Background(), "t-1", "user-1")

	stale, err := svc.CreateDraft(ctx, CreateDraftRequest{})
	if err != nil {
		t.Fatalf("CreateDraft: %v", err)
	}
	fresh, err := svc.CreateDraft(ctx, CreateDraftRequest{})
	if err != nil {
		t.Fatalf("CreateDraft: %v", err)
	}
	repo.mu.Lock()
	repo.drafts[stale.ID].UpdatedAt = time.Now().UTC().Add(-31 * 24 * time.Hour)
	repo.mu.Unlock()

	deleted, err := repo.DeleteDraftsOlderThan(context.Background(), time.Now().UTC().Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("DeleteDraftsOlderThan: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}
	if _, err := svc.GetDraft(ctx, stale.ID); !errors.Is(err, ErrDraftNotFound) {
		t.Errorf("stale draft should be reaped, got %v", err)
	}
	if _, err := svc.GetDraft(ctx, fresh.ID); err != nil {
		t.Errorf("fresh draft must survive the sweep: %v", err)
	}
}
