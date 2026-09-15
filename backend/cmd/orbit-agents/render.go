package main

import (
	"fmt"
	"os"

	"sigs.k8s.io/yaml"

	agentsConfig "github.com/clear-route/orbit/cmd/orbit-agents/config"
	"github.com/clear-route/orbit/internal/agents"
	"github.com/clear-route/orbit/internal/agents/manifestpatch"
)

// provisionerConfig builds the provisioner config from the service config, so
// the server and -render produce the same manifests.
func provisionerConfig(cfg *agentsConfig.Config, patches []manifestpatch.Patch) agents.K8sProvisionerConfig {
	return agents.K8sProvisionerConfig{
		SessionManagerImage:        cfg.Agents.SessionManagerImage,
		SandboxImage:               cfg.Agents.SandboxImage,
		SandboxGRPCPort:            int32(cfg.Agents.SandboxGRPCPort), //nolint:gosec
		GitAuthorName:              cfg.Agents.SandboxGitAuthorName,
		GitAuthorEmail:             cfg.Agents.SandboxGitAuthorEmail,
		SessionManagerPostgresSSL:  cfg.Agents.SessionManager.PostgresSSLMode,
		ImagePullSecrets:           cfg.Agents.ImagePullSecrets,
		TrustDomain:                cfg.Platform.TrustDomain,
		SystemNamespace:            cfg.Kubernetes.SystemNamespace,
		AuthorizationPolicyEnabled: cfg.Agents.SessionManager.AuthorizationPolicyEnabled,
		Patches:                    patches,
	}
}

// validationAgentType is a stand-in agent type used to render every patchable
// resource at startup, so a patch that fails to apply stops the pod at boot.
var validationAgentType = agents.AgentType{ID: "validate", Slug: "validate", TenantID: "validate"}

// loadPatches reads the patches in dir and renders every patchable resource
// once to prove they apply.
func loadPatches(cfg *agentsConfig.Config, dir string) ([]manifestpatch.Patch, error) {
	patches, err := manifestpatch.Load(dir)
	if err != nil {
		return nil, fmt.Errorf("load manifest patches from %s: %w", dir, err)
	}
	if _, err := agents.RenderResources(provisionerConfig(cfg, patches), validationAgentType, "tenant-validate", true); err != nil {
		return nil, fmt.Errorf("validate manifest patches in %s: %w", dir, err)
	}
	return patches, nil
}

// renderManifests prints the YAML orbit-agents would provision for one agent
// type with every manifest patch applied. It backs the -render CLI mode.
func renderManifests(patchDir string, at agents.AgentType, namespace string, includeSandbox bool) error {
	if at.Slug == "" {
		return fmt.Errorf("-agent-slug is required")
	}
	cfg := agentsConfig.Current
	patches, err := loadPatches(cfg, patchDir)
	if err != nil {
		return err
	}
	objs, err := agents.RenderResources(provisionerConfig(cfg, patches), at, namespace, includeSandbox)
	if err != nil {
		return err
	}
	for _, obj := range objs {
		b, err := yaml.Marshal(obj)
		if err != nil {
			return fmt.Errorf("marshal: %w", err)
		}
		if _, err := fmt.Fprintln(os.Stdout, "---"); err != nil {
			return fmt.Errorf("write separator: %w", err)
		}
		if _, err := os.Stdout.Write(b); err != nil {
			return fmt.Errorf("write manifest: %w", err)
		}
	}
	return nil
}
