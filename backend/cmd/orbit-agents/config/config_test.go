package agentsConfig

import (
	"reflect"
	"testing"

	"github.com/clear-route/orbit/internal/platform/config"
)

func TestBaseDefaults_NoLocalRegistryOrTrustDomain(t *testing.T) {
	cfg, err := config.LoadService[Config]("cmd/orbit-agents", "production")
	if err != nil {
		t.Fatalf("load base config: %v", err)
	}
	if cfg.Agents.SessionManagerImage != "ghcr.io/clear-route/orbit-session-manager:latest" {
		t.Errorf("sessionManagerImage = %q, want the ghcr.io default", cfg.Agents.SessionManagerImage)
	}
	if cfg.Agents.SandboxImage != "ghcr.io/clear-route/orbit-sandbox-exec:latest" {
		t.Errorf("sandboxImage = %q, want the ghcr.io default", cfg.Agents.SandboxImage)
	}
	if cfg.Platform.TrustDomain != "" {
		t.Errorf("trustDomain = %q, want empty so ORBIT_TRUST_DOMAIN is required in-cluster", cfg.Platform.TrustDomain)
	}
	if cfg.Kubernetes.SystemNamespace != "orbit-system" {
		t.Errorf("systemNamespace = %q, want the orbit-system default", cfg.Kubernetes.SystemNamespace)
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
	if cfg.Agents.SandboxImage != "orbit-registry:5001/orbit-sandbox-exec:latest" {
		t.Errorf("sandboxImage = %q, want the local registry image", cfg.Agents.SandboxImage)
	}
	if cfg.Platform.TrustDomain != "orbit.test" {
		t.Errorf("trustDomain = %q, want orbit.test", cfg.Platform.TrustDomain)
	}
}

func TestApplyEnvOverrides(t *testing.T) {
	env := map[string]string{
		"ORBIT_SYSTEM_NAMESPACE":                             "orbit-prod",
		"ORBIT_TRUST_DOMAIN":                                 "orbit.example.com",
		"ORBIT_SESSION_MANAGER_IMAGE":                        "registry.example.com/orbit-session-manager:1.2.3",
		"ORBIT_SANDBOX_IMAGE":                                "registry.example.com/orbit-sandbox-exec:1.2.3",
		"ORBIT_SANDBOX_GIT_AUTHOR_NAME":                      "Orbit Agent",
		"ORBIT_SANDBOX_GIT_AUTHOR_EMAIL":                     "agent@example.com",
		"ORBIT_AGENTS_IMAGE_PULL_SECRETS":                    "ghcr-pull, private-pull,,",
		"ORBIT_SESSION_MANAGER_AUTHORIZATION_POLICY_ENABLED": "false",
		"ORBIT_MANIFEST_PATCH_DIR":                           "/etc/custom/patches",
	}
	cfg := &Config{}
	cfg.Kubernetes.SystemNamespace = "orbit-system"
	cfg.Agents.SessionManager.AuthorizationPolicyEnabled = true
	applyEnvOverrides(cfg, func(k string) string { return env[k] })

	if cfg.Kubernetes.SystemNamespace != "orbit-prod" {
		t.Errorf("SystemNamespace = %q", cfg.Kubernetes.SystemNamespace)
	}
	if cfg.Platform.TrustDomain != "orbit.example.com" {
		t.Errorf("TrustDomain = %q", cfg.Platform.TrustDomain)
	}
	if cfg.Agents.SessionManagerImage != env["ORBIT_SESSION_MANAGER_IMAGE"] {
		t.Errorf("SessionManagerImage = %q", cfg.Agents.SessionManagerImage)
	}
	if cfg.Agents.SandboxImage != env["ORBIT_SANDBOX_IMAGE"] {
		t.Errorf("SandboxImage = %q", cfg.Agents.SandboxImage)
	}
	if cfg.Agents.SandboxGitAuthorName != "Orbit Agent" || cfg.Agents.SandboxGitAuthorEmail != "agent@example.com" {
		t.Errorf("git author = %q <%q>", cfg.Agents.SandboxGitAuthorName, cfg.Agents.SandboxGitAuthorEmail)
	}
	if want := []string{"ghcr-pull", "private-pull"}; !reflect.DeepEqual(cfg.Agents.ImagePullSecrets, want) {
		t.Errorf("ImagePullSecrets = %v, want %v", cfg.Agents.ImagePullSecrets, want)
	}
	if cfg.Agents.SessionManager.AuthorizationPolicyEnabled {
		t.Error("AuthorizationPolicyEnabled should be overridden to false")
	}
	if cfg.Agents.ManifestPatchDir != "/etc/custom/patches" {
		t.Errorf("ManifestPatchDir = %q", cfg.Agents.ManifestPatchDir)
	}
}

func TestApplyEnvOverrides_NodeMaxOldSpaceMB(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		want      int
		wantValid bool
	}{
		{name: "unset keeps the YAML value", value: "", want: 512, wantValid: true},
		{name: "set", value: "1024", want: 1024, wantValid: true},
		{name: "zero disables the cap", value: "0", want: 0, wantValid: true},
		{name: "negative", value: "-5", want: -5},
		{name: "not a number", value: "1Gi", want: -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{}
			cfg.Agents.SessionManager.NodeMaxOldSpaceMB = 512
			env := map[string]string{"ORBIT_SESSION_MANAGER_NODE_MAX_OLD_SPACE_MB": tt.value}
			applyEnvOverrides(cfg, func(k string) string { return env[k] })
			if got := cfg.Agents.SessionManager.NodeMaxOldSpaceMB; got != tt.want {
				t.Errorf("NodeMaxOldSpaceMB = %d, want %d", got, tt.want)
			}
			if err := Validate(cfg); (err == nil) != tt.wantValid {
				t.Errorf("Validate() error = %v, want valid %v", err, tt.wantValid)
			}
		})
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

func TestApplyEnvOverrides_UnsetLeavesYAMLValues(t *testing.T) {
	cfg := &Config{}
	cfg.Kubernetes.SystemNamespace = "orbit-system"
	cfg.Agents.SandboxImage = "from-yaml"
	cfg.Agents.ImagePullSecrets = []string{"from-yaml"}
	applyEnvOverrides(cfg, func(string) string { return "" })

	if cfg.Kubernetes.SystemNamespace != "orbit-system" || cfg.Agents.SandboxImage != "from-yaml" {
		t.Errorf("unset env vars must not clear YAML values: %+v", cfg.Agents)
	}
	if !reflect.DeepEqual(cfg.Agents.ImagePullSecrets, []string{"from-yaml"}) {
		t.Errorf("ImagePullSecrets = %v, want the YAML value kept", cfg.Agents.ImagePullSecrets)
	}
}
