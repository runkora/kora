// Package agentsConfig holds configuration for the orbit-agents binary.
package agentsConfig

import (
	"errors"
	"time"

	"github.com/clear-route/orbit/internal/platform/config"
)

// Config holds orbit-agents configuration values.
//
// Values are loaded from:
//
//  1. cmd/orbit-agents/config/base.yaml  — service defaults
//  2. cmd/orbit-agents/config/{env}.yaml — env overrides (optional)
//  3. /etc/orbit/config/cmd/orbit-agents.yaml — rendered by the chart
type Config struct {
	App struct {
		Version string `yaml:"version"`
	} `yaml:"app"`

	Server struct {
		Port                int    `yaml:"port"`
		BasePath            string `yaml:"basePath"`
		ShutdownTimeoutSecs int    `yaml:"shutdownTimeoutSecs"`
	} `yaml:"server"`

	Observability struct {
		ServiceName string `yaml:"serviceName"`
	} `yaml:"observability"`

	Kubernetes struct {
		// InCluster uses the pod's service account token. Set false for local dev.
		InCluster bool `yaml:"inCluster"`
		// TenantNamespacePrefix is prepended to tenant IDs to form namespace names.
		TenantNamespacePrefix string `yaml:"tenantNamespacePrefix"`
		// SystemNamespace is where orbit-agents itself runs.
		SystemNamespace string `yaml:"systemNamespace"`
	} `yaml:"kubernetes"`

	Platform struct {
		// TrustDomain is the SPIFFE trust domain (e.g. "orbit.test").
		// Used to build agent SPIFFE IDs for policy grant creation.
		TrustDomain string `yaml:"trustDomain"`
		// IdentityServiceURL is the base URL of the orbit-identity service.
		// Used by the policy client's token function to exchange the pod's SVID
		// for a platform bearer token.
		IdentityServiceURL string `yaml:"identityServiceURL"`
		// PolicyServiceURL is the base URL of the orbit-policy-service.
		// When empty, grant management is skipped (local dev without policy service).
		PolicyServiceURL string `yaml:"policyServiceURL"`
	} `yaml:"platform"`

	Policy PolicyConfig `yaml:"policy"`

	Drafts DraftsConfig `yaml:"drafts"`

	Agents struct {
		// SessionManagerImage is the container image for session-manager StatefulSet pods.
		SessionManagerImage string `yaml:"sessionManagerImage"`
		// SandboxImage is the base image reference that rendered previews use.
		// Live sandboxes run the digest pinned on the kora-base SandboxImage.
		SandboxImage string `yaml:"sandboxImage"`
		// Environments bounds environment sizes. Default also sizes the
		// built-in default environment. Each dimension is a Kubernetes quantity.
		Environments struct {
			DefaultSize EnvironmentSize `yaml:"defaultSize"`
			MaxSize     EnvironmentSize `yaml:"maxSize"`
		} `yaml:"environments"`
		// SandboxGitAuthorName/Email set the git identity (GIT_AUTHOR_* and
		// GIT_COMMITTER_*) for commits the agent makes inside the sandbox. Set
		// per-deployment so the platform identity is not baked into the operator.
		SandboxGitAuthorName  string `yaml:"sandboxGitAuthorName"`
		SandboxGitAuthorEmail string `yaml:"sandboxGitAuthorEmail"`
		// ImagePullSecrets names the Secrets, present in every tenant namespace,
		// that session-manager and sandbox pods pull their images with.
		ImagePullSecrets []string `yaml:"imagePullSecrets"`
		// MaxConcurrentRunsPerPod is the default ceiling applied to new agent types.
		MaxConcurrentRunsPerPod int `yaml:"maxConcurrentRunsPerPod"`
		// Guardrails are the install's sandbox provisioning settings, sent with
		// every run (docs/adr/2026-09-29-agent-run-guardrails.md).
		Guardrails struct {
			SandboxProvisionAttempts    int `yaml:"sandboxProvisionAttempts"`
			SandboxProvisionBackoffSecs int `yaml:"sandboxProvisionBackoffSecs"`
			SandboxProvisionBudgetSecs  int `yaml:"sandboxProvisionBudgetSecs"`
		} `yaml:"guardrails"`
		// ConnectionRegistryPath is the mounted connections.json file rendered by
		// the operator's ConnectionAggregator into the orbit-tenant-connections
		// ConfigMap. orbit-agents reads it to serve the read-only connection
		// catalog (GET /api/v1/connections). Absent/empty yields an empty catalog.
		ConnectionRegistryPath string `yaml:"connectionRegistryPath"`
		// ManifestPatchDir is the directory of kustomize patch files applied to
		// the resources orbit-agents provisions. The chart mounts the manifest
		// patches ConfigMap here. A missing directory means no patches.
		ManifestPatchDir string `yaml:"manifestPatchDir"`
		// WorkloadsFile is the mounted YAML the chart renders from
		// agents.workloads and agents.workspace: placement, pod metadata,
		// service account annotations and workspace storage for the resources
		// orbit-agents provisions. A missing file means no customization.
		// See docs/adr/2026-09-16-first-class-workload-placement.md.
		WorkloadsFile string `yaml:"workloadsFile"`
		// SessionManager holds settings for the session-manager pods orbit-agents provisions.
		SessionManager struct {
			// AuthorizationPolicyEnabled creates an Istio AuthorizationPolicy
			// beside each agent StatefulSet restricting its session-manager
			// port to the orbit-agents principal. Requires SPIFFE/mTLS, so the
			// chart sets it from platform.spiffe.enabled AND
			// agents.authorizationPolicy.enabled.
			AuthorizationPolicyEnabled bool `yaml:"authorizationPolicyEnabled"`
			// NodeMaxOldSpaceMB caps the V8 heap of Node processes a run starts
			// on its agent pod, so one runaway run cannot OOM-kill the pod's
			// other runs. Zero disables the cap.
			NodeMaxOldSpaceMB int `yaml:"nodeMaxOldSpaceMB"`
		} `yaml:"sessionManager"`
		// SandboxMount gives each sandbox a working folder on its local disk at
		// /workspaces/<run>/sandbox, mounted into the session-manager pod by the
		// sandbox-mount node agent (docs/adr/2026-10-08-sandbox-workspace-mount.md).
		SandboxMount bool `yaml:"sandboxMount"`
	} `yaml:"agents"`
}

