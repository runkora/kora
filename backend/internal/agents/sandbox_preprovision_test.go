package agents

import (
	"context"
	"errors"
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

// A pre-provision races the connection registry's load. The pod it creates is
// reused for the rest of the run rather than rebuilt, so persisting one with an
// empty kubeconfig leaves the agent unable to reach any cluster for the whole
// run, with no retry. Deferring costs that run its warm start and nothing else.
func TestProvisionSandbox_AheadDefersWhileRegistryUnresolved(t *testing.T) {
	svc, repo, k8s, _ := newTestService(t)
	seedKubeRun(t, repo, svc, false)
	k8s.sandboxEndpoint = "10.0.0.5:50051"

	_, err := svc.run.ProvisionSandbox(context.Background(), "run-1", true)
	if !errors.Is(err, ErrKubeconfigNotReady) {
		t.Fatalf("error = %v, want ErrKubeconfigNotReady", err)
	}
	if k8s.provisionSandboxCalls != 0 {
		t.Errorf("provisioned a pod anyway (%d calls); a deferred pre-provision must create nothing", k8s.provisionSandboxCalls)
	}
}

// On-demand provisioning is a correctness requirement, not a latency
// optimization: the agent is asking for a shell now. An unresolved registry
// still yields a sandbox, because no kubeconfig beats no sandbox.
func TestProvisionSandbox_OnDemandProceedsWhileRegistryUnresolved(t *testing.T) {
	svc, repo, k8s, _ := newTestService(t)
	seedKubeRun(t, repo, svc, false)
	k8s.sandboxEndpoint = "10.0.0.5:50051"

	sandbox, err := svc.run.ProvisionSandbox(context.Background(), "run-1", false)
	if err != nil {
		t.Fatalf("on-demand provisioning failed: %v", err)
	}
	if sandbox.Endpoint != "10.0.0.5:50051" {
		t.Errorf("endpoint = %q, want 10.0.0.5:50051", sandbox.Endpoint)
	}
}

// Once the registry resolves, a pre-provision is exactly what it was added for.
func TestProvisionSandbox_AheadProvisionsOnceResolved(t *testing.T) {
	svc, repo, k8s, _ := newTestService(t)
	seedKubeRun(t, repo, svc, true)
	k8s.sandboxEndpoint = "10.0.0.5:50051"

	sandbox, err := svc.run.ProvisionSandbox(context.Background(), "run-1", true)
	if err != nil {
		t.Fatalf("pre-provision failed with a resolved registry: %v", err)
	}
	if sandbox.Endpoint != "10.0.0.5:50051" {
		t.Errorf("endpoint = %q, want 10.0.0.5:50051", sandbox.Endpoint)
	}
	if len(k8s.provisionKubeClusters) != 1 || k8s.provisionKubeClusters[0].Name != "prod-cluster" {
		t.Errorf("kubeClusters = %+v, want the resolved prod-cluster entry", k8s.provisionKubeClusters)
	}
}
