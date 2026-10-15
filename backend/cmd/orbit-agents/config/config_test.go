package agentsConfig

import (
	"testing"

	"github.com/clear-route/orbit/internal/platform/config"
)

func TestBaseDefaults_NoLocalRegistryOrTrustDomain(t *testing.T) {
	cfg, err := config.LoadService[Config]("cmd/orbit-agents", "production")
	if err != nil {
		t.Fatalf("load base config: %v", err)
	}
	if cfg.Agents.SessionManagerImage != "ghcr.io/runkora/orbit-session-manager:latest" {
		t.Errorf("sessionManagerImage = %q, want the ghcr.io default", cfg.Agents.SessionManagerImage)
	}
	if cfg.Agents.SandboxImage != "ghcr.io/runkora/kora-sandbox-base:latest" {
		t.Errorf("sandboxImage = %q, want the ghcr.io default", cfg.Agents.SandboxImage)
	}
	if cfg.Platform.TrustDomain != "" {
		t.Errorf("trustDomain = %q, want empty so the chart must render it", cfg.Platform.TrustDomain)
	}
	if cfg.Kubernetes.SystemNamespace != "orbit-system" {
		t.Errorf("systemNamespace = %q, want the orbit-system default", cfg.Kubernetes.SystemNamespace)
	}
	if e := cfg.Agents.Environments; e.DefaultSize != (EnvironmentSize{CPU: "2", Memory: "4Gi", Disk: "10Gi"}) || e.MaxSize != e.DefaultSize {
		t.Errorf("environments = %+v, want the documented default and maximum sizes", e)
	}
	if len(cfg.Agents.ImagePullSecrets) != 0 {
		t.Errorf("imagePullSecrets = %v, want none by default", cfg.Agents.ImagePullSecrets)
	}
}

func TestLocalOverlay_KeepsLocalRegistryAndTrustDomain(t *testing.T) {
	cfg, err := config.LoadService[Config]("cmd/orbit-agents", "local")
	if err != nil {
		t.Fatalf("load local config: %v", err)
	}
	if cfg.Agents.SessionManagerImage != "orbit-registry:5001/orbit-session-manager:latest" {
		t.Errorf("sessionManagerImage = %q, want the local registry image", cfg.Agents.SessionManagerImage)
	}
	if cfg.Agents.SandboxImage != "orbit-registry:5001/kora-sandbox-base:latest" {
		t.Errorf("sandboxImage = %q, want the local registry image", cfg.Agents.SandboxImage)
	}
	if cfg.Platform.TrustDomain != "orbit.test" {
		t.Errorf("trustDomain = %q, want orbit.test", cfg.Platform.TrustDomain)
	}
}

func TestValidate_NodeMaxOldSpaceMB(t *testing.T) {
	for mb, valid := range map[int]bool{512: true, 0: true, -5: false} {
		cfg := &Config{}
		cfg.Agents.SessionManager.NodeMaxOldSpaceMB = mb
		if err := Validate(cfg); (err == nil) != valid {
			t.Errorf("Validate(nodeMaxOldSpaceMB=%d) error = %v, want valid %v", mb, err, valid)
		}
	}
}

func TestValidate_RequiresTrustDomainInCluster(t *testing.T) {
	cfg := &Config{}
	cfg.Kubernetes.InCluster = true
	if err := Validate(cfg); err == nil {
		t.Fatal("empty trust domain in cluster must fail validation")
	}
	cfg.Platform.TrustDomain = "orbit.test"
	if err := Validate(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cfg.Platform.TrustDomain = ""
	cfg.Kubernetes.InCluster = false
	if err := Validate(cfg); err != nil {
		t.Fatalf("local dev without a trust domain must pass: %v", err)
	}
}
