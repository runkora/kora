package devenvironments

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/clear-route/orbit/internal/platform/config"
)

// Config is the non-secret, YAML-sourced configuration for the
// devenvironments feature.
type Config struct {
	Quota      QuotaConfig      `yaml:"quota"`
	Kubeconfig KubeconfigConfig `yaml:"kubeconfig"`
	APIServer  APIServerConfig  `yaml:"apiServer"`

	// DevEnvironmentID is set only when this identity service is itself
	// running inside a dev environment's own vcluster (the operator threads
	// it in via ORBIT_DEV_ENVIRONMENT_ID — charts/orbit/templates/identity/
	// deployment.yaml's identity.config.devEnvironmentId value). Empty (the
	// normal, host-install case) means CreateEnvironment behaves normally;
	// non-empty means it refuses every call outright — see
	// ErrNestedProvisioning.
	DevEnvironmentID string `yaml:"-"`
}

// QuotaConfig bounds how many active environments a single principal may hold.
type QuotaConfig struct {
	// MaxActivePerOwner is the per-principal cap on active environments.
	MaxActivePerOwner int `yaml:"maxActivePerOwner"`
}

// KubeconfigConfig controls the kubeconfig-minting endpoint.
type KubeconfigConfig struct {
	// SignTimeoutSeconds bounds how long Kubeconfig() waits for the operator
	// to sign a freshly submitted CSR.
	SignTimeoutSeconds int `yaml:"signTimeoutSeconds"`
}

// SignTimeout returns Kubeconfig.SignTimeoutSeconds as a time.Duration.
func (c KubeconfigConfig) SignTimeout() time.Duration {
	return time.Duration(c.SignTimeoutSeconds) * time.Second
}

// APIServerConfig resolves the vcluster API server external-reachability
// question: status.ingressHost is only a hostname used for SNI-based
// TLS-passthrough routing (see the operator's devenvironment_ingress.go) —
// it is never required to resolve via DNS. The kubeconfig's actual server:
// address is this separately-configured, genuinely reachable host:port (the
// shared Istio ingress gateway's TLS-passthrough listener), decoupled from
// the per-environment SNI value via the kubeconfig's tls-server-name field
// (assembleKubeconfig).
type APIServerConfig struct {
	// ExternalAddress is a "host:port" pointing at the shared ingress
	// gateway's TLS-passthrough listener (orbitapi.DevEnvAPIGatewayPort).
	// Required — no default; a k3d-only default here was exactly the
	// hardwiring this delivery removes. Set via
	// ORBIT_DEVENV_API_SERVER_EXTERNAL_ADDRESS.
	ExternalAddress string `yaml:"externalAddress"`
}

// LoadConfig loads the devenvironments feature configuration from
// internal/devenvironments/config/base.yaml and applies environment variable
// overrides. Returns an error (never panics or exits) when a required value
// is missing — this package is not a cmd/ entrypoint, so the caller
// (cmd/identity/main.go) decides how to fail the process.
func LoadConfig() (*Config, error) {
	cfg, err := config.Load[Config]("internal/devenvironments", "base")
	if err != nil {
		return nil, fmt.Errorf("load devenvironments config: %w", err)
	}
	overrideInt(&cfg.Quota.MaxActivePerOwner, "ORBIT_DEVENV_MAX_ACTIVE_PER_OWNER")
	overrideInt(&cfg.Kubeconfig.SignTimeoutSeconds, "ORBIT_DEVENV_KUBECONFIG_SIGN_TIMEOUT_SECONDS")
	overrideString(&cfg.APIServer.ExternalAddress, "ORBIT_DEVENV_API_SERVER_EXTERNAL_ADDRESS")
	overrideString(&cfg.DevEnvironmentID, "ORBIT_DEV_ENVIRONMENT_ID")

	if cfg.APIServer.ExternalAddress == "" {
		return nil, fmt.Errorf("devenvironments config: apiServer.externalAddress is required — set ORBIT_DEVENV_API_SERVER_EXTERNAL_ADDRESS")
	}
	return cfg, nil
}

func overrideInt(dst *int, envVar string) {
	if v := os.Getenv(envVar); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			*dst = n
		}
	}
}

func overrideString(dst *string, envVar string) {
	if v := os.Getenv(envVar); v != "" {
		*dst = v
	}
}
