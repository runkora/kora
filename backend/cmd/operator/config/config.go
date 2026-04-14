// Package operatorConfig holds the configuration for the orbit-operator binary.
package operatorConfig

import (
	"os"
	"time"

	"github.com/clear-route/orbit/internal/platform/config"
)

// Config holds the operator's non-secret configuration.
// Secrets (e.g. signing key) are passed via the platform Secret referenced
// in the OrbitPlatform CR — the operator binary has no secrets of its own.
//
// Values are loaded from:
//
//  1. cmd/operator/config/base.yaml       — defaults
//  2. cmd/operator/config/{env}.yaml      — env overrides (optional)
type Config struct {
	App struct {
		Version string `yaml:"version"`
	} `yaml:"app"`

	Operator struct {
		// Namespace is the namespace the operator itself runs in.
		// Defaults to orbit-system.
		Namespace string `yaml:"namespace"`

		// ChartDir is the path to the local charts directory.
		// Used in local dev (charts/). In production the charts are
		// embedded in the operator image at /etc/orbit/charts.
		ChartDir string `yaml:"chartDir"`

		// LeaderElection enables leader election for HA deployments.
		LeaderElection bool `yaml:"leaderElection"`

		// WebhookCertDir is the directory containing the TLS cert and key for
		// the admission webhook server (tls.crt, tls.key). Provisioned by the
		// Helm chart from the orbit-operator-webhook-tls Secret.
		WebhookCertDir string `yaml:"webhookCertDir"`

		// SyncPeriod is how often the operator re-reconciles all resources
		// even when no events are received (drift detection).
		SyncPeriodSecs int `yaml:"syncPeriodSecs"`

		// ShutdownTimeoutSecs is the graceful shutdown timeout.
		ShutdownTimeoutSecs int `yaml:"shutdownTimeoutSecs"`
	} `yaml:"operator"`

	Observability struct {
		ServiceName string `yaml:"serviceName"`
	} `yaml:"observability"`
}

// SyncPeriod returns the sync period as a time.Duration.
func (c *Config) SyncPeriod() time.Duration {
	return time.Duration(c.Operator.SyncPeriodSecs) * time.Second
}

// ShutdownTimeout returns the graceful shutdown timeout as a time.Duration.
func (c *Config) ShutdownTimeout() time.Duration {
	return time.Duration(c.Operator.ShutdownTimeoutSecs) * time.Second
}

// Current holds all non-secret configuration for the operator.
// Explicit ORBIT_* env var overrides are applied after YAML loading so that
// operators running in-cluster never need APP_ENV-based YAML branching.
var Current = func() *Config {
	cfg := config.MustLoadService[Config]("cmd/operator", config.Environment())
	// Allow in-cluster dev (Kind, minikube) to disable leader election without
	// a separate APP_ENV YAML file.
	if v := os.Getenv("ORBIT_LEADER_ELECTION"); v != "" {
		cfg.Operator.LeaderElection = v == "true" || v == "1"
	}
	return cfg
}()
