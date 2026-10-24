package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/platform/declarative"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/token"
	"github.com/runkora/kora/backend/internal/policyservice/roles"
	"github.com/runkora/kora/backend/internal/policyservice/tenantbudget"
	"github.com/runkora/kora/backend/internal/policyservice/vocabulary"
)

// fakeRegistry locks the tenants in locked and declares the budgets in budgets.
type fakeRegistry struct {
	locked  map[string]bool
	budgets map[string]declarative.Budget
}

func (f fakeRegistry) APIAuthoringEnabled(tenantID string) bool { return !f.locked[tenantID] }

func (f fakeRegistry) TenantBudget(tenantID string) (declarative.Budget, bool) {
	b, ok := f.budgets[tenantID]
	return b, ok
}

// declaredRoles records declared writes and holds one stored role.
type declaredRoles struct {
	roles.Repository
	stored   *roles.RoleEntry
	upserted *roles.RoleEntry
	declared []*roles.RoleEntry
	deleted  []string
}

func (r *declaredRoles) GetByRole(_ context.Context, _, role string) (*roles.RoleEntry, error) {
	if r.stored == nil || r.stored.Role != role {
		return nil, roles.ErrRoleNotFound
	}
	return r.stored, nil
}

func (r *declaredRoles) ListByTenant(context.Context, string, []string, int, int) ([]*roles.RoleEntry, error) {
	return []*roles.RoleEntry{r.stored}, nil
}

func (r *declaredRoles) UpsertDeclared(_ context.Context, e *roles.RoleEntry) error {
	r.upserted = e
	return nil
}

func (r *declaredRoles) ListDeclared(context.Context, string) ([]*roles.RoleEntry, error) {
	return r.declared, nil
}

func (r *declaredRoles) DeleteDeclared(_ context.Context, _, name string) (bool, error) {
	r.deleted = append(r.deleted, name)
	return name == "present", nil
}

// knownScopes is a vocabulary holding only the scopes listed.
type knownScopes struct {
	vocabulary.Repository
	scopes map[string]bool
}

func (v knownScopes) Exists(_ context.Context, scope string) (bool, error) {
	return v.scopes[scope], nil
}
func (knownScopes) List(context.Context, string) ([]*vocabulary.ScopeEntry, error) {
	return nil, nil
}

func newDeclaredTestService(t *testing.T, repo roles.Repository) *Service {
	t.Helper()
	svc := &Service{
		roleRepo:        repo,
		vocabRepo:       knownScopes{scopes: map[string]bool{"agents:runs:read": true}},
		logger:          logging.NewLoggerWithOutput(io.Discard),
		recompileTimers: map[string]*time.Timer{},
		lastWriteAt:     map[string]time.Time{},
	}
	// A write schedules a debounced recompile that needs a bundle server this
	// test does not have. Stop it before it fires.
	t.Cleanup(func() {
		svc.recompileMu.Lock()
		defer svc.recompileMu.Unlock()
		for _, tm := range svc.recompileTimers {
			tm.Stop()
		}
		if svc.gatewayRecompileTimer != nil {
			svc.gatewayRecompileTimer.Stop()
		}
	})
	return svc
}

