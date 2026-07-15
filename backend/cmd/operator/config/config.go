// Package operatorConfig holds the configuration for the orbit-operator binary.
package operatorConfig

import (
	"encoding/json"
	"log"
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
		// OrbitDevEnvironment's status.ingressHost ("{name}.{domain}") — the
		// per-environment SNI/certificate-SAN host for brokered kubectl
		// access. Required in production (validateDevEnvRequiredConfig):
		// there is no cluster-agnostic default. Override via
		// ORBIT_DEVENV_INGRESS_DOMAIN.
		DevEnvIngressDomain string `yaml:"devEnvIngressDomain"`

		// DevEnvAppExternalURL is the browser-facing base URL every dev
		// environment's Orbit UI/API is served under, path-scoped per
		// environment: {DevEnvAppExternalURL}/{env-id} (the vcluster ADR's
		// path-based-app-ingress amendment). One shared hostname for all
		// environments — one DNS record, one TLS certificate — as opposed to
		// the per-environment SNI hostnames of the kubectl API route
		// (DevEnvIngressDomain above), which never need to resolve in DNS.
		// Required in production; override via ORBIT_DEVENV_APP_EXTERNAL_URL.
		DevEnvAppExternalURL string `yaml:"devEnvAppExternalURL"`

		// DevEnvVClusterVersion pins the vcluster chart version installed by
		// the OrbitDevEnvironment reconciler. Override via
		// ORBIT_DEVENV_VCLUSTER_VERSION. Empty means "use the built-in
		// default" (see defaultChartVersions in internal/operator, kept in
		// sync with scripts/cluster/versions.env's VCLUSTER_CHART_VERSION).
		DevEnvVClusterVersion string `yaml:"devEnvVClusterVersion"`

		// DevEnvImageRegistry is the registry prefix used both for the
		// in-vcluster orbit-operator chart's own image and for the
		// spec.imageRegistry field of the OrbitPlatform CR the reconciler
		// applies inside each environment. Required in production — unlike
		// orbitImageRef's general fallback, there is no built-in versioned
		// image reference this can silently default to. Override via
		// ORBIT_DEVENV_IMAGE_REGISTRY.
		DevEnvImageRegistry string `yaml:"devEnvImageRegistry"`

		// DevEnvSelfImageTag is the tag THIS operator's own container is
		// running at (charts/orbit-operator/templates/deployment.yaml
		// threads it in unconditionally, via the same
		// "orbit-operator.imageTag" helper the container's own image line
		// uses, so it can never drift out of sync with what's actually
		// deployed). A dev environment's symbolic spec.ref "main" resolves
		// to this value — never a separately published or floating tag —
		// so it always matches the currently deployed Orbit version
		// exactly. Required in production; override via
		// ORBIT_DEVENV_SELF_IMAGE_TAG.
		DevEnvSelfImageTag string `yaml:"devEnvSelfImageTag"`

		// DevEnvQuota holds the ResourceQuota ceilings applied to each dev
		// environment's host namespace. Defaults below are derived from the
		// measured per-environment footprint (~3.5 CPU / 6 Gi requested,
		// ~38 CPU / 27 Gi declared limits — every injected sidecar declares
		// 2 CPU/1Gi) rather than guessed against live runs (vcluster ADR §4).
		DevEnvQuota struct {
			CPURequests    string `yaml:"cpuRequests"`
			MemoryRequests string `yaml:"memoryRequests"`
			CPULimits      string `yaml:"cpuLimits"`
			MemoryLimits   string `yaml:"memoryLimits"`
			MaxPods        string `yaml:"maxPods"`
		} `yaml:"devEnvQuota"`

		// DevEnvLimitRange holds the default per-container request/limit
		// applied inside each dev environment's namespace. The default CPU
		// request must accommodate istiod's own 2Gi memory request — a
		// LimitRange default below a workload's own request is a synchronous
		// admission rejection, not a soft warning (vcluster ADR pinned #19).
		DevEnvLimitRange struct {
			DefaultCPURequest    string `yaml:"defaultCPURequest"`
			DefaultMemoryRequest string `yaml:"defaultMemoryRequest"`
			DefaultCPULimit      string `yaml:"defaultCPULimit"`
			DefaultMemoryLimit   string `yaml:"defaultMemoryLimit"`
		} `yaml:"devEnvLimitRange"`

		// DevEnvNodeSelector and DevEnvTolerations place dev-environment
		// workloads (host namespace + vcluster StatefulSet) on a dedicated,
		// tainted node pool on staging. Both are JSON-encoded over their env
		// var overrides (ORBIT_DEVENV_NODE_SELECTOR, ORBIT_DEVENV_TOLERATIONS)
		// because they are structured values with no scalar representation,
		// and this in-cluster deployment never sets APP_ENV — env vars are
		// the only override path available to it.
		DevEnvNodeSelector map[string]string  `yaml:"devEnvNodeSelector"`
		DevEnvTolerations  []DevEnvToleration `yaml:"devEnvTolerations"`
	} `yaml:"operator"`

	Observability struct {
		ServiceName string `yaml:"serviceName"`
	} `yaml:"observability"`
}

