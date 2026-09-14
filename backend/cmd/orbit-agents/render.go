package main

import (
	"fmt"
	"os"

	"sigs.k8s.io/yaml"

	"github.com/clear-route/orbit/internal/agents"
	"github.com/clear-route/orbit/internal/agents/manifestpatch"
)

// renderManifests prints the final YAML for every resource orbit-agents would
// provision for the given agent slug, with all manifest patches applied.
// Used by the `orbit-agents -render` CLI mode for pre-deploy previews.
func renderManifests(patchDir, agentSlug, tenantID, namespace string, includeSandbox bool) error {
	if agentSlug == "" {
		return fmt.Errorf("-agent-slug is required")
	}
	patches, err := manifestpatch.Load(patchDir)
	if err != nil {
		return fmt.Errorf("load patches: %w", err)
	}
	if verr := manifestpatch.Validate(patches); verr != nil {
		return fmt.Errorf("validate patches: %w", verr)
	}

	cfg := agents.K8sProvisionerConfig{
		SessionManagerImage: "orbit-session-manager:render",
		SandboxImage:        "orbit-sandbox-exec:render",
		SandboxGRPCPort:     50051,
		SystemNamespace:     "orbit-system",
		Patches:             patches,
	}
	at := agents.AgentType{
		ID:       "render-" + agentSlug,
		Slug:     agentSlug,
		TenantID: tenantID,
	}
	objs, err := agents.RenderResources(cfg, at, namespace, includeSandbox)
	if err != nil {
		return err
	}
	for _, obj := range objs {
		b, err := yaml.Marshal(obj)
		if err != nil {
			return fmt.Errorf("marshal: %w", err)
		}
		fmt.Fprintln(os.Stdout, "---")
		if _, err := os.Stdout.Write(b); err != nil {
			return err
		}
	}
	return nil
}
