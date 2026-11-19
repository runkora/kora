package agents

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"testing"
	"time"

	identityclient "github.com/runkora/kora/backend/internal/platform/clients/identity"
	policyclient "github.com/runkora/kora/backend/internal/platform/clients/policy"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

// The kora-agents clients satisfy the grading principal's interfaces.
var (
	_ graderGrants       = (*policyclient.Client)(nil)
	_ serviceTokenMinter = (*identityclient.Client)(nil)
)

// fakeGraderGrants is an in-memory policy service for one tenant.
type fakeGraderGrants struct {
	grants    []policyclient.Grant
	next      int
	creates   int
	refuse    bool
	deleteErr error
	listedFor []string
}

func (f *fakeGraderGrants) ListGrants(_ context.Context, tenantID, principalType, principalID string) ([]policyclient.Grant, error) {
	f.listedFor = append(f.listedFor, tenantID+"|"+principalType+"|"+principalID)
	var out []policyclient.Grant
	for _, g := range f.grants {
		if g.TenantID == tenantID && g.PrincipalType == principalType && g.PrincipalID == principalID {
			out = append(out, g)
		}
	}
	return out, nil
}

func (f *fakeGraderGrants) CreateGrant(_ context.Context, tenantID, principalType, principalID string, scopes []string, budget *policyclient.BudgetAssignment, delegationRequired bool) (string, error) {
	if f.refuse {
		return "", errors.New("refused")
	}
	f.next++
	f.creates++
	id := fmt.Sprintf("g%d", f.next)
	f.grants = append(f.grants, policyclient.Grant{ID: id, TenantID: tenantID, PrincipalType: principalType, PrincipalID: principalID, Scopes: scopes, Budget: budget, DelegationRequired: delegationRequired})
	return id, nil
}

func (f *fakeGraderGrants) DeleteGrant(_ context.Context, grantID string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.grants = slices.DeleteFunc(f.grants, func(g policyclient.Grant) bool { return g.ID == grantID })
	return nil
}

func (f *fakeGraderGrants) only(t *testing.T) policyclient.Grant {
	t.Helper()
	if len(f.grants) != 1 {
		t.Fatalf("want one grant, got %+v", f.grants)
	}
	return f.grants[0]
}

const (
	scopeJev  = "inference:liquid:jev-1-14:use"
	scopeJev2 = "inference:liquid:jev-2:use"
)

func TestGraderPrincipalEnsure_CreatesServiceGrant(t *testing.T) {
	f := &fakeGraderGrants{}
	gp := NewGraderPrincipal(f, nil, logging.NewLoggerWithOutput(io.Discard))

	if err := gp.Ensure(context.Background(), "acme", []string{scopeJev}); err != nil {
		t.Fatal(err)
	}
	g := f.only(t)
	if g.TenantID != "acme" || g.PrincipalType != "SERVICE" || g.PrincipalID != "grader:acme" {
		t.Fatalf("grant identity = %s %s %s", g.TenantID, g.PrincipalType, g.PrincipalID)
	}
	if !scopeSetsEqual(g.Scopes, []string{"inference:invoke", scopeJev}) {
		t.Fatalf("scopes = %v", g.Scopes)
	}
	if g.DelegationRequired || g.Budget != nil {
		t.Fatalf("new grant should carry no budget and no delegation marker: %+v", g)
	}
}

func TestGraderPrincipalEnsure_Idempotent(t *testing.T) {
	f := &fakeGraderGrants{}
	gp := NewGraderPrincipal(f, nil, logging.NewLoggerWithOutput(io.Discard))

	for range 3 {
		if err := gp.Ensure(context.Background(), "acme", []string{scopeJev}); err != nil {
			t.Fatal(err)
		}
	}
	if f.creates != 1 {
		t.Fatalf("creates = %d, want 1", f.creates)
	}
	f.only(t)
}

