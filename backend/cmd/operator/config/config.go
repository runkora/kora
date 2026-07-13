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

		// DevEnvIngressDomain is the base domain used to compute each
		// OrbitDevEnvironment's status.ingressHost ("{name}.{domain}").
		// Real ingress/DNS/TLS wiring is a staging concern (see the vcluster
		// ADR's "Out of local scope") — locally this is informational only.
		DevEnvIngressDomain string `yaml:"devEnvIngressDomain"`

		// DevEnvAppExternalURL is the browser-facing base URL every dev
		// environment's Orbit UI/API is served under, path-scoped per
		// environment: {DevEnvAppExternalURL}/{env-id} (the ADR's
		// path-based-app-ingress amendment). One shared hostname for all
		// environments — one DNS record, one TLS certificate — as opposed to
		// the per-environment SNI hostnames of the kubectl API route
		// (DevEnvIngressDomain above), which never need to resolve in DNS.
		// The URL's hostname is what the shared gateway's L7 app listener
		// matches on; the scheme/port only shape status.appURL. Override via
		// ORBIT_DEVENV_APP_EXTERNAL_URL.
		DevEnvAppExternalURL string `yaml:"devEnvAppExternalURL"`

		// DevEnvAppTLSCredentialName optionally names an istio-system TLS
		// Secret for the shared app listener. When set, the app gateway
		// terminates HTTPS with it (and redirects HTTP); when empty (local
		// k3d), the listener is plain HTTP on port 80. Override via
		// ORBIT_DEVENV_APP_TLS_CREDENTIAL.
		DevEnvAppTLSCredentialName string `yaml:"devEnvAppTLSCredentialName"`

		// DevEnvVClusterVersion pins the vcluster chart version installed by
		// the OrbitDevEnvironment reconciler. Override via
		// ORBIT_DEVENV_VCLUSTER_VERSION. Empty means "use the built-in
		// default" (see defaultChartVersions in internal/operator).
		DevEnvVClusterVersion string `yaml:"devEnvVClusterVersion"`

		// DevEnvImageRegistry is the registry prefix used both for the
		// in-vcluster orbit-operator chart's own image and for the
		// spec.imageRegistry field of the OrbitPlatform CR the reconciler
		// applies inside each environment. Empty means "use built-in
		// versioned image references" (production); local dev sets this via
		// the chart's devEnv.imageRegistry value (deploy/local/operator-values.yaml),
		// which is templated into the ORBIT_DEVENV_IMAGE_REGISTRY env var —
		// this in-cluster deployment never sets APP_ENV, so
		// cmd/operator/config/development.yaml is not actually loaded here;
		// env-var overrides are the only way local dev values reach the pod
		// (same pattern as ORBIT_LEADER_ELECTION/ORBIT_OPA_IMAGE below).
		DevEnvImageRegistry string `yaml:"devEnvImageRegistry"`
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
	if v := os.Getenv("ORBIT_DEVENV_VCLUSTER_VERSION"); v != "" {
		cfg.Operator.DevEnvVClusterVersion = v
	}
	if v := os.Getenv("ORBIT_DEVENV_IMAGE_REGISTRY"); v != "" {
		cfg.Operator.DevEnvImageRegistry = v
	}
	if cfg.Operator.DevEnvIngressDomain == "" {
		cfg.Operator.DevEnvIngressDomain = "dev.orbit.local"
	}
	if v := os.Getenv("ORBIT_DEVENV_APP_EXTERNAL_URL"); v != "" {
		cfg.Operator.DevEnvAppExternalURL = v
	}
	if v := os.Getenv("ORBIT_DEVENV_APP_TLS_CREDENTIAL"); v != "" {
		cfg.Operator.DevEnvAppTLSCredentialName = v
	}
	if cfg.Operator.DevEnvAppExternalURL == "" {
		// Same host + TLS port the main platform frontend already uses
		// locally (deploy/local/platform-values.yaml's frontendHost, 8443
		// being the k3d loadbalancer's mapping of the shared gateway's port
		// 443 — infrastructure/bootstrap/k3d-config.yaml). Path-prefix
		// routing (/{env-id}) distinguishes an environment, not a separate
		// hostname, so dev environments share the same host/cert as prod
		// traffic rather than a distinct plain-HTTP "devenv." host.
		cfg.Operator.DevEnvAppExternalURL = "https://orbit.local.clearroute.io:8443"
	}
	if cfg.Operator.DevEnvAppTLSCredentialName == "" {
		cfg.Operator.DevEnvAppTLSCredentialName = "orbit-gateway-tls"
	}
	return cfg
}()
