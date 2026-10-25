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
	declared []declarative.Declared
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

func (r *declaredRoles) ListDeclared(context.Context, string) ([]declarative.Declared, error) {
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

func roleItem(t *testing.T, name string, spec map[string]any) declarative.Item {
	t.Helper()
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	return declarative.Item{TenantID: "t1", Name: name, UID: "uid-" + name, Generation: 1, Spec: raw, Repository: "github.com/acme/roles"}
}

func TestRoleApplier_Refusals(t *testing.T) {
	cases := []struct {
		name    string
		spec    map[string]any
		outcome string
		message string
	}{
		{"reserved role", map[string]any{"role": "platform-admin", "scopes": []string{"agents:runs:read"}}, declarative.OutcomeInvalid, ""},
		{"no scopes", map[string]any{"scopes": []string{}}, declarative.OutcomeInvalid, ""},
		{"zero budget", map[string]any{"scopes": []string{"agents:runs:read"}, "budget": map[string]any{"limitUSD": 0}}, declarative.OutcomeInvalid, ""},
		{"daily budget", map[string]any{"scopes": []string{"agents:runs:read"}, "budget": map[string]any{"limitUSD": 5, "resetCadence": "daily"}}, declarative.OutcomeInvalid, ""},
		{"malformed spec", map[string]any{"scopes": "agents:runs:read"}, declarative.OutcomeInvalid, ""},
		{"scopes missing from the vocabulary", map[string]any{"scopes": []string{"a:write", "agents:runs:read", "b:write"}},
			declarative.OutcomePending, "scopes not in the scope vocabulary: a:write, b:write"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &declaredRoles{}
			res, err := NewRoleApplier(newDeclaredTestService(t, repo)).Apply(context.Background(), roleItem(t, "reviewers", tc.spec))
			if err != nil {
				t.Fatal(err)
			}
			if res.Outcome != tc.outcome || res.Ready || res.Message == "" || (tc.message != "" && res.Message != tc.message) {
				t.Errorf("result = %+v, want %s %q", res, tc.outcome, tc.message)
			}
			if repo.upserted != nil {
				t.Errorf("a refused role was written: %+v", repo.upserted)
			}
		})
	}
}

