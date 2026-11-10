package agentsConfig

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/runkora/kora/backend/internal/platform/config"
)

func TestBaseDefaults_NoLocalRegistryOrTrustDomain(t *testing.T) {
	cfg, err := config.LoadService[Config]("cmd/kora-agents", "production")
	if err != nil {
		t.Fatalf("load base config: %v", err)
	}
	if cfg.Agents.SessionManagerImage != "ghcr.io/runkora/kora-session-manager:latest" {
		t.Errorf("sessionManagerImage = %q, want the ghcr.io default", cfg.Agents.SessionManagerImage)
	}
	if cfg.Agents.SandboxImage != "ghcr.io/runkora/kora-sandbox-base:latest" {
		t.Errorf("sandboxImage = %q, want the ghcr.io default", cfg.Agents.SandboxImage)
	}
	if cfg.Platform.TrustDomain != "" {
		t.Errorf("trustDomain = %q, want empty so the chart must render it", cfg.Platform.TrustDomain)
	}
	if cfg.Kubernetes.SystemNamespace != "kora-system" {
		t.Errorf("systemNamespace = %q, want the kora-system default", cfg.Kubernetes.SystemNamespace)
	}
	if e := cfg.Agents.Environments; e.DefaultSize != (EnvironmentSize{CPU: "2", Memory: "4Gi", Disk: "10Gi"}) || e.MaxSize != e.DefaultSize {
		t.Errorf("environments = %+v, want the documented default and maximum sizes", e)
	}
	if len(cfg.Agents.ImagePullSecrets) != 0 {
		t.Errorf("imagePullSecrets = %v, want none by default", cfg.Agents.ImagePullSecrets)
	}
}

func TestLocalOverlay_KeepsLocalRegistryAndTrustDomain(t *testing.T) {
	cfg, err := config.LoadService[Config]("cmd/kora-agents", "local")
	if err != nil {
		t.Fatalf("load local config: %v", err)
	}
	if cfg.Agents.SessionManagerImage != "kora-registry:5001/kora-session-manager:latest" {
		t.Errorf("sessionManagerImage = %q, want the local registry image", cfg.Agents.SessionManagerImage)
	}
	if cfg.Agents.SandboxImage != "kora-registry:5001/kora-sandbox-base:latest" {
		t.Errorf("sandboxImage = %q, want the local registry image", cfg.Agents.SandboxImage)
	}
	if cfg.Platform.TrustDomain != "runkora.test" {
		t.Errorf("trustDomain = %q, want runkora.test", cfg.Platform.TrustDomain)
	}
}

func TestValidate_Landlock(t *testing.T) {
	for mode, valid := range map[string]bool{"enforce": true, "bestEffort": true, "": true, "off": false} {
		cfg := validConfig()
		cfg.Agents.SessionManager.Landlock = mode
		if err := Validate(cfg); (err == nil) != valid {
			t.Errorf("Validate(landlock=%q) error = %v, want valid %v", mode, err, valid)
		}
	}
}

func TestValidate_RequiresTrustDomainInCluster(t *testing.T) {
	cfg := validConfig()
	cfg.Kubernetes.InCluster = true
	if err := Validate(cfg); err == nil {
		t.Fatal("empty trust domain in cluster must fail validation")
	}
	cfg.Platform.TrustDomain = "runkora.test"
	if err := Validate(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cfg.Platform.TrustDomain = ""
	cfg.Kubernetes.InCluster = false
	if err := Validate(cfg); err != nil {
		t.Fatalf("local dev without a trust domain must pass: %v", err)
	}
}

// validConfig passes Validate, for tests that change one field.
func validConfig() *Config {
	cfg := &Config{}
	cfg.Agents.Scheduling.StartingRunAllowance = StartingRunAllowance{
		ClaudeCode: resource.MustParse("180Mi"), Codex: resource.MustParse("60Mi"), OpenCode: resource.MustParse("440Mi"),
	}
	return cfg
}

// Every harness needs a positive allowance, or kora-agents does not start.
func TestValidate_StartingRunAllowance(t *testing.T) {
	for name, set := range map[string]func(*StartingRunAllowance){
		"codex missing":       func(a *StartingRunAllowance) { a.Codex = resource.Quantity{} },
		"openCode zero":       func(a *StartingRunAllowance) { a.OpenCode = resource.MustParse("0") },
		"claudeCode negative": func(a *StartingRunAllowance) { a.ClaudeCode = resource.MustParse("-1Mi") },
	} {
		cfg := validConfig()
		set(&cfg.Agents.Scheduling.StartingRunAllowance)
		if err := Validate(cfg); err == nil {
			t.Errorf("%s: Validate passed, want an error", name)
		}
	}
	if err := Validate(validConfig()); err != nil {
		t.Errorf("Validate(valid) = %v", err)
	}
}
