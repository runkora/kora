// Package agentsConfig holds configuration for the orbit-agents binary.
package agentsConfig

import (
	"os"
	"time"

	"github.com/clear-route/orbit/internal/platform/config"
)

// Config holds orbit-agents configuration values.
//
// Values are loaded from:
//
//  1. cmd/orbit-agents/config/base.yaml  — service defaults
//  2. cmd/orbit-agents/config/{env}.yaml — env overrides (optional)
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

	Agents struct {
		// SessionManagerImage is the container image for session-manager StatefulSet pods.
		SessionManagerImage string `yaml:"sessionManagerImage"`
		// SandboxImage is the default container image for sandbox pods.
		SandboxImage string `yaml:"sandboxImage"`
		// SandboxGRPCPort is the port the sandbox-exec gRPC server listens on.
		SandboxGRPCPort int `yaml:"sandboxGRPCPort"`
		// SandboxGitAuthorName/Email set the git identity (GIT_AUTHOR_* and
		// GIT_COMMITTER_*) for commits the agent makes inside the sandbox. Set
		// per-deployment so the platform identity is not baked into the operator.
		SandboxGitAuthorName  string `yaml:"sandboxGitAuthorName"`
		SandboxGitAuthorEmail string `yaml:"sandboxGitAuthorEmail"`
		// MaxConcurrentRunsPerPod is the default ceiling applied to new agent types.
		MaxConcurrentRunsPerPod int `yaml:"maxConcurrentRunsPerPod"`
		// ConnectionRegistryPath is the mounted connections.json file rendered by
		// the operator's ConnectionAggregator into the orbit-tenant-connections
		// ConfigMap. orbit-agents reads it to serve the read-only connection
		// catalog (GET /api/v1/connections). Absent/empty yields an empty catalog.
		ConnectionRegistryPath string `yaml:"connectionRegistryPath"`
		// SessionManager holds env var overrides injected into session-manager pods.
		SessionManager struct {
			// PostgresSSLMode is injected as ORBIT_POSTGRES_SSL_MODE.
			// Should be "require" in production and "disable" for local dev.
			PostgresSSLMode string `yaml:"postgresSSLMode"`
		} `yaml:"sessionManager"`
	} `yaml:"agents"`
}

// PolicyConfig configures the in-process OPA policy evaluator.
type PolicyConfig struct {
	// BundleEndpoint is the base URL for the policy-service bundle API.
	// e.g. http://orbit-policy-service.orbit-system.svc/api/v1/policy/bundles
	BundleEndpoint         string `yaml:"bundleEndpoint"`
	BundlePollIntervalSecs int    `yaml:"bundlePollIntervalSecs"`
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
var Current = func() *Config {
	cfg := config.MustLoadService[Config]("cmd/orbit-agents", config.Environment())
	if v := os.Getenv("ORBIT_SESSION_MANAGER_POSTGRES_SSL_MODE"); v != "" {
		cfg.Agents.SessionManager.PostgresSSLMode = v
	}
	return cfg
}()