func TestRoleApplier_Projects(t *testing.T) {
	repo := &declaredRoles{}
	svc := newDeclaredTestService(t, repo)
	res, err := NewRoleApplier(svc).Apply(context.Background(), roleItem(t, "reviewers", map[string]any{
		"role": "Reviewers", "scopes": []string{"agents:runs:read"}, "budget": map[string]any{"limitUSD": 200, "pooled": true},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != declarative.OutcomeProjected || !res.Ready {
		t.Errorf("result = %+v", res)
	}
	got := repo.upserted
	if got == nil || got.Role != "Reviewers" || got.ResourceName != "reviewers" || got.ResourceUID != "uid-reviewers" ||
		got.Repository != "github.com/acme/roles" || got.Budget.ResetCadence != "monthly" || !got.Budget.Pooled {
		t.Errorf("upserted = %+v", got)
	}
	if len(svc.recompileTimers) != 1 {
		t.Errorf("recompiles = %d, want 1", len(svc.recompileTimers))
	}
}

// takenRoles refuses every declared write as a role held by another item.
type takenRoles struct{ declaredRoles }

func (*takenRoles) UpsertDeclared(context.Context, *roles.RoleEntry) error { return roles.ErrRoleTaken }

func TestRoleApplier_NameConflict(t *testing.T) {
	res, err := NewRoleApplier(newDeclaredTestService(t, &takenRoles{})).Apply(context.Background(), roleItem(t, "reviewers", map[string]any{"scopes": []string{"agents:runs:read"}}))
	if err != nil || res.Outcome != declarative.OutcomeNameConflict || res.Message == "" {
		t.Errorf("result = %+v, %v", res, err)
	}
}

func TestRoleApplier_UnchangedWritesNothing(t *testing.T) {
	stored := &roles.RoleEntry{ID: "row-1", TenantID: "t1", Role: "reviewers", Scopes: []string{"agents:runs:read"}, Source: declarative.SourceCRD,
		ResourceName: "reviewers", ResourceUID: "uid-reviewers", Repository: "github.com/acme/roles", Budget: &roles.BudgetAssignment{LimitUSD: 5, ResetCadence: "monthly"}}
	repo := &declaredRoles{stored: stored}
	svc := newDeclaredTestService(t, repo)
	res, err := NewRoleApplier(svc).Apply(context.Background(), roleItem(t, "reviewers", map[string]any{
		"scopes": []string{"agents:runs:read"}, "budget": map[string]any{"limitUSD": 5},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.ID != "row-1" || !res.Ready || repo.upserted != nil || len(svc.recompileTimers) != 0 {
		t.Errorf("an unchanged resource was written: %+v, upserted %+v", res, repo.upserted)
	}
}

func TestRoleApplier_DeleteAndList(t *testing.T) {
	repo := &declaredRoles{declared: []declarative.Declared{{Name: "reviewers", UID: "uid-1"}}}
	svc := newDeclaredTestService(t, repo)
	a := NewRoleApplier(svc)
	if err := a.Delete(context.Background(), "t1", "absent"); err != nil || len(svc.recompileTimers) != 0 {
		t.Errorf("deleting nothing: err = %v, recompiles = %d", err, len(svc.recompileTimers))
	}
	if err := a.Delete(context.Background(), "t1", "present"); err != nil || len(svc.recompileTimers) != 1 {
		t.Errorf("deleting a row: err = %v, recompiles = %d", err, len(svc.recompileTimers))
	}
	got, err := a.List(context.Background(), "t1")
	if err != nil || !slices.Equal(got, repo.declared) {
		t.Errorf("list = %+v, %v", got, err)
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
			repo := &declaredRoles{stored: &roles.RoleEntry{TenantID: "t1", Role: "declared", Source: declarative.SourceCRD}}
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
	repo := &declaredRoles{stored: &roles.RoleEntry{TenantID: "t1", Role: "declared", Scopes: []string{"agents:runs:read"}, Source: declarative.SourceCRD, ResourceUID: "uid-1"}}
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

// budgetRegistryFake declares the budgets in budgets for the tenants in ids.
type budgetRegistryFake struct {
	ids     []string
	budgets map[string]declarative.Budget
	err     error
}

func (f budgetRegistryFake) TenantIDs() []string { return f.ids }
func (f budgetRegistryFake) TenantBudget(tenantID string) (declarative.Budget, bool) {
	b, ok := f.budgets[tenantID]
	return b, ok
}
func (f budgetRegistryFake) Err() error { return f.err }

// declaredBudgets records projected tenant budget writes.
type declaredBudgets struct {
	tenantbudget.Repository
	rows    []*tenantbudget.TenantBudget
	set     map[string]*tenantbudget.TenantBudget
	removed []string
	lists   int
}

func (f *declaredBudgets) ListDeclared(context.Context) ([]*tenantbudget.TenantBudget, error) {
	f.lists++
	return f.rows, nil
}

func (f *declaredBudgets) UpsertDeclared(_ context.Context, tb *tenantbudget.TenantBudget) error {
	if f.set == nil {
		f.set = map[string]*tenantbudget.TenantBudget{}
	}
	f.set[tb.TenantID] = tb
	return nil
}

func (f *declaredBudgets) DeleteDeclared(_ context.Context, tenantID string) error {
	f.removed = append(f.removed, tenantID)
	return nil
}

func newBudgetTestService(t *testing.T, repo tenantbudget.Repository) *Service {
	t.Helper()
	svc := newDeclaredTestService(t, &declaredRoles{})
	svc.tenantBudgetRepo = repo
	return svc
}

func TestSyncTenantBudgets(t *testing.T) {
	repo := &declaredBudgets{rows: []*tenantbudget.TenantBudget{
		{TenantID: "acme", LimitUSD: 1000, ResetCadence: "monthly"},
		{TenantID: "beta", LimitUSD: 50, ResetCadence: "weekly"},
		{TenantID: "removed", LimitUSD: 5, ResetCadence: "weekly"},
		{TenantID: "invalid", LimitUSD: 5, ResetCadence: "weekly"},
	}}
	registry := budgetRegistryFake{ids: []string{"acme", "beta", "gamma", "invalid", "plain"}, budgets: map[string]declarative.Budget{
		"acme":    {LimitUSD: 1000, ResetCadence: "monthly"},
		"beta":    {LimitUSD: 75, ResetCadence: "weekly"},
		"gamma":   {LimitUSD: 10},
		"invalid": {LimitUSD: 10, ResetCadence: "daily"},
	}}
	if err := newBudgetTestService(t, repo).syncTenantBudgets(context.Background(), registry); err != nil {
		t.Fatal(err)
	}
	if len(repo.set) != 2 || repo.set["beta"].LimitUSD != 75 || repo.set["gamma"].ResetCadence != "monthly" {
		t.Errorf("set = %+v, want beta at 75 and gamma with the monthly default", repo.set)
	}
	if len(repo.removed) != 1 || repo.removed[0] != "removed" {
		t.Errorf("removed = %v, want [removed]: an invalid budget keeps its last applied row", repo.removed)
	}
}

func TestSyncTenantBudgets_KeepsBudgetsWhenRegistryIsUnusable(t *testing.T) {
	rows := []*tenantbudget.TenantBudget{{TenantID: "acme", LimitUSD: 1, ResetCadence: "weekly"}}
	for name, registry := range map[string]budgetRegistryFake{
		"unreadable": {ids: []string{"acme"}, err: errors.New("parse error")},
		"empty":      {},
	} {
		t.Run(name, func(t *testing.T) {
			repo := &declaredBudgets{rows: rows}
			_ = newBudgetTestService(t, repo).syncTenantBudgets(context.Background(), registry)
			if len(repo.removed) != 0 || len(repo.set) != 0 {
				t.Errorf("set = %v, removed = %v", repo.set, repo.removed)
			}
		})
	}
}

func TestRunTenantBudgets_SyncsAtStartAndStops(t *testing.T) {
	repo := &declaredBudgets{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	newBudgetTestService(t, repo).RunTenantBudgets(ctx, budgetRegistryFake{ids: []string{"acme"}})
	if repo.lists != 1 {
		t.Errorf("syncs = %d, want one at start before stopping", repo.lists)
	}
}

// failingRoles fails every role read.
type failingRoles struct{ declaredRoles }

func (*failingRoles) GetByRole(context.Context, string, string) (*roles.RoleEntry, error) {
	return nil, errors.New("db down")
}

func TestRefuseRoleWrite_ReadErrorIsNotAnAPIRole(t *testing.T) {
	c := &Controller{svc: newDeclaredTestService(t, &failingRoles{}), logger: logging.NewLoggerWithOutput(io.Discard), registry: fakeRegistry{}}
	w := httptest.NewRecorder()
	if !c.refuseRoleWrite(context.Background(), w, "t1", "reviewers") || w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 when the role cannot be read", w.Code)
	}
}
