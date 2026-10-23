package main

import (
	"errors"
	"fmt"
	"os"

	"sigs.k8s.io/yaml"

	agentsConfig "github.com/runkora/kora/backend/cmd/kora-agents/config"
	"github.com/runkora/kora/backend/internal/agents"
	"github.com/runkora/kora/backend/internal/agents/manifestpatch"
	"github.com/runkora/kora/backend/internal/platform/brand"
	"github.com/runkora/kora/backend/internal/platform/observability"
	"github.com/runkora/kora/backend/internal/platform/ports"
)

// workloadsFile is the shape of the ConfigMap the chart renders from
// agents.workloads and agents.workspace.
type workloadsFile struct {
	Workloads agents.WorkloadsConfig `json:"workloads"`
	Workspace agents.WorkspaceConfig `json:"workspace"`
}

// loadWorkloads reads the workload customization file. A missing file is not an
// error: it means the chart rendered no customization, or this is local dev
// without the mount. A malformed or rejected one fails startup, since silently
// provisioning agents on the wrong nodes or storage is worse than not starting.
func loadWorkloads(path string) (workloadsFile, error) {
	var out workloadsFile
	if path == "" {
		return out, nil
	}
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, fmt.Errorf("read workload customization %s: %w", path, err)
	}
	if err := yaml.Unmarshal(body, &out); err != nil {
		return out, fmt.Errorf("parse workload customization %s: %w", path, err)
	}
	if err := out.Workloads.Validate(); err != nil {
		return out, fmt.Errorf("invalid workload customization in %s: %w", path, err)
	}
	return out, nil
}

// provisionerConfig builds the provisioner config from the service config, so
// the server and -render produce the same manifests.
func provisionerConfig(cfg *agentsConfig.Config, patches []manifestpatch.Patch, workloads workloadsFile, b brand.Brand) agents.K8sProvisionerConfig {
	return agents.K8sProvisionerConfig{
		Workloads:              workloads.Workloads,
		Workspace:              workloads.Workspace,
		SessionManagerImage:    cfg.Agents.SessionManagerImage,
		SandboxImage:           cfg.Agents.SandboxImage,
		DefaultEnvironmentSize: environmentSizes(cfg).Default,
		SandboxReadyWait:       sandboxGuardrails(cfg).ReadyWait(),
		SandboxStartupSeconds:  cfg.Agents.SandboxStartupSeconds,
		// Not configurable: the operator's ingress NetworkPolicy admits this port,
		// and a pod on a different one would be unreachable.
		SandboxGRPCPort:            ports.SandboxExecGRPC,
		GitAuthorName:              cfg.Agents.SandboxGitAuthorName,
		GitAuthorEmail:             cfg.Agents.SandboxGitAuthorEmail,
		OTLPEndpoint:               observability.TenantOTLPEndpoint(),
		SamplingRate:               observability.SamplingRateString(),
		ProductName:                b.ProductName(),
		ImagePullSecrets:           cfg.Agents.ImagePullSecrets,
		TrustDomain:                cfg.Platform.TrustDomain,
		SystemNamespace:            cfg.Kubernetes.SystemNamespace,
		AuthorizationPolicyEnabled: cfg.Agents.SessionManager.AuthorizationPolicyEnabled,
		NodeMaxOldSpaceMB:          cfg.Agents.SessionManager.NodeMaxOldSpaceMB,
		MinReplicas:                cfg.Agents.Scheduling.MinReplicas,
		DrainGracePeriodSecs:       cfg.Agents.Scheduling.DrainGracePeriodSecs,
		SandboxMount:               cfg.Agents.SandboxMount,
		Patches:                    patches,
	}
}

// validationAgentType is a stand-in agent type used to render every patchable
// resource at startup, so a patch that fails to apply stops the pod at boot.
var validationAgentType = agents.AgentType{ID: "validate", Slug: "validate", TenantID: "validate"}

// loadProvisioning reads the manifest patches and the workload customization,
// then renders every patchable resource once to prove the combination applies.
func loadProvisioning(cfg *agentsConfig.Config, dir string, b brand.Brand) ([]manifestpatch.Patch, workloadsFile, error) {
	workloads, err := loadWorkloads(cfg.Agents.WorkloadsFile)
	if err != nil {
		return nil, workloads, err
	}
	patches, err := manifestpatch.Load(dir)
	if err != nil {
		return nil, workloads, fmt.Errorf("load manifest patches from %s: %w", dir, err)
	}
	if _, err := agents.RenderResources(provisionerConfig(cfg, patches, workloads, b), validationAgentType, "tenant-validate", true); err != nil {
		return nil, workloads, fmt.Errorf("validate provisioning config (patches in %s, workloads in %s): %w", dir, cfg.Agents.WorkloadsFile, err)
	}
	return patches, workloads, nil
}

// renderManifests prints the YAML kora-agents would provision for one agent
// type with every manifest patch applied. It backs the -render CLI mode.
func renderManifests(patchDir string, at agents.AgentType, namespace string, includeSandbox bool) error {
	if at.Slug == "" {
		return fmt.Errorf("-agent-slug is required")
	}
	cfg := agentsConfig.Current
	b, err := brand.Load()
	if err != nil {
		return fmt.Errorf("load brand: %w", err)
	}
	patches, workloads, err := loadProvisioning(cfg, patchDir, b)
	if err != nil {
		return err
	}
	objs, err := agents.RenderResources(provisionerConfig(cfg, patches, workloads, b), at, namespace, includeSandbox)
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