// DevEnvToleration mirrors corev1.Toleration's JSON shape closely enough for
// the reconciler to convert directly, without this config package depending
// on k8s.io/api.
type DevEnvToleration struct {
	Key               string `yaml:"key" json:"key,omitempty"`
	Operator          string `yaml:"operator" json:"operator,omitempty"`
	Value             string `yaml:"value" json:"value,omitempty"`
	Effect            string `yaml:"effect" json:"effect,omitempty"`
	TolerationSeconds *int64 `yaml:"tolerationSeconds" json:"tolerationSeconds,omitempty"`
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

	if v := os.Getenv("ORBIT_DEVENV_INGRESS_DOMAIN"); v != "" {
		cfg.Operator.DevEnvIngressDomain = v
	}
	if v := os.Getenv("ORBIT_DEVENV_APP_EXTERNAL_URL"); v != "" {
		cfg.Operator.DevEnvAppExternalURL = v
	}
	if v := os.Getenv("ORBIT_DEVENV_VCLUSTER_VERSION"); v != "" {
		cfg.Operator.DevEnvVClusterVersion = v
	}
	if v := os.Getenv("ORBIT_DEVENV_IMAGE_REGISTRY"); v != "" {
		cfg.Operator.DevEnvImageRegistry = v
	}
	if v := os.Getenv("ORBIT_DEVENV_SELF_IMAGE_TAG"); v != "" {
		cfg.Operator.DevEnvSelfImageTag = v
	}
	if v := os.Getenv("ORBIT_DEVENV_QUOTA_CPU_REQUESTS"); v != "" {
		cfg.Operator.DevEnvQuota.CPURequests = v
	}
	if v := os.Getenv("ORBIT_DEVENV_QUOTA_MEMORY_REQUESTS"); v != "" {
		cfg.Operator.DevEnvQuota.MemoryRequests = v
	}
	if v := os.Getenv("ORBIT_DEVENV_QUOTA_CPU_LIMITS"); v != "" {
		cfg.Operator.DevEnvQuota.CPULimits = v
	}
	if v := os.Getenv("ORBIT_DEVENV_QUOTA_MEMORY_LIMITS"); v != "" {
		cfg.Operator.DevEnvQuota.MemoryLimits = v
	}
	if v := os.Getenv("ORBIT_DEVENV_QUOTA_MAX_PODS"); v != "" {
		cfg.Operator.DevEnvQuota.MaxPods = v
	}
	if v := os.Getenv("ORBIT_DEVENV_LIMITRANGE_DEFAULT_CPU_REQUEST"); v != "" {
		cfg.Operator.DevEnvLimitRange.DefaultCPURequest = v
	}
	if v := os.Getenv("ORBIT_DEVENV_LIMITRANGE_DEFAULT_MEMORY_REQUEST"); v != "" {
		cfg.Operator.DevEnvLimitRange.DefaultMemoryRequest = v
	}
	if v := os.Getenv("ORBIT_DEVENV_LIMITRANGE_DEFAULT_CPU_LIMIT"); v != "" {
		cfg.Operator.DevEnvLimitRange.DefaultCPULimit = v
	}
	if v := os.Getenv("ORBIT_DEVENV_LIMITRANGE_DEFAULT_MEMORY_LIMIT"); v != "" {
		cfg.Operator.DevEnvLimitRange.DefaultMemoryLimit = v
	}
	if v := os.Getenv("ORBIT_DEVENV_NODE_SELECTOR"); v != "" {
		var selector map[string]string
		if err := json.Unmarshal([]byte(v), &selector); err != nil {
			log.Fatalf("cmd/operator: ORBIT_DEVENV_NODE_SELECTOR is not valid JSON: %v", err)
		}
		cfg.Operator.DevEnvNodeSelector = selector
	}
	if v := os.Getenv("ORBIT_DEVENV_TOLERATIONS"); v != "" {
		var tolerations []DevEnvToleration
		if err := json.Unmarshal([]byte(v), &tolerations); err != nil {
			log.Fatalf("cmd/operator: ORBIT_DEVENV_TOLERATIONS is not valid JSON: %v", err)
		}
		cfg.Operator.DevEnvTolerations = tolerations
	}

	// Quota/LimitRange defaults not otherwise set via YAML or env — the
	// measured per-environment footprint (vcluster ADR §4/§7), not a guess
	// re-bumped against live runs.
	if cfg.Operator.DevEnvQuota.CPURequests == "" {
		cfg.Operator.DevEnvQuota.CPURequests = "6"
	}
	if cfg.Operator.DevEnvQuota.MemoryRequests == "" {
		cfg.Operator.DevEnvQuota.MemoryRequests = "12Gi"
	}
	if cfg.Operator.DevEnvQuota.CPULimits == "" {
		cfg.Operator.DevEnvQuota.CPULimits = "48"
	}
	if cfg.Operator.DevEnvQuota.MemoryLimits == "" {
		cfg.Operator.DevEnvQuota.MemoryLimits = "32Gi"
	}
	if cfg.Operator.DevEnvQuota.MaxPods == "" {
		cfg.Operator.DevEnvQuota.MaxPods = "60"
	}
	if cfg.Operator.DevEnvLimitRange.DefaultCPURequest == "" {
		cfg.Operator.DevEnvLimitRange.DefaultCPURequest = "100m"
	}
	if cfg.Operator.DevEnvLimitRange.DefaultMemoryRequest == "" {
		cfg.Operator.DevEnvLimitRange.DefaultMemoryRequest = "128Mi"
	}
	if cfg.Operator.DevEnvLimitRange.DefaultCPULimit == "" {
		cfg.Operator.DevEnvLimitRange.DefaultCPULimit = "2"
	}
	if cfg.Operator.DevEnvLimitRange.DefaultMemoryLimit == "" {
		cfg.Operator.DevEnvLimitRange.DefaultMemoryLimit = "3Gi"
	}

	validateDevEnvRequiredConfig(cfg)

	return cfg
}()

