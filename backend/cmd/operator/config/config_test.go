package operatorConfig

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/platform/config"
)

func TestBaseDefaults(t *testing.T) {
	cfg, err := config.LoadService[Config]("cmd/operator", "production")
	if err != nil {
		t.Fatalf("load base config: %v", err)
	}
	in := cfg.Install
	if in.SystemNamespace != "kora-system" || in.PodSecurity.Enforce != "restricted" || in.Injection.BundlePollingMaxDelay != 60 {
		t.Errorf("install defaults = %+v", in)
	}
	if in.SandboxImages.BaseImage != "ghcr.io/runkora/kora-sandbox-base:latest" || in.SandboxImages.WarmBase {
		t.Errorf("sandboxImages defaults = %+v", in.SandboxImages)
	}
	if in.TrustDomain != "" {
		t.Errorf("trustDomain = %q, want empty so the chart must render it", in.TrustDomain)
	}
}

func TestPreflightInterval_HasAFloor(t *testing.T) {
	for secs, want := range map[int]time.Duration{0: 10 * time.Second, 5: 10 * time.Second, 300: 5 * time.Minute} {
		if got := (Preflight{IntervalSeconds: secs}).Interval(); got != want {
			t.Errorf("Interval(%d) = %s, want %s", secs, got, want)
		}
	}
}

// Registry mirrors and the warmer placement decode from YAML, Kubernetes types included.
func TestSandboxImages_DecodeFromYAML(t *testing.T) {
	dir := t.TempDir()
	write := func(path, body string) {
		t.Helper()
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example\n")
	write("cmd/operator/config/base.yaml", `
install:
  sandboxImages:
    baseImage: kora-registry:5001/kora-sandbox-base:latest
    warmBase: true
    registryMirrors:
      kora-registry:5001: http://kora-registry:5000
    warmerPlacement:
      nodeSelector:
        pool: sandbox
      tolerations:
        - key: sandbox
          operator: Exists
          effect: NoSchedule
      affinity:
        nodeAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            nodeSelectorTerms:
              - matchExpressions:
                  - key: pool
                    operator: In
                    values: [sandbox]
      priorityClassName: sandbox
    bundled:
      go:
        enabled: true
        warm: true
        image: kora-registry:5001/kora-sandbox-go:latest
      node:
        enabled: false
        warm: false
        image: kora-registry:5001/kora-sandbox-node:latest
`)
	t.Chdir(dir)

	cfg, err := config.LoadService[Config]("cmd/operator", "production")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	si := cfg.Install.SandboxImages
	if !si.WarmBase || si.RegistryMirrors["kora-registry:5001"] != "http://kora-registry:5000" {
		t.Errorf("sandboxImages = %+v", si)
	}
	p := si.WarmerPlacement
	if p.NodeSelector["pool"] != "sandbox" || p.PriorityClassName != "sandbox" || len(p.Tolerations) != 1 || p.Tolerations[0].Effect != "NoSchedule" {
		t.Errorf("warmerPlacement = %+v", p)
	}
	if p.Affinity == nil || p.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions[0].Values[0] != "sandbox" {
		t.Errorf("affinity = %+v", p.Affinity)
	}
	if g := si.Bundled["go"]; !g.Enabled || !g.Warm || g.Image != "kora-registry:5001/kora-sandbox-go:latest" {
		t.Errorf("bundled go = %+v", g)
	}
	if n := si.Bundled["node"]; n.Enabled || n.Image != "kora-registry:5001/kora-sandbox-node:latest" {
		t.Errorf("bundled node = %+v", n)
	}
}

// Sandbox sidecar resources decode from the chart's rendered JSON.
func TestSandboxSidecars_DecodeFromYAML(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "cmd/operator/config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	body := `install:
  injection:
    sandboxSidecars: {"istioProxy":{"cpu":"200m","memory":"128Mi"},"identityProxy":{"cpu":"200m","memory":"64Mi"},"opa":{"cpu":"500m","memory":"128Mi"}}
`
	if err := os.WriteFile(filepath.Join(dir, "cmd/operator/config/base.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	cfg, err := config.LoadService[Config]("cmd/operator", "production")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	sc := cfg.Install.Injection.SandboxSidecars
	if sc.IstioProxy.Cpu().String() != "200m" || sc.IdentityProxy.Memory().String() != "64Mi" || sc.OPA.Cpu().String() != "500m" {
		t.Errorf("sandboxSidecars = %+v", sc)
	}
}