func TestGraderPrincipalEnsure_ReplacesScopesKeepsBudget(t *testing.T) {
	budget := &policyclient.BudgetAssignment{LimitUSD: 20, ResetCadence: "monthly"}
	f := &fakeGraderGrants{grants: []policyclient.Grant{{
		ID: "old", TenantID: "acme", PrincipalType: "SERVICE", PrincipalID: "grader:acme",
		Scopes: []string{"inference:invoke", scopeJev}, Budget: budget,
	}}}
	gp := NewGraderPrincipal(f, nil, logging.NewLoggerWithOutput(io.Discard))

	// The grant holds exactly the given models. A model no Grader names any more loses its scope.
	for _, want := range [][]string{{scopeJev, scopeJev2}, {scopeJev2}} {
		if err := gp.Ensure(context.Background(), "acme", want); err != nil {
			t.Fatal(err)
		}
		g := f.only(t)
		if !scopeSetsEqual(g.Scopes, append([]string{"inference:invoke"}, want...)) {
			t.Fatalf("scopes = %v, want invoke plus %v", g.Scopes, want)
		}
		if g.Budget != budget {
			t.Fatalf("budget = %+v, want the old grant's budget kept", g.Budget)
		}
	}
	if f.creates != 2 {
		t.Fatalf("creates = %d, want 2", f.creates)
	}
	// The same set again changes nothing.
	if err := gp.Ensure(context.Background(), "acme", []string{scopeJev2, scopeJev2}); err != nil {
		t.Fatal(err)
	}
	if f.creates != 2 {
		t.Fatalf("creates = %d after a repeated set, want 2", f.creates)
	}
}

func TestGraderPrincipalEnsure_EmptySetRemovesGrants(t *testing.T) {
	f := &fakeGraderGrants{grants: []policyclient.Grant{
		{ID: "a", TenantID: "acme", PrincipalType: "SERVICE", PrincipalID: "grader:acme", Scopes: []string{"inference:invoke", scopeJev}},
		{ID: "b", TenantID: "acme", PrincipalType: "SERVICE", PrincipalID: "grader:acme", Scopes: []string{"inference:invoke", scopeJev2}},
	}}
	gp := NewGraderPrincipal(f, nil, logging.NewLoggerWithOutput(io.Discard))

	for range 2 {
		if err := gp.Ensure(context.Background(), "acme", nil); err != nil {
			t.Fatal(err)
		}
		if len(f.grants) != 0 || f.creates != 0 {
			t.Fatalf("grants = %+v creates = %d, want none", f.grants, f.creates)
		}
	}
}

func TestGraderPrincipalEnsure_EmptySetReportsFailedDelete(t *testing.T) {
	f := &fakeGraderGrants{deleteErr: errors.New("unavailable"), grants: []policyclient.Grant{
		{ID: "a", TenantID: "acme", PrincipalType: "SERVICE", PrincipalID: "grader:acme", Scopes: []string{"inference:invoke", scopeJev}},
	}}
	gp := NewGraderPrincipal(f, nil, logging.NewLoggerWithOutput(io.Discard))

	if err := gp.Ensure(context.Background(), "acme", nil); err == nil {
		t.Fatal("expected a failed delete to fail Ensure")
	}
}

func TestGraderPrincipalEnsure_MergesDuplicatesAndDropsForeignScopes(t *testing.T) {
	f := &fakeGraderGrants{grants: []policyclient.Grant{
		{ID: "a", TenantID: "acme", PrincipalType: "SERVICE", PrincipalID: "grader:acme", Scopes: []string{"inference:invoke", scopeJev}},
		{ID: "b", TenantID: "acme", PrincipalType: "SERVICE", PrincipalID: "grader:acme", Scopes: []string{"inference:invoke", scopeJev2, "agents:runs:write"}},
	}}
	gp := NewGraderPrincipal(f, nil, logging.NewLoggerWithOutput(io.Discard))

	if err := gp.Ensure(context.Background(), "acme", []string{scopeJev}); err != nil {
		t.Fatal(err)
	}
	g := f.only(t)
	if !scopeSetsEqual(g.Scopes, []string{"inference:invoke", scopeJev}) {
		t.Fatalf("scopes = %v", g.Scopes)
	}
}

