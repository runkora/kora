package declared

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/platform/declarative"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/policyservice/api"
	"github.com/runkora/kora/backend/internal/policyservice/roles"
	"github.com/runkora/kora/backend/internal/policyservice/tenantbudget"
)

// fakeRoles records what the applier asks of the service. err is returned by every upsert.
type fakeRoles struct {
	err      error
	got      *roles.RoleEntry
	deleted  string
	declared []*roles.RoleEntry
}

func (f *fakeRoles) UpsertDeclaredRole(_ context.Context, in *roles.RoleEntry) (*roles.RoleEntry, error) {
	f.got = in
	if f.err != nil {
		return nil, f.err
	}
	out := *in
	out.ID = "row-1"
	return &out, nil
}

func (f *fakeRoles) DeleteDeclaredRole(_ context.Context, _, name string) error {
	f.deleted = name
	return nil
}

func (f *fakeRoles) ListDeclaredRoles(context.Context, string) ([]*roles.RoleEntry, error) {
	return f.declared, nil
}

func item(t *testing.T, name string, spec map[string]any) declarative.Item {
	t.Helper()
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	return declarative.Item{TenantID: "acme", Name: name, UID: "uid-" + name, Generation: 1, Spec: raw}
}

func TestRoleApplier_Projects(t *testing.T) {
	svc := &fakeRoles{}
	res, err := NewRoleApplier(svc).Apply(context.Background(), item(t, "reviewers", map[string]any{
		"scopes": []string{"agents:runs:read"},
		"budget": map[string]any{"limitUSD": 200, "resetCadence": "weekly", "pooled": true},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != declarative.OutcomeProjected || !res.Ready || res.ID != "row-1" {
		t.Errorf("result = %+v", res)
	}
	g := svc.got
	if g.TenantID != "acme" || g.Role != "reviewers" || g.ResourceName != "reviewers" || g.ResourceUID != "uid-reviewers" ||
		g.Budget == nil || g.Budget.LimitUSD != 200 || g.Budget.ResetCadence != "weekly" || !g.Budget.Pooled {
		t.Errorf("upsert = %+v", g)
	}
}

func TestRoleApplier_KeyIsSpecRole(t *testing.T) {
	svc := &fakeRoles{}
	if _, err := NewRoleApplier(svc).Apply(context.Background(), item(t, "reviewers", map[string]any{"role": "Reviewers", "scopes": []string{"a:read"}})); err != nil {
		t.Fatal(err)
	}
	if svc.got.Role != "Reviewers" || svc.got.ResourceName != "reviewers" {
		t.Errorf("upsert = %+v, want role Reviewers from resource reviewers", svc.got)
	}
}

func TestRoleApplier_Outcomes(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		outcome string
	}{
		{"reserved or invalid", fmt.Errorf("%w: reserved", api.ErrInvalidDeclaredRole), declarative.OutcomeInvalid},
		{"role held by another item", roles.ErrRoleTaken, declarative.OutcomeNameConflict},
		{"scopes missing", &api.MissingScopesError{Scopes: []string{"a:write", "b:write"}}, declarative.OutcomePending},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := NewRoleApplier(&fakeRoles{err: tc.err}).Apply(context.Background(), item(t, "reviewers", map[string]any{"scopes": []string{"a:write"}}))
			if err != nil {
				t.Fatal(err)
			}
			if res.Outcome != tc.outcome || res.Ready || res.Message == "" {
				t.Errorf("result = %+v, want %s with a message", res, tc.outcome)
			}
		})
	}

	res, _ := NewRoleApplier(&fakeRoles{err: &api.MissingScopesError{Scopes: []string{"a:write", "b:write"}}}).Apply(context.Background(), item(t, "r", map[string]any{"scopes": []string{"a:write"}}))
	if res.Message != "scopes not in the scope vocabulary: a:write, b:write" {
		t.Errorf("pending message = %q", res.Message)
	}

	if _, err := NewRoleApplier(&fakeRoles{err: errors.New("db down")}).Apply(context.Background(), item(t, "r", map[string]any{"scopes": []string{"a:read"}})); err == nil {
		t.Error("a storage failure is an error, not an outcome")
	}

	bad := declarative.Item{TenantID: "acme", Name: "r", Spec: json.RawMessage(`{"scopes":"a:read"}`)}
	if res, err := NewRoleApplier(&fakeRoles{}).Apply(context.Background(), bad); err != nil || res.Outcome != declarative.OutcomeInvalid {
		t.Errorf("malformed spec: %+v, %v", res, err)
	}
}