func TestUpsertDeclaredRole(t *testing.T) {
	cases := []struct {
		name    string
		role    string
		scopes  []string
		budget  *roles.BudgetAssignment
		invalid bool
		missing []string
	}{
		{name: "reserved role", role: "platform-admin", scopes: []string{"agents:runs:read"}, invalid: true},
		{name: "no scopes", role: "reviewers", invalid: true},
		{name: "zero budget", role: "reviewers", scopes: []string{"agents:runs:read"}, budget: &roles.BudgetAssignment{ResetCadence: "monthly"}, invalid: true},
		{name: "daily budget", role: "reviewers", scopes: []string{"agents:runs:read"}, budget: &roles.BudgetAssignment{LimitUSD: 5, ResetCadence: "daily"}, invalid: true},
		{name: "scopes missing from the vocabulary", role: "reviewers", scopes: []string{"a:write", "agents:runs:read", "b:write"}, missing: []string{"a:write", "b:write"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &declaredRoles{}
			in := &roles.RoleEntry{TenantID: "t1", Role: tc.role, Scopes: tc.scopes, Budget: tc.budget, ResourceName: "r", ResourceUID: "uid-1"}
			_, err := newDeclaredTestService(t, repo).UpsertDeclaredRole(context.Background(), in)
			if tc.invalid != errors.Is(err, ErrInvalidDeclaredRole) {
				t.Errorf("err = %v, invalid = %v", err, tc.invalid)
			}
			var missing *MissingScopesError
			if tc.missing != nil && (!errors.As(err, &missing) || !slices.Equal(missing.Scopes, tc.missing)) {
				t.Errorf("err = %v, want missing %v", err, tc.missing)
			}
			if repo.upserted != nil {
				t.Errorf("a refused role was written: %+v", repo.upserted)
			}
		})
	}

	repo := &declaredRoles{}
	in := &roles.RoleEntry{TenantID: "t1", Role: "reviewers", Scopes: []string{"agents:runs:read"}, Budget: &roles.BudgetAssignment{LimitUSD: 200, Pooled: true}, ResourceName: "reviewers", ResourceUID: "uid-1"}
	if _, err := newDeclaredTestService(t, repo).UpsertDeclaredRole(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	got := repo.upserted
	if got == nil || got.ResourceUID != "uid-1" || got.ResourceName != "reviewers" || got.Budget.ResetCadence != "monthly" || !got.Budget.Pooled {
		t.Errorf("upserted = %+v", got)
	}
}

func TestDeleteDeclaredRole(t *testing.T) {
	repo := &declaredRoles{}
	svc := newDeclaredTestService(t, repo)
	if err := svc.DeleteDeclaredRole(context.Background(), "t1", "absent"); err != nil || len(svc.recompileTimers) != 0 {
		t.Errorf("deleting nothing: err = %v, recompiles = %d", err, len(svc.recompileTimers))
	}
	if err := svc.DeleteDeclaredRole(context.Background(), "t1", "present"); err != nil || len(svc.recompileTimers) != 1 {
		t.Errorf("deleting a row: err = %v, recompiles = %d", err, len(svc.recompileTimers))
	}
}

func TestRoleWrites_DeclaredAndLocked(t *testing.T) {
	system := &token.PlatformClaims{Kora: token.KoraClaims{PrincipalType: token.PrincipalTypeSystem}}
	type call struct {
		method, role, body string
	}
	upsert := func(role string) call {
		return call{http.MethodPut, role, `{"tenant_id":"t1","role":"` + role + `","scopes":["agents:runs:read"]}`}
	}
	patch := func(role string) call { return call{http.MethodPatch, role, `{"tenant_id":"t1","budget":null}`} }
	del := func(role string) call { return call{http.MethodDelete, role, ""} }

	cases := []struct {
		name     string
		call     call
		locked   bool
		wantCode int
		wantErr  string
	}{
		{"upsert a declared role", upsert("declared"), false, http.StatusConflict, "managed_by_resource"},
		{"budget on a declared role", patch("declared"), false, http.StatusConflict, "managed_by_resource"},
		{"delete a declared role", del("declared"), false, http.StatusConflict, "managed_by_resource"},
		{"upsert in a locked tenant", upsert("new"), true, http.StatusForbidden, "api_authoring_disabled"},
		{"budget in a locked tenant", patch("declared"), true, http.StatusForbidden, "api_authoring_disabled"},
		{"delete in a locked tenant", del("declared"), true, http.StatusForbidden, "api_authoring_disabled"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &declaredRoles{stored: &roles.RoleEntry{TenantID: "t1", Role: "declared", Source: roles.SourceCRD}}
			c := &Controller{
				svc:      newDeclaredTestService(t, repo),
				logger:   logging.NewLoggerWithOutput(io.Discard),
				registry: fakeRegistry{locked: map[string]bool{"t1": tc.locked}},
			}
			req := httptest.NewRequest(tc.call.method, "/roles/"+tc.call.role+"?tenant_id=t1", bytes.NewBufferString(tc.call.body))
			req.SetPathValue("role", tc.call.role)
			req = req.WithContext(token.WithClaims(req.Context(), system))
			w := httptest.NewRecorder()
			switch tc.call.method {
			case http.MethodPut:
				c.handleUpsertRole(w, req)
			case http.MethodPatch:
				c.handleUpdateRoleBudget(w, req)
			default:
				c.handleDeleteRole(w, req)
			}
			var body struct {
				Code string `json:"code"`
			}
			_ = json.Unmarshal(w.Body.Bytes(), &body)
			if w.Code != tc.wantCode || body.Code != tc.wantErr {
				t.Errorf("got %d %q, want %d %q (body %s)", w.Code, body.Code, tc.wantCode, tc.wantErr, w.Body)
			}
		})
	}
}

