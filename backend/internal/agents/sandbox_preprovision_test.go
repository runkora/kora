package agents

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	platformconns "github.com/clear-route/orbit/internal/platform/connections"
)

// seedKubeRun sets up a run whose agent type declares one Kubernetes connector,
// and installs a resolver whose registry decides whether that connector
// resolves. An empty registry models the window before connections.json is
// mounted, which is what a pre-provision at session start races.
func seedKubeRun(t *testing.T, repo *mockRepository, svc *testServiceFacade, resolves bool) {
	t.Helper()
	at := seedAgentType(t, repo, AgentType{
		ID: "at-1", TenantID: "t-1", Slug: "bot",
		Connectors: []Connector{kubeConnector("prod-cluster")},
	})
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateActive})

	entries := map[string]struct{ adapter, baseHost string }{}
	if resolves {
		entries["prod-cluster"] = struct{ adapter, baseHost string }{platformconns.AdapterKubernetesBearer, "k8s.example.com:443"}
	}
	svc.run.SetScopeResolver(NewScopeResolver(nil, stubConns{entries: entries}))
}

// On-demand provisioning is a correctness requirement, not a latency
// optimization: the agent is asking for a shell now. An unresolved registry
// still yields a sandbox, because no kubeconfig beats no sandbox.
func TestProvisionSandbox_OnDemandProceedsWhileRegistryUnresolved(t *testing.T) {
	svc, repo, k8s, _ := newTestService(t)
	seedKubeRun(t, repo, svc, false)
	k8s.sandboxEndpoint = "10.0.0.5:50051"

	sandbox, err := svc.run.ProvisionSandbox(context.Background(), "run-1", "", false)
	if err != nil {
		t.Fatalf("on-demand provisioning failed: %v", err)
	}
	if sandbox.Endpoint != "10.0.0.5:50051" {
		t.Errorf("endpoint = %q, want 10.0.0.5:50051", sandbox.Endpoint)
	}
}

// Without the flag the request is on-demand, which provisions even while the
// registry is unresolved. This is the compatibility half: an older
// session-manager sends no flag and keeps working.
func TestHandleProvisionSandbox_NoFlagIsOnDemand(t *testing.T) {
	c, repo, k8s, _ := newTestController(t)
	seedKubeRunForController(t, repo, c, false)
	k8s.sandboxEndpoint = "10.0.0.5:50051"

	w := httptest.NewRecorder()
	c.handleProvisionSandbox(w, callbackReq(http.MethodPost, "/internal/runs/run-1/provision-sandbox", ""))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	if k8s.provisionSandboxCalls != 1 {
		t.Errorf("provisionSandboxCalls = %d, want 1", k8s.provisionSandboxCalls)
	}
}

// seedKubeRunForController is seedKubeRun for the handler tests, which build a
// Controller rather than the service facade.
func seedKubeRunForController(t *testing.T, repo *mockRepository, c *Controller, resolves bool) {
	t.Helper()
	at := seedAgentType(t, repo, AgentType{
		ID: "at-1", TenantID: "t-1", Slug: "bot",
		Connectors: []Connector{kubeConnector("prod-cluster")},
	})
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateActive})

	entries := map[string]struct{ adapter, baseHost string }{}
	if resolves {
		entries["prod-cluster"] = struct{ adapter, baseHost string }{platformconns.AdapterKubernetesBearer, "k8s.example.com:443"}
	}
	c.runs.SetScopeResolver(NewScopeResolver(nil, stubConns{entries: entries}))
}

// errConns models a connection registry that fails rather than returning an
// empty answer, which KubeClusters surfaces as an error instead of unresolved.
type errConns struct{}

func (errConns) ResolveConnection(_ context.Context, _, _ string) (string, string, bool, error) {
	return "", "", false, errors.New("registry unavailable")
}

// On demand it still provisions: the agent is asking for a shell now.
func TestProvisionSandbox_OnDemandProceedsOnResolverError(t *testing.T) {
	svc, repo, k8s, _ := newTestService(t)
	at := seedAgentType(t, repo, AgentType{
		ID: "at-1", TenantID: "t-1", Slug: "bot",
		Connectors: []Connector{kubeConnector("prod-cluster")},
	})
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateActive})
	svc.run.SetScopeResolver(NewScopeResolver(nil, errConns{}))
	k8s.sandboxEndpoint = "10.0.0.5:50051"

	if _, err := svc.run.ProvisionSandbox(context.Background(), "run-1", "", false); err != nil {
		t.Fatalf("on-demand provisioning failed: %v", err)
	}
	if k8s.provisionSandboxCalls != 1 {
		t.Errorf("provisionSandboxCalls = %d, want 1", k8s.provisionSandboxCalls)
	}
}
