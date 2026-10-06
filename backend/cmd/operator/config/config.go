// Package operatorConfig holds the configuration for the orbit-operator binary.
package operatorConfig

import (
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/clear-route/orbit/internal/platform/config"
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

	Injection   Injection   `yaml:"injection"`
	PodSecurity PodSecurity `yaml:"podSecurity"`
	Preflight   Preflight   `yaml:"preflight"`
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
