package agents

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/runkora/kora/backend/internal/agents/sandbox/kubeconfig"
	platformconns "github.com/runkora/kora/backend/internal/platform/connections"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/probes"
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
// reach the sandbox as KORA_KUBE_CLUSTERS, and that an agent with none gets no
// env var at all rather than an empty/"null" one.
func TestSandboxContainerEnv_KubeClusters(t *testing.T) {
	cfg := K8sProvisionerConfig{SandboxGRPCPort: 50051, SystemNamespace: "kora-system"}

	env := sandboxContainerEnv(cfg, []kubeconfig.Cluster{{Name: "c", Server: "https://x:443"}}, nil, "/workspaces/run-1", "test-exec-token")
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

	for _, e := range sandboxContainerEnv(cfg, nil, nil, "/workspaces/run-1", "test-exec-token") {
		if e.Name == kubeconfig.ClustersEnvVar {
			t.Errorf("%s must be omitted when the agent has no kubernetes connection, got %q", e.Name, e.Value)
		}
	}
}

// TestSandboxContainer_WorkspaceMountScopedToRunID locks the fix for
// https://github.com/runkora/kora/issues/134: the workspaces PVC is
// shared by every run co-located on the same pod ordinal, so the sandbox
// container must mount it with SubPath: runID at /workspaces/{runID} — never
// the bare PVC root — so a run's sandbox pod cannot see a sibling run's
// directory.
func TestSandboxContainer_WorkspaceMountScopedToRunID(t *testing.T) {
	cfg := K8sProvisionerConfig{SandboxGRPCPort: 50051, SystemNamespace: "kora-system"}
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
	// The agent pod's TMPDIR is the run's own .tmp/{runID}, outside the directory the agent can rename.
	want := map[string]string{wantMountPath: runID, "/workspaces/.tmp/" + runID: ".tmp/" + runID}
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

// The sandbox reports Ready as soon as its port opens. A startup probe checks
// every second until then, and the readiness probe has no initial delay.
func TestSandboxContainer_StartupProbe(t *testing.T) {
	cfg := K8sProvisionerConfig{SandboxGRPCPort: 50051, SystemNamespace: "kora-system"}
	c, err := sandboxContainer(cfg, testSandboxSpec(), "run-1", "token")
	if err != nil {
		t.Fatal(err)
	}
	sp, rp := c.StartupProbe, c.ReadinessProbe
	if sp == nil || sp.TCPSocket == nil || sp.TCPSocket.Port.IntValue() != 50051 {
		t.Fatalf("startup probe = %+v, want a TCP check on the gRPC port", sp)
	}
	if sp.PeriodSeconds != 1 || sp.InitialDelaySeconds != 0 {
		t.Errorf("startup period/delay = %d/%d, want 1/0", sp.PeriodSeconds, sp.InitialDelaySeconds)
	}
	if got := sp.FailureThreshold * sp.PeriodSeconds; got != probes.DefaultStartupSeconds {
		t.Errorf("startup allows %ds, want the default %ds", got, probes.DefaultStartupSeconds)
	}
	cfg.SandboxStartupSeconds = 300
	c, err = sandboxContainer(cfg, testSandboxSpec(), "run-1", "token")
	if err != nil {
		t.Fatal(err)
	}
	if got := c.StartupProbe.FailureThreshold * c.StartupProbe.PeriodSeconds; got != 300 {
		t.Errorf("startup allows %ds with an override of 300s", got)
	}
	if rp.InitialDelaySeconds != 0 {
		t.Errorf("readiness initial delay = %d, want 0", rp.InitialDelaySeconds)
	}
}

// placeholderConns is a connectionResolver that also serves placeholderEnv.
type placeholderConns struct {
	stubConns
	envs map[string]string
}

func (p placeholderConns) PlaceholderEnv(_ context.Context, _, name string) (string, bool, error) {
	env, ok := p.envs[name]
	return env, ok, nil
}

func TestPlaceholderEnvs_OnlyGrantedConnections(t *testing.T) {
	r := NewScopeResolver(nil, placeholderConns{envs: map[string]string{"npm": "NPM_TOKEN", "other": "OTHER_TOKEN"}})
	conn := func(n string) Connector { return Connector{Connection: n, Actions: []string{"read"}} }

	got, err := r.PlaceholderEnvs(context.Background(), "t1", []Connector{conn("npm"), conn("npm"), conn("missing")})
	if err != nil {
		t.Fatalf("PlaceholderEnvs: %v", err)
	}
	if len(got) != 1 || got[0] != "NPM_TOKEN" {
		t.Errorf("granted = %v, want only NPM_TOKEN (OTHER_TOKEN is not granted)", got)
	}

	none, err := r.PlaceholderEnvs(context.Background(), "t1", nil)
	if err != nil || len(none) != 0 {
		t.Errorf("no grant = (%v, %v), want none", none, err)
	}
	if got, _ := NewScopeResolver(nil, stubConns{}).PlaceholderEnvs(context.Background(), "t1", []Connector{conn("npm")}); got != nil {
		t.Errorf("resolver without placeholder support = %v, want nil", got)
	}
}

func TestSandboxContainerEnv_PlaceholderEnvs(t *testing.T) {
	cfg := K8sProvisionerConfig{SandboxGRPCPort: 50051, SystemNamespace: "kora-system"}
	value := func(env []corev1.EnvVar, name string) (string, bool) {
		for _, e := range env {
			if e.Name == name {
				return e.Value, true
			}
		}
		return "", false
	}

	env := sandboxContainerEnv(cfg, nil, []string{"NPM_TOKEN", "GH_TOKEN"}, "/workspaces/run-1", "exec")
	if v, ok := value(env, "NPM_TOKEN"); !ok || v != CredentialPlaceholder {
		t.Errorf("NPM_TOKEN = %q (set %v), want the placeholder", v, ok)
	}
	n := 0
	for _, e := range env {
		if e.Name == "GH_TOKEN" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("GH_TOKEN set %d times, want once", n)
	}

	if _, ok := value(sandboxContainerEnv(cfg, nil, nil, "/workspaces/run-1", "exec"), "NPM_TOKEN"); ok {
		t.Error("NPM_TOKEN set without the grant")
	}
}

// TestCatalogPlaceholderEnv_NoRealToken locks that the catalog reads the
// variable name only: a registry entry carrying a secret-looking value never
// reaches the sandbox env.
func TestCatalogPlaceholderEnv_NoRealToken(t *testing.T) {
	const real = "real-token-must-not-leak"
	path := filepath.Join(t.TempDir(), "connections.json")
	registry := `[
	 {"tenant_id":"t1","name":"npm","adapter":"bearer","base_host":"registry.npmjs.org","secret_ref":"` + real + `","config":{"placeholderEnv":"NPM_TOKEN"}},
	 {"tenant_id":"t1","name":"plain","adapter":"bearer","base_host":"a.example.com","config":{}},
	 {"tenant_id":"t1","name":"gh","adapter":"github_app","base_host":"github.com","config":{"placeholderEnv":"GHX"}},
	 {"tenant_id":"t2","name":"npm2","adapter":"bearer","base_host":"b.example.com","config":{"placeholderEnv":"T2_TOKEN"}}]`
	if err := os.WriteFile(path, []byte(registry), 0o600); err != nil {
		t.Fatal(err)
	}
	cat := NewConnectionCatalog(path, logging.NewLogger())
	r := NewScopeResolver(nil, cat)

	cs := []Connector{{Connection: "npm"}, {Connection: "plain"}, {Connection: "gh"}, {Connection: "npm2"}}
	envs, err := r.PlaceholderEnvs(context.Background(), "t1", cs)
	if err != nil {
		t.Fatal(err)
	}
	if len(envs) != 1 || envs[0] != "NPM_TOKEN" {
		t.Fatalf("envs = %v, want only NPM_TOKEN (not unset, non-bearer or other-tenant)", envs)
	}
	cfg := K8sProvisionerConfig{SandboxGRPCPort: 50051, SystemNamespace: "kora-system"}
	for _, e := range sandboxContainerEnv(cfg, nil, envs, "/workspaces/run-1", "exec") {
		if strings.Contains(e.Value, real) {
			t.Errorf("real token reached sandbox env %s", e.Name)
		}
	}
}

func TestValidPlaceholderEnvName(t *testing.T) {
	for _, ok := range []string{"NPM_TOKEN", "_x1"} {
		if !validPlaceholderEnvName(ok) {
			t.Errorf("%q rejected", ok)
		}
	}
	for _, bad := range []string{"", "1A", "A=B", "A B", "PATH", "LD_PRELOAD", "KORA_X", "a-b"} {
		if validPlaceholderEnvName(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
	cfg := K8sProvisionerConfig{SandboxGRPCPort: 50051, SystemNamespace: "kora-system"}
	for _, e := range sandboxContainerEnv(cfg, nil, []string{"LD_PRELOAD", "A=B"}, "/workspaces/r", "exec") {
		if e.Name == "LD_PRELOAD" || e.Name == "A=B" {
			t.Errorf("invalid name %q set", e.Name)
		}
	}
}