func TestGraderPrincipalEnsure_ReplacesDelegatedGrant(t *testing.T) {
	f := &fakeGraderGrants{grants: []policyclient.Grant{{
		ID: "old", TenantID: "acme", PrincipalType: "SERVICE", PrincipalID: "grader:acme",
		Scopes: []string{"inference:invoke", scopeJev}, DelegationRequired: true,
	}}}
	gp := NewGraderPrincipal(f, nil, logging.NewLoggerWithOutput(io.Discard))

	if err := gp.Ensure(context.Background(), "acme", []string{scopeJev}); err != nil {
		t.Fatal(err)
	}
	if g := f.only(t); g.ID == "old" || g.DelegationRequired {
		t.Fatalf("a delegation-required grant must be replaced: %+v", g)
	}
}

func TestGraderPrincipalEnsure_RefusedCreateKeepsOldGrant(t *testing.T) {
	f := &fakeGraderGrants{refuse: true, grants: []policyclient.Grant{{
		ID: "old", TenantID: "acme", PrincipalType: "SERVICE", PrincipalID: "grader:acme", Scopes: []string{"inference:invoke", scopeJev},
	}}}
	gp := NewGraderPrincipal(f, nil, logging.NewLoggerWithOutput(io.Discard))

	if err := gp.Ensure(context.Background(), "acme", []string{scopeJev2}); err == nil {
		t.Fatal("expected the refused create to fail Ensure")
	}
	if g := f.only(t); g.ID != "old" {
		t.Fatalf("old grant deleted after a refused create: %+v", g)
	}
}

func TestGraderPrincipalEnsure_RejectsNonModelScopes(t *testing.T) {
	for _, s := range []string{
		"inference:invoke",
		"agents:runs:write",
		"inference:liquid:use",
		"inference:*:*:use",
		"inference:liquid:*:use",
		"inference::jev:use",
	} {
		t.Run(s, func(t *testing.T) {
			f := &fakeGraderGrants{}
			gp := NewGraderPrincipal(f, nil, logging.NewLoggerWithOutput(io.Discard))
			err := gp.Ensure(context.Background(), "acme", []string{s})
			if !errors.Is(err, errInvalidGraderScope) {
				t.Fatalf("err = %v, want errInvalidGraderScope", err)
			}
			if len(f.listedFor) != 0 || len(f.grants) != 0 {
				t.Fatal("a rejected scope must not reach the policy service")
			}
		})
	}
}

func TestGraderPrincipalEnsure_RequiresTenant(t *testing.T) {
	gp := NewGraderPrincipal(&fakeGraderGrants{}, nil, logging.NewLoggerWithOutput(io.Discard))
	if err := gp.Ensure(context.Background(), "", []string{scopeJev}); err == nil {
		t.Fatal("expected an error for an empty tenant")
	}
}

type fakeMinter struct {
	tenant, principal, display string
	ttl                        time.Duration
	err                        error
}

func (m *fakeMinter) MintServiceToken(_ context.Context, tenantID, principalID, displayName string, ttl time.Duration) (string, error) {
	m.tenant, m.principal, m.display, m.ttl = tenantID, principalID, displayName, ttl
	if m.err != nil {
		return "", m.err
	}
	return "tok", nil
}

func TestGraderPrincipalToken(t *testing.T) {
	m := &fakeMinter{}
	gp := NewGraderPrincipal(nil, m, logging.NewLoggerWithOutput(io.Discard))

	tok, err := gp.Token(context.Background(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	if tok != "tok" || m.tenant != "acme" || m.principal != "grader:acme" || m.display != "grader:acme" {
		t.Fatalf("mint call = %+v, token %q", m, tok)
	}
	if m.ttl <= 0 || m.ttl > 15*time.Minute {
		t.Fatalf("ttl = %s, want short-lived within identity's cap", m.ttl)
	}
}

func TestGraderPrincipalToken_Errors(t *testing.T) {
	m := &fakeMinter{err: errors.New("forbidden")}
	gp := NewGraderPrincipal(nil, m, logging.NewLoggerWithOutput(io.Discard))
	if _, err := gp.Token(context.Background(), "acme"); err == nil {
		t.Fatal("expected the mint error")
	}
	if _, err := gp.Token(context.Background(), ""); err == nil {
		t.Fatal("expected an error for an empty tenant")
	}
}
