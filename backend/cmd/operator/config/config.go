// Package operatorConfig holds the configuration for the orbit-operator binary.
package operatorConfig

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
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

	// Install is set from the environment; the fields are not in YAML.
	Install Install `yaml:"-"`
}

// Install contains chart-supplied settings.
type Install struct {
	TrustDomain      string
	SystemNamespace  string
	IstioNamespace   string
	IstioRevision    string
	SPIREClassName   string
	ImagePullSecrets []string

	// CollectorNamespace is the in-cluster namespace of the OTLP collector
	// agent pods may export to.
	CollectorNamespace string

	// CollectorCIDRs are the networkPolicy.externalServices ranges serving an
	// out-of-cluster collector's port. Used when CollectorNamespace is empty.
	CollectorCIDRs []string

	// WebhookCertManager is true when cert-manager issues the webhook certificate
	// and the operator must not manage it.
	WebhookCertManager bool

	Injection   Injection
	PodSecurity PodSecurity
	Preflight   Preflight
}

// Injection describes the containers the webhook adds to every tenant pod.
type Injection struct {
	IdentityProxyImage      string
	IdentityProxyPullPolicy string
	IdentityProxyResources  corev1.ResourceRequirements

	OPAImage              string
	OPAPullPolicy         string
	OPALogLevel           string
	BundlePollingMinDelay int
	BundlePollingMaxDelay int
	OPAResources          corev1.ResourceRequirements
}

// PodSecurity holds the Pod Security Admission levels for tenant namespaces.
type PodSecurity struct {
	Enforce string
	Warn    string
	Audit   string
}

// Preflight configures prerequisite verification.
type Preflight struct {
	SupportedIstioMinors []string
	Interval             time.Duration
}

// SyncPeriod returns the sync period as a time.Duration.
func (c *Config) SyncPeriod() time.Duration {
	return time.Duration(c.Operator.SyncPeriodSecs) * time.Second
}

// ShutdownTimeout returns the graceful shutdown timeout as a time.Duration.
func (c *Config) ShutdownTimeout() time.Duration {
	return time.Duration(c.Operator.ShutdownTimeoutSecs) * time.Second
}

// Current is the loaded configuration: YAML defaults, then the install-time
// environment the chart sets.
var Current = func() *Config {
	cfg := config.MustLoadService[Config]("cmd/operator", config.Environment())
	if v := os.Getenv("ORBIT_LEADER_ELECTION"); v != "" {
		cfg.Operator.LeaderElection = v == "true" || v == "1"
	}
	cfg.Install = loadInstall()
	return cfg
}()

func loadInstall() Install {
	in := Install{
		TrustDomain:        os.Getenv("ORBIT_TRUST_DOMAIN"),
		SystemNamespace:    envOr("ORBIT_SYSTEM_NAMESPACE", "orbit-system"),
		IstioNamespace:     envOr("ORBIT_ISTIO_NAMESPACE", "istio-system"),
		IstioRevision:      os.Getenv("ORBIT_ISTIO_REVISION"),
		SPIREClassName:     envOr("ORBIT_SPIRE_CLASS_NAME", "spire-system-spire"),
		ImagePullSecrets:   splitList(os.Getenv("ORBIT_IMAGE_PULL_SECRETS")),
		CollectorNamespace: os.Getenv("ORBIT_COLLECTOR_NAMESPACE"),
		CollectorCIDRs:     splitList(os.Getenv("ORBIT_COLLECTOR_CIDRS")),
		WebhookCertManager: os.Getenv("ORBIT_WEBHOOK_CERT_MANAGER") == "true",
		Injection: Injection{
			IdentityProxyImage:      envOr("ORBIT_INJECT_IDENTITY_PROXY_IMAGE", "ghcr.io/clear-route/orbit-identity-proxy:latest"),
			IdentityProxyPullPolicy: envOr("ORBIT_INJECT_IDENTITY_PROXY_PULL_POLICY", "IfNotPresent"),
			IdentityProxyResources:  resourcesEnv("ORBIT_INJECT_IDENTITY_PROXY_RESOURCES"),
			OPAImage:                envOr("ORBIT_INJECT_OPA_IMAGE", "docker.io/openpolicyagent/opa:1.15.2-envoy-static"),
			OPAPullPolicy:           envOr("ORBIT_INJECT_OPA_PULL_POLICY", "IfNotPresent"),
			OPALogLevel:             envOr("ORBIT_OPA_LOG_LEVEL", "info"),
			BundlePollingMinDelay:   intEnv("ORBIT_BUNDLE_POLLING_MIN_DELAY", 30),
			BundlePollingMaxDelay:   intEnv("ORBIT_BUNDLE_POLLING_MAX_DELAY", 60),
			OPAResources:            resourcesEnv("ORBIT_INJECT_OPA_RESOURCES"),
		},
		PodSecurity: PodSecurity{
			Enforce: envOr("ORBIT_TENANT_PSA_ENFORCE", "restricted"),
			Warn:    envOr("ORBIT_TENANT_PSA_WARN", "restricted"),
			Audit:   envOr("ORBIT_TENANT_PSA_AUDIT", "restricted"),
		},
		Preflight: Preflight{
			SupportedIstioMinors: splitList(os.Getenv("ORBIT_PREFLIGHT_SUPPORTED_ISTIO_MINORS")),
			Interval:             time.Duration(max(intEnv("ORBIT_PREFLIGHT_INTERVAL_SECONDS", 300), 10)) * time.Second,
		},
	}
	return in
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func intEnv(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func splitList(v string) []string {
	var out []string
	for part := range strings.SplitSeq(v, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ResourceParseErrors collects the keys resourcesEnv failed to parse, so
// main can log them once the injected logger exists; Current is built before
// that logger, so resourcesEnv cannot log directly.
var ResourceParseErrors []error

// resourcesEnv parses resource requirements from an environment variable.
// Malformed JSON yields no requirements and is recorded in ResourceParseErrors
// for main to log; the chart renders this JSON, and a chart bug is surfaced,
// not crashed on.
func resourcesEnv(key string) corev1.ResourceRequirements {
	var rr corev1.ResourceRequirements
	if v := os.Getenv(key); v != "" {
		if err := json.Unmarshal([]byte(v), &rr); err != nil {
			ResourceParseErrors = append(ResourceParseErrors, fmt.Errorf("%s: %w", key, err))
		}
	}
	return rr
}