func TestListRoles_CarriesSource(t *testing.T) {
	repo := &declaredRoles{stored: &roles.RoleEntry{TenantID: "t1", Role: "declared", Scopes: []string{"agents:runs:read"}, Source: roles.SourceCRD, ResourceUID: "uid-1"}}
	c := &Controller{svc: newDeclaredTestService(t, repo), logger: logging.NewLoggerWithOutput(io.Discard)}
	req := httptest.NewRequest(http.MethodGet, "/roles", nil)
	req = req.WithContext(token.WithClaims(req.Context(), &token.PlatformClaims{Kora: token.KoraClaims{PrincipalType: token.PrincipalTypeHuman, TenantID: "t1"}}))
	w := httptest.NewRecorder()
	c.handleListRoles(w, req)

	var body struct {
		Roles []map[string]any `json:"roles"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, r := range body.Roles {
		if r["role"] != "declared" {
			continue
		}
		if r["source"] != "crd" {
			t.Errorf("source = %v, want crd", r["source"])
		}
		if _, leaked := r["resource_uid"]; leaked {
			t.Error("the resource UID is not part of the API")
		}
		return
	}
	t.Fatalf("declared role missing from %s", w.Body)
}

// apiTenantBudgets accepts every API write.
type apiTenantBudgets struct{ tenantbudget.Repository }

func (apiTenantBudgets) Upsert(context.Context, *tenantbudget.TenantBudget) error { return nil }
func (apiTenantBudgets) Delete(context.Context, string) error                     { return nil }

func TestTenantBudgetWrites_Declared(t *testing.T) {
	for _, tc := range []struct {
		name     string
		declared bool
		want     int
	}{
		{"declared", true, http.StatusConflict},
		{"not declared", false, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry := fakeRegistry{}
			if tc.declared {
				registry.budgets = map[string]declarative.Budget{"t1": {LimitUSD: 1000, ResetCadence: "monthly"}}
			}
			svc := &Service{
				tenantBudgetRepo: apiTenantBudgets{},
				logger:           logging.NewLoggerWithOutput(io.Discard),
				recompileTimers:  map[string]*time.Timer{},
				lastWriteAt:      map[string]time.Time{},
			}
			t.Cleanup(func() {
				for _, tm := range svc.recompileTimers {
					tm.Stop()
				}
			})
			c := &Controller{svc: svc, logger: svc.logger, registry: registry}
			human := &token.PlatformClaims{Kora: token.KoraClaims{PrincipalType: token.PrincipalTypeHuman, TenantID: "t1"}}

			put := httptest.NewRequest(http.MethodPut, "/tenant-budget", bytes.NewBufferString(`{"limit_usd":10,"reset_cadence":"weekly"}`))
			w := httptest.NewRecorder()
			c.handleUpsertTenantBudget(w, put.WithContext(token.WithClaims(put.Context(), human)))
			if w.Code != tc.want {
				t.Errorf("PUT status = %d, want %d", w.Code, tc.want)
			}

			wantDelete := http.StatusNoContent
			if tc.declared {
				wantDelete = http.StatusConflict
			}
			del := httptest.NewRequest(http.MethodDelete, "/tenant-budget", nil)
			w = httptest.NewRecorder()
			c.handleDeleteTenantBudget(w, del.WithContext(token.WithClaims(del.Context(), human)))
			if w.Code != wantDelete {
				t.Errorf("DELETE status = %d, want %d", w.Code, wantDelete)
			}
		})
	}
}

func TestUpsertDeclaredRole_UnchangedWritesNothing(t *testing.T) {
	stored := &roles.RoleEntry{ID: "row-1", TenantID: "t1", Role: "reviewers", Scopes: []string{"agents:runs:read"}, Source: roles.SourceCRD,
		ResourceName: "reviewers", ResourceUID: "uid-1", Budget: &roles.BudgetAssignment{LimitUSD: 5, ResetCadence: "monthly"}}
	repo := &declaredRoles{stored: stored}
	svc := newDeclaredTestService(t, repo)
	in := &roles.RoleEntry{TenantID: "t1", Role: "reviewers", Scopes: []string{"agents:runs:read"}, Budget: &roles.BudgetAssignment{LimitUSD: 5},
		ResourceName: "reviewers", ResourceUID: "uid-1"}
	got, err := svc.UpsertDeclaredRole(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "row-1" || repo.upserted != nil || len(svc.recompileTimers) != 0 {
		t.Errorf("an unchanged resource was written: id %s, upserted %+v", got.ID, repo.upserted)
	}
}
