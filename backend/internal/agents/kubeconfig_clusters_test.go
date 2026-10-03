package agents

import (
	"context"
	"testing"

	"github.com/clear-route/orbit/internal/agents/sandbox/kubeconfig"
	platformconns "github.com/clear-route/orbit/internal/platform/connections"
)

// stubConns is a connectionResolver whose registry can be empty (nothing
// resolves) to model the window before connections.json is mounted.
type stubConns struct {
	entries map[string]struct{ adapter, baseHost string }
}

func (s stubConns) ResolveConnection(_ context.Context, _, name string) (string, string, bool, error) {
	e, ok := s.entries[name]
	if !ok {
		return "", "", false, nil
	}
	return e.adapter, e.baseHost, true, nil
}

func kubeConnector(name string) Connector {
	return Connector{Connection: name, Actions: []string{"cluster:read"}}
}

// TestKubeClusters_OnlyKubernetesAdapters locks that the adapter check is an
// explicit allowlist: a github_app connector contributes no kubeconfig entry,
// and doing so is not "unresolved".
func TestKubeClusters_OnlyKubernetesAdapters(t *testing.T) {
	r := NewScopeResolver(nil, stubConns{entries: map[string]struct{ adapter, baseHost string }{
		"prod-cluster": {platformconns.AdapterKubernetesBearer, "k8s.example.com:443"},
		"github":       {platformconns.AdapterGitHubApp, "github.com"},
	}})

	at := AgentType{TenantID: "t1", Connectors: []Connector{kubeConnector("prod-cluster"), kubeConnector("github")}}
	clusters, unresolved, err := r.KubeClusters(context.Background(), at.TenantID, at.Connectors)
	if err != nil {
		t.Fatalf("KubeClusters: %v", err)
	}
	if unresolved != 0 {
		t.Errorf("unresolved = %d, want 0 — both connections resolve", unresolved)
	}
	if len(clusters) != 1 || clusters[0].Name != "prod-cluster" {
		t.Fatalf("clusters = %+v, want only the kubernetes connection", clusters)
	}
	if clusters[0].Server != "https://k8s.example.com:443" {
		t.Errorf("server = %q, want the https URL", clusters[0].Server)
	}
}

// TestKubeClusters_UnresolvedIsNotTheSameAsNone is the distinction the warm-up
// path depends on. A registry that has not loaded resolves nothing, which must
// NOT be mistaken for "this agent has no Kubernetes connection" — otherwise a
// sandbox is warmed with no kubeconfig and then reused for the whole run.
func TestKubeClusters_UnresolvedIsNotTheSameAsNone(t *testing.T) {
	cold := NewScopeResolver(nil, stubConns{}) // registry not mounted yet
	clusters, unresolved, err := cold.KubeClusters(context.Background(), "t1", []Connector{kubeConnector("prod-cluster")})
	if err != nil {
		t.Fatalf("KubeClusters: %v", err)
	}
	if len(clusters) != 0 {
		t.Errorf("clusters = %+v, want none from a cold registry", clusters)
	}
	if unresolved != 1 {
		t.Errorf("unresolved = %d, want 1 — the caller must be able to tell this apart from an agent with no k8s connector", unresolved)
	}

	// Contrast: a loaded registry, agent holds only a github connector. Nothing
	// to render, but nothing unresolved either — warm-up must proceed.
	warm := NewScopeResolver(nil, stubConns{entries: map[string]struct{ adapter, baseHost string }{
		"github": {platformconns.AdapterGitHubApp, "github.com"},
	}})
	clusters, unresolved, err = warm.KubeClusters(context.Background(), "t1", []Connector{kubeConnector("github")})
	if err != nil {
		t.Fatalf("KubeClusters: %v", err)
	}
	if len(clusters) != 0 || unresolved != 0 {
		t.Errorf("clusters=%+v unresolved=%d, want none/0", clusters, unresolved)
	}
}

