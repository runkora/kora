// Package operatorConfig holds the configuration for the orbit-operator binary.
package operatorConfig

import (
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/runkora/kora/backend/internal/platform/config"
)

// Config contains operator configuration.
type Config struct {
	App struct {
		Version string `yaml:"version"`
	} `yaml:"app"`

	Operator struct {
		// LeaderElection gates the reconcilers; webhooks serve from every replica.
		LeaderElection bool `yaml:"leaderElection"`

		// WebhookCertDir is where the operator writes the serving certificate it
		// manages (or where cert-manager mounts one).
		WebhookCertDir string `yaml:"webhookCertDir"`

		// SyncPeriodSecs is how often every object is re-reconciled without an event.
		SyncPeriodSecs int `yaml:"syncPeriodSecs"`

		// ShutdownTimeoutSecs is the graceful shutdown timeout.
		ShutdownTimeoutSecs int `yaml:"shutdownTimeoutSecs"`
	} `yaml:"operator"`

	Observability struct {
		ServiceName string `yaml:"serviceName"`
	} `yaml:"observability"`

	// Install holds the per-install settings the chart renders.
	Install Install `yaml:"install"`
}

// Install contains chart-supplied settings.
type Install struct {
	TrustDomain      string   `yaml:"trustDomain"`
	SystemNamespace  string   `yaml:"systemNamespace"`
	IstioNamespace   string   `yaml:"istioNamespace"`
	IstioRevision    string   `yaml:"istioRevision"`
	SPIREClassName   string   `yaml:"spireClassName"`
	ImagePullSecrets []string `yaml:"imagePullSecrets"`

	// CollectorNamespace is the in-cluster namespace of the OTLP collector
	// agent pods may export to.
	CollectorNamespace string `yaml:"collectorNamespace"`

	// CollectorCIDRs are the networkPolicy.externalServices ranges serving an
	// out-of-cluster collector's port. Used when CollectorNamespace is empty.
	CollectorCIDRs []string `yaml:"collectorCIDRs"`

	// WebhookCertManager is true when cert-manager issues the webhook certificate
	// and the operator must not manage it.
	WebhookCertManager bool `yaml:"webhookCertManager"`

	Injection     Injection     `yaml:"injection"`
	PodSecurity   PodSecurity   `yaml:"podSecurity"`
	Preflight     Preflight     `yaml:"preflight"`
	SandboxImages SandboxImages `yaml:"sandboxImages"`
}

// SandboxImages configures sandbox image validation and warming.
type SandboxImages struct {
	// BaseImage is registered in every tenant namespace as kora-base.
	BaseImage string `yaml:"baseImage"`
	// WarmBase keeps the base image pulled on the nodes that run sandboxes.
	WarmBase bool `yaml:"warmBase"`
	// RegistryMirrors maps a registry host in an image reference to the host the
	// operator reads it from. A target given as http://host is read over plain HTTP.
	RegistryMirrors map[string]string `yaml:"registryMirrors"`
	// WarmerPlacement is the sandbox placement that image warmers run in.
	WarmerPlacement WarmerPlacement `yaml:"warmerPlacement"`
	// Bundled holds the bundled sandbox images keyed as in agents.sandboxImages.bundled.
	// A key that is absent counts as disabled.
	Bundled map[string]BundledSandboxImage `yaml:"bundled"`
}

// BundledSandboxImage is one bundled image as the chart renders it.
type BundledSandboxImage struct {
	Enabled bool   `yaml:"enabled"`
	Warm    bool   `yaml:"warm"`
	Image   string `yaml:"image"`
}

// WarmerPlacement mirrors operator.WarmerPlacement with yaml tags. main converts between them, which needs identical fields.
type WarmerPlacement struct {
	NodeSelector              map[string]string                 `yaml:"nodeSelector"`
	Tolerations               []corev1.Toleration               `yaml:"tolerations"`
	Affinity                  *corev1.Affinity                  `yaml:"affinity"`
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `yaml:"topologySpreadConstraints"`
	PriorityClassName         string                            `yaml:"priorityClassName"`
}

// Injection describes the containers the webhook adds to every tenant pod.
type Injection struct {
	IdentityProxyImage      string                      `yaml:"identityProxyImage"`
	IdentityProxyPullPolicy string                      `yaml:"identityProxyPullPolicy"`
	IdentityProxyResources  corev1.ResourceRequirements `yaml:"identityProxyResources"`

	OPAImage              string                      `yaml:"opaImage"`
	OPAPullPolicy         string                      `yaml:"opaPullPolicy"`
	OPALogLevel           string                      `yaml:"opaLogLevel"`
	BundlePollingMinDelay int                         `yaml:"bundlePollingMinDelay"`
	BundlePollingMaxDelay int                         `yaml:"bundlePollingMaxDelay"`
	OPAResources          corev1.ResourceRequirements `yaml:"opaResources"`

	// StartupProbeSeconds is the startup probe budget of both sidecars. Zero takes the default.
	StartupProbeSeconds int `yaml:"startupProbeSeconds"`

	// SandboxSidecars pins each sidecar on sandbox pods: its requests equal its limits.
	SandboxSidecars SandboxSidecars `yaml:"sandboxSidecars"`
}

// SandboxSidecars holds the resources of each sidecar on a sandbox pod.
type SandboxSidecars struct {
	IstioProxy    corev1.ResourceList `yaml:"istioProxy"`
	IdentityProxy corev1.ResourceList `yaml:"identityProxy"`
	OPA           corev1.ResourceList `yaml:"opa"`
}

// PodSecurity holds the Pod Security Admission levels for tenant namespaces.
type PodSecurity struct {
	Enforce string `yaml:"enforce"`
	Warn    string `yaml:"warn"`
	Audit   string `yaml:"audit"`
}

// Preflight configures prerequisite verification.
type Preflight struct {
	SupportedIstioMinors []string `yaml:"supportedIstioMinors"`
	IntervalSeconds      int      `yaml:"intervalSeconds"`
}

// Interval returns the preflight interval, at least ten seconds.
func (p Preflight) Interval() time.Duration {
	return time.Duration(max(p.IntervalSeconds, 10)) * time.Second
}

// SyncPeriod returns the sync period as a time.Duration.
func (c *Config) SyncPeriod() time.Duration {
	return time.Duration(c.Operator.SyncPeriodSecs) * time.Second
}

// ShutdownTimeout returns the graceful shutdown timeout as a time.Duration.
func (c *Config) ShutdownTimeout() time.Duration {
	return time.Duration(c.Operator.ShutdownTimeoutSecs) * time.Second
}

// Current is the loaded configuration: YAML defaults, then the file the chart renders.
var Current = config.MustLoadService[Config]("cmd/operator", config.Environment())
