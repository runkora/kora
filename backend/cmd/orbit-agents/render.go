package main

import (
	"fmt"
	"os"
	"strconv"

	"sigs.k8s.io/yaml"

	"github.com/clear-route/orbit/internal/agents"
	"github.com/clear-route/orbit/internal/agents/manifestpatch"
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt32(key string, def int32) int32 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 32)
	if err != nil {
		return def
	}
	return int32(n)
}

// renderManifests prints the final YAML for every resource orbit-agents would
// provision for the given agent slug, with all manifest patches applied.
// Used by the `orbit-agents -render` CLI mode for pre-deploy previews.
//
// Image/port/system-namespace defaults are overridable via env vars so a
// preview run under `helm template | orbit-agents -render` reflects the same
// config the runtime would see:
//
//	ORBIT_SESSION_MANAGER_IMAGE, ORBIT_SANDBOX_IMAGE, ORBIT_SANDBOX_GRPC_PORT,
//	ORBIT_SYSTEM_NAMESPACE, ORBIT_SANDBOX_GIT_AUTHOR_NAME,
//	ORBIT_SANDBOX_GIT_AUTHOR_EMAIL.
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
		SessionManagerImage: envOr("ORBIT_SESSION_MANAGER_IMAGE", "orbit-session-manager:render"),
		SandboxImage:        envOr("ORBIT_SANDBOX_IMAGE", "orbit-sandbox-exec:render"),
		SandboxGRPCPort:     envInt32("ORBIT_SANDBOX_GRPC_PORT", 50051),
		SystemNamespace:     envOr("ORBIT_SYSTEM_NAMESPACE", "orbit-system"),
		GitAuthorName:       os.Getenv("ORBIT_SANDBOX_GIT_AUTHOR_NAME"),
		GitAuthorEmail:      os.Getenv("ORBIT_SANDBOX_GIT_AUTHOR_EMAIL"),
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
		if _, err := fmt.Fprintln(os.Stdout, "---"); err != nil {
			return fmt.Errorf("write separator: %w", err)
		}
		if _, err := os.Stdout.Write(b); err != nil {
			return fmt.Errorf("write manifest: %w", err)
		}
	}
	return nil
}