func TestKubeClusters_NoResolverWired(t *testing.T) {
	r := NewScopeResolver(nil, nil)
	clusters, unresolved, err := r.KubeClusters(context.Background(), "t1", []Connector{kubeConnector("c")})
	if err != nil || clusters != nil || unresolved != 0 {
		t.Errorf("KubeClusters with no resolver = (%v, %d, %v), want (nil, 0, nil)", clusters, unresolved, err)
	}
}

// TestSandboxContainerEnv_KubeClusters locks that the resolved clusters actually
// reach the sandbox as ORBIT_KUBE_CLUSTERS, and that an agent with none gets no
// env var at all rather than an empty/"null" one.
func TestSandboxContainerEnv_KubeClusters(t *testing.T) {
	cfg := K8sProvisionerConfig{SandboxGRPCPort: 50051, SystemNamespace: "orbit-system"}

	env := sandboxContainerEnv(cfg, []kubeconfig.Cluster{{Name: "c", Server: "https://x:443"}}, "/workspaces/run-1", "test-exec-token")
	var got string
	for _, e := range env {
		if e.Name == kubeconfig.ClustersEnvVar {
			got = e.Value
		}
	}
	if got == "" {
		t.Fatalf("%s not set on the sandbox container", kubeconfig.ClustersEnvVar)
	}
	clusters, err := kubeconfig.Decode(got)
	if err != nil || len(clusters) != 1 || clusters[0].Name != "c" {
		t.Errorf("decoded %+v (err %v), want the one cluster", clusters, err)
	}

	for _, e := range sandboxContainerEnv(cfg, nil, "/workspaces/run-1", "test-exec-token") {
		if e.Name == kubeconfig.ClustersEnvVar {
			t.Errorf("%s must be omitted when the agent has no kubernetes connection, got %q", e.Name, e.Value)
		}
	}
}

// TestSandboxContainer_WorkspaceMountScopedToRunID locks the fix for
// https://github.com/clear-route/orbit/issues/134: the workspaces PVC is
// shared by every run co-located on the same pod ordinal, so the sandbox
// container must mount it with SubPath: runID at /workspaces/{runID} — never
// the bare PVC root — so a run's sandbox pod cannot see a sibling run's
// directory.
func TestSandboxContainer_WorkspaceMountScopedToRunID(t *testing.T) {
	cfg := K8sProvisionerConfig{SandboxGRPCPort: 50051, SystemNamespace: "orbit-system"}
	runID := "run-abc-123"

	c, err := sandboxContainer(cfg, testSandboxSpec(), runID, "test-exec-token")
	if err != nil {
		t.Fatal(err)
	}

	subPaths := map[string]string{}
	for _, m := range c.VolumeMounts {
		if m.Name == "workspaces" {
			subPaths[m.MountPath] = m.SubPath
		}
	}
	wantMountPath := "/workspaces/" + runID
	// /tmp is the run's own .tmp/{runID}, outside the directory the agent can rename.
	want := map[string]string{wantMountPath: runID, "/tmp": ".tmp/" + runID, "/workspaces/.tmp/" + runID: ".tmp/" + runID}
	if len(subPaths) != len(want) {
		t.Errorf("workspaces mounts = %v, want %v", subPaths, want)
	}
	for mountPath, subPath := range want {
		if got, ok := subPaths[mountPath]; !ok || got != subPath {
			t.Errorf("workspaces mount at %q has SubPath %q, want %q (an unscoped SubPath exposes every sibling run's directory)", mountPath, got, subPath)
		}
	}
	if c.WorkingDir != wantMountPath {
		t.Errorf("WorkingDir = %q, want the run's workspace %q", c.WorkingDir, wantMountPath)
	}

	var gotWorkspace string
	for _, e := range c.Env {
		if e.Name == "WORKSPACE" {
			gotWorkspace = e.Value
		}
	}
	if gotWorkspace != wantMountPath {
		t.Errorf("WORKSPACE env = %q, want %q", gotWorkspace, wantMountPath)
	}
}