// validateDevEnvRequiredConfig fails fast at startup when dev-environment
// config with no safe production default is missing. Each of these used to
// silently fall back to a k3d-only value (localhost, dev.orbit.local, an
// unset registry) that would then falsely "work" against the wrong cluster
// — the vcluster ADR's k3d-hardwired finding (§1 item 1).
func validateDevEnvRequiredConfig(cfg *Config) {
	required := []struct {
		field, envVar, value string
	}{
		{"operator.devEnvIngressDomain", "ORBIT_DEVENV_INGRESS_DOMAIN", cfg.Operator.DevEnvIngressDomain},
		{"operator.devEnvAppExternalURL", "ORBIT_DEVENV_APP_EXTERNAL_URL", cfg.Operator.DevEnvAppExternalURL},
		{"operator.devEnvImageRegistry", "ORBIT_DEVENV_IMAGE_REGISTRY", cfg.Operator.DevEnvImageRegistry},
		{"operator.devEnvSelfImageTag", "ORBIT_DEVENV_SELF_IMAGE_TAG", cfg.Operator.DevEnvSelfImageTag},
	}
	for _, r := range required {
		if r.value == "" {
			log.Fatalf("cmd/operator: %s is required — set %s (or the equivalent devEnv.* chart value)", r.field, r.envVar)
		}
	}
}