// EnvironmentSize is a sandbox's CPU, memory and local disk.
type EnvironmentSize struct {
	CPU    string `yaml:"cpu"`
	Memory string `yaml:"memory"`
	Disk   string `yaml:"disk"`
}

// PolicyConfig configures the in-process OPA policy evaluator.
type PolicyConfig struct {
	// BundleEndpoint is the base URL for the policy-service bundle API.
	// e.g. http://orbit-policy-service.orbit-system.svc/api/v1/policy/bundles
	BundleEndpoint         string `yaml:"bundleEndpoint"`
	BundlePollIntervalSecs int    `yaml:"bundlePollIntervalSecs"`
}

// DraftsConfig configures the agent type draft reaper. Abandoned drafts are
// swept after the retention window. See docs/adr/2026-07-17-agent-builder.md.
type DraftsConfig struct {
	// ReaperIntervalSecs is how often the draft reaper sweeps. Defaults to
	// hourly when <= 0.
	ReaperIntervalSecs int `yaml:"reaperIntervalSecs"`
	// RetentionDays is how long an untouched draft survives before the reaper
	// deletes it. Defaults to 30 days when <= 0.
	RetentionDays int `yaml:"retentionDays"`
}

// ReaperInterval returns the configured sweep interval, defaulting to 1 hour.
func (c *DraftsConfig) ReaperInterval() time.Duration {
	if c.ReaperIntervalSecs <= 0 {
		return time.Hour
	}
	return time.Duration(c.ReaperIntervalSecs) * time.Second
}

// RetentionDuration returns the configured draft retention, defaulting to 30 days.
func (c *DraftsConfig) RetentionDuration() time.Duration {
	if c.RetentionDays <= 0 {
		return 30 * 24 * time.Hour
	}
	return time.Duration(c.RetentionDays) * 24 * time.Hour
}

// BundlePollInterval returns the configured poll interval, defaulting to 30s.
func (c *PolicyConfig) BundlePollInterval() time.Duration {
	if c.BundlePollIntervalSecs <= 0 {
		return 30 * time.Second
	}
	return time.Duration(c.BundlePollIntervalSecs) * time.Second
}

// ShutdownTimeout returns the server shutdown timeout as a time.Duration.
func (c *Config) ShutdownTimeout() time.Duration {
	return time.Duration(c.Server.ShutdownTimeoutSecs) * time.Second
}

// TenantNamespace returns the k8s namespace for the given tenant ID.
func (c *Config) TenantNamespace(tenantID string) string {
	return c.Kubernetes.TenantNamespacePrefix + tenantID
}

// Current holds all non-secret configuration for orbit-agents.
// Secrets (Postgres DSN) must be supplied via environment variables.
var Current = config.MustLoadService[Config]("cmd/orbit-agents", config.Environment())

// Validate rejects an in-cluster config that would mint identities in an
// empty trust domain ("spiffe:///tenant/..."). main calls it at startup; the
// base YAML carries no trust domain on purpose, so it cannot run at load.
func Validate(cfg *Config) error {
	if cfg.Kubernetes.InCluster && cfg.Platform.TrustDomain == "" {
		return errors.New("platform.trustDomain must be set when kubernetes.inCluster is true")
	}
	if cfg.Agents.SessionManager.NodeMaxOldSpaceMB < 0 {
		return errors.New("agents.sessionManager.nodeMaxOldSpaceMB must be a non-negative integer")
	}
	return nil
}
