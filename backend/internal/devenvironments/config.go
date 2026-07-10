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
}

// QuotaConfig bounds how many active environments a single principal may hold.
type QuotaConfig struct {
	// MaxActivePerOwner is decision 8's per-principal cap.
	MaxActivePerOwner int `yaml:"maxActivePerOwner"`
}

// KubeconfigConfig controls the kubeconfig-minting endpoint.
type KubeconfigConfig struct {
	// SignTimeoutSeconds bounds how long Kubeconfig() polls the CR's status
	// for the operator to sign a freshly submitted CSR.
	SignTimeoutSeconds int `yaml:"signTimeoutSeconds"`
}

// SignTimeout returns Kubeconfig.SignTimeoutSeconds as a time.Duration.
func (c KubeconfigConfig) SignTimeout() time.Duration {
	return time.Duration(c.SignTimeoutSeconds) * time.Second
}

// APIServerConfig resolves the "vcluster API server external-reachability
// question" the ADR flagged as blocking: status.ingressHost is only a
// hostname used for SNI-based TLS-passthrough routing (see the operator's
// devenvironment_ingress.go) — it is never required to resolve via DNS. The
// kubeconfig's actual server: address is this separately-configured,
// genuinely reachable host:port (the shared Istio ingress gateway's
// TLS-passthrough listener), decoupled from the per-environment SNI value via
// the kubeconfig's tls-server-name field (assembleKubeconfig).
type APIServerConfig struct {
	// ExternalAddress is a "host:port" pointing at the shared ingress
	// gateway's TLS-passthrough listener (operator's devEnvAPIGatewayPort).
	// Local dev: the k3d loadbalancer's mapped port on localhost — no DNS
	// needed at all, since SNI routing carries the environment identity, not
	// this address. Staging: a stable DNS name for the gateway's Service.
	// Override with ORBIT_DEVENV_API_SERVER_EXTERNAL_ADDRESS.
	ExternalAddress string `yaml:"externalAddress"`
}

// LoadConfig loads the devenvironments feature configuration from
// internal/devenvironments/config/base.yaml and applies environment variable
// overrides.
func LoadConfig() (*Config, error) {
	cfg, err := config.Load[Config]("internal/devenvironments", "base")
	if err != nil {
		return nil, fmt.Errorf("load devenvironments config: %w", err)
	}
	overrideInt(&cfg.Quota.MaxActivePerOwner, "ORBIT_DEVENV_MAX_ACTIVE_PER_OWNER")
	overrideInt(&cfg.Kubeconfig.SignTimeoutSeconds, "ORBIT_DEVENV_KUBECONFIG_SIGN_TIMEOUT_SECONDS")
	overrideString(&cfg.APIServer.ExternalAddress, "ORBIT_DEVENV_API_SERVER_EXTERNAL_ADDRESS")
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