func TestRoleApplier_DeleteAndList(t *testing.T) {
	svc := &fakeRoles{declared: []*roles.RoleEntry{{Role: "Reviewers", ResourceName: "reviewers", ResourceUID: "uid-1"}}}
	a := NewRoleApplier(svc)
	if err := a.Delete(context.Background(), "acme", "reviewers"); err != nil || svc.deleted != "reviewers" {
		t.Errorf("delete: err = %v, deleted = %q", err, svc.deleted)
	}
	got, err := a.List(context.Background(), "acme")
	if err != nil || len(got) != 1 || got[0] != (declarative.Declared{Name: "reviewers", UID: "uid-1"}) {
		t.Errorf("list = %+v, %v", got, err)
	}
}

// fakeRegistry declares the budgets in budgets for the tenants in ids.
type fakeRegistry struct {
	ids     []string
	budgets map[string]declarative.Budget
	err     error
}

func (f fakeRegistry) TenantIDs() []string { return f.ids }
func (f fakeRegistry) TenantBudget(tenantID string) (declarative.Budget, bool) {
	b, ok := f.budgets[tenantID]
	return b, ok
}
func (f fakeRegistry) Err() error { return f.err }

// fakeBudgets records projected tenant budget writes.
type fakeBudgets struct {
	rows    []*tenantbudget.TenantBudget
	set     map[string]float64
	cadence map[string]string
	removed []string
	lists   int
}

func (f *fakeBudgets) ListDeclaredTenantBudgets(context.Context) ([]*tenantbudget.TenantBudget, error) {
	f.lists++
	return f.rows, nil
}

func (f *fakeBudgets) SetDeclaredTenantBudget(_ context.Context, tenantID string, limitUSD float64, cadence string) error {
	if f.set == nil {
		f.set, f.cadence = map[string]float64{}, map[string]string{}
	}
	f.set[tenantID], f.cadence[tenantID] = limitUSD, cadence
	return nil
}

func (f *fakeBudgets) RemoveDeclaredTenantBudget(_ context.Context, tenantID string) error {
	f.removed = append(f.removed, tenantID)
	return nil
}

var discard = logging.NewLoggerWithOutput(io.Discard)

func TestSyncBudgets(t *testing.T) {
	svc := &fakeBudgets{rows: []*tenantbudget.TenantBudget{
		{TenantID: "acme", LimitUSD: 1000, ResetCadence: "monthly"},
		{TenantID: "beta", LimitUSD: 50, ResetCadence: "weekly"},
		{TenantID: "removed", LimitUSD: 5, ResetCadence: "weekly"},
		{TenantID: "invalid", LimitUSD: 5, ResetCadence: "weekly"},
	}}
	registry := fakeRegistry{ids: []string{"acme", "beta", "gamma", "invalid", "plain"}, budgets: map[string]declarative.Budget{
		"acme":    {LimitUSD: 1000, ResetCadence: "monthly"},
		"beta":    {LimitUSD: 75, ResetCadence: "weekly"},
		"gamma":   {LimitUSD: 10},
		"invalid": {LimitUSD: 10, ResetCadence: "daily"},
	}}
	if err := syncBudgets(context.Background(), svc, registry, discard); err != nil {
		t.Fatal(err)
	}
	if len(svc.set) != 2 || svc.set["beta"] != 75 || svc.set["gamma"] != 10 {
		t.Errorf("set = %v, want beta at 75 and gamma at 10", svc.set)
	}
	if svc.cadence["gamma"] != "monthly" {
		t.Errorf("gamma cadence = %q, want the monthly default", svc.cadence["gamma"])
	}
	if len(svc.removed) != 1 || svc.removed[0] != "removed" {
		t.Errorf("removed = %v, want [removed]: an invalid budget keeps its last applied row", svc.removed)
	}
}

func TestSyncBudgets_KeepsBudgetsWhenRegistryIsUnusable(t *testing.T) {
	rows := []*tenantbudget.TenantBudget{{TenantID: "acme", LimitUSD: 1, ResetCadence: "weekly"}}
	for name, registry := range map[string]fakeRegistry{
		"unreadable": {ids: []string{"acme"}, err: errors.New("parse error")},
		"empty":      {},
	} {
		t.Run(name, func(t *testing.T) {
			svc := &fakeBudgets{rows: rows}
			_ = syncBudgets(context.Background(), svc, registry, discard)
			if len(svc.removed) != 0 || len(svc.set) != 0 {
				t.Errorf("set = %v, removed = %v", svc.set, svc.removed)
			}
		})
	}
}

func TestRunTenantBudgets_SyncsAtStartAndStops(t *testing.T) {
	svc := &fakeBudgets{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	RunTenantBudgets(ctx, svc, fakeRegistry{ids: []string{"acme"}}, time.Hour, discard)
	if svc.lists != 1 {
		t.Errorf("syncs = %d, want one at start before stopping", svc.lists)
	}
}
