package agents

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	policyclient "github.com/clear-route/orbit/internal/platform/clients/policy"
	"github.com/clear-route/orbit/internal/platform/logging"
)

func TestAgentSlugFromPrincipalID(t *testing.T) {
	cases := []struct {
		id   string
		slug string
		ok   bool
	}{
		{"spiffe://orbit.test/tenant/orbit-tenant-example/agent/coder", "coder", true},
		{"spiffe://orbit.test/tenant/orbit-tenant-example/agent/", "", false},
		{"spiffe://orbit.test/tenant/orbit-tenant-example/service/coder", "", false},
		{"trigger-source:abc", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		if slug, ok := agentSlugFromPrincipalID(tc.id); slug != tc.slug || ok != tc.ok {
			t.Errorf("%q = %q, %v; want %q, %v", tc.id, slug, ok, tc.slug, tc.ok)
		}
	}
}

func TestAgentBudgetsBySlug_MapsGrantsAndSkipsTheRest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		grants := []policyclient.Grant{
			{PrincipalID: "spiffe://orbit.test/tenant/ns/agent/coder", Budget: &policyclient.BudgetAssignment{LimitUSD: 50, ResetCadence: "weekly", PerRunLimitUSD: 5}},
			{PrincipalID: "spiffe://orbit.test/tenant/ns/agent/no-budget"},
			{PrincipalID: "not-a-spiffe-id", Budget: &policyclient.BudgetAssignment{LimitUSD: 9}},
			{PrincipalID: "spiffe://orbit.test/tenant/ns/agent/", Budget: &policyclient.BudgetAssignment{LimitUSD: 9}},
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"grants": grants, "total": len(grants)})
	}))
	t.Cleanup(srv.Close)
	logger := logging.NewLoggerWithOutput(io.Discard)
	p := &K8sProvisioner{
		policyClient: policyclient.NewClient(srv.URL, func(context.Context) (string, error) { return "tok", nil }, logger),
		logger:       logger,
	}

	got, err := p.AgentBudgetsBySlug(context.Background(), "t1")
	if err != nil {
		t.Fatalf("AgentBudgetsBySlug: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("budgets = %+v, want only coder", got)
	}
	if b := got["coder"]; b.LimitUSD != 50 || b.ResetCadence != "weekly" || b.PerRunLimitUSD != 5 {
		t.Errorf("coder = %+v", b)
	}
}

func TestAgentBudgetsBySlug_NoPolicyClient(t *testing.T) {
	got, err := (&K8sProvisioner{}).AgentBudgetsBySlug(context.Background(), "t1")
	if err != nil || len(got) != 0 {
		t.Errorf("got %+v, %v; want an empty map", got, err)
	}
}
