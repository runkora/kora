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

		// OPAImage is the OPA Envoy plugin sidecar image injected into tenant pods.
		// Override via ORBIT_OPA_IMAGE env var to patch CVEs without a code change.
		OPAImage string `yaml:"opaImage"`

		// OPALogLevel is the log level for the OPA sidecar (e.g. info, debug, error).
		// Default: info. Override via ORBIT_OPA_LOG_LEVEL.
		OPALogLevel string `yaml:"opaLogLevel"`

		// BundlePollingMinDelay is the minimum OPA bundle polling interval in seconds.
		// Override via ORBIT_BUNDLE_POLLING_MIN_DELAY.
		BundlePollingMinDelay int `yaml:"bundlePollingMinDelay"`

		// BundlePollingMaxDelay is the maximum OPA bundle polling interval in seconds.
		// Override via ORBIT_BUNDLE_POLLING_MAX_DELAY.
		BundlePollingMaxDelay int `yaml:"bundlePollingMaxDelay"`
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
	if v := os.Getenv("ORBIT_OPA_IMAGE"); v != "" {
		cfg.Operator.OPAImage = v
	}
	if v := os.Getenv("ORBIT_OPA_LOG_LEVEL"); v != "" {
		cfg.Operator.OPALogLevel = v
	}
	// Apply defaults for OPA settings if not set via YAML or env.
	if cfg.Operator.OPAImage == "" {
		cfg.Operator.OPAImage = "openpolicyagent/opa:1.15.2-envoy-static"
	}
	if cfg.Operator.OPALogLevel == "" {
		cfg.Operator.OPALogLevel = "info"
	}
	if cfg.Operator.BundlePollingMinDelay == 0 {
		cfg.Operator.BundlePollingMinDelay = 30
	}
	if cfg.Operator.BundlePollingMaxDelay == 0 {
		cfg.Operator.BundlePollingMaxDelay = 60
	}
	return cfg
}()
