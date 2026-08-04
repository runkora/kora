// Package policyServiceConfig holds the configuration for the policy-service binary.
package policyServiceConfig

import (
	"os"
	"time"

	"github.com/clear-route/orbit/internal/platform/config"
)

// Config holds policy-service configuration values.
//
// Values are loaded from:
//
//  1. cmd/policy-service/config/base.yaml        — service defaults
//  2. cmd/policy-service/config/{env}.yaml       — env overrides (optional)
type Config struct {
	App struct {
		Version string `yaml:"version"`
	} `yaml:"app"`

	Server struct {
		Port                int    `yaml:"port"`
		BasePath            string `yaml:"basePath"`
		ShutdownTimeoutSecs int    `yaml:"shutdownTimeoutSecs"`
	} `yaml:"server"`

	// Bundle holds cluster-specific values compiled into every OPA bundle.
	// These are read by authz.rego via data.config.* so the Rego itself
	// remains cluster-agnostic.
	Bundle struct {
		// JWKSURL is the URL of the identity service JWKS endpoint.
		// Set via POLICY_SERVICE_BUNDLE_JWKS_URL env var.
		JWKSURL string `yaml:"jwksURL"`
		// Issuer is the expected JWT issuer claim.
		Issuer string `yaml:"issuer"`
		// Audience is the expected JWT audience claim.
		Audience string `yaml:"audience"`
		// FrontendHost is the hostname (without port) of the frontend service.
		// Compiled into the gateway bundle so authz_gateway.rego can bypass
		// OPA enforcement for browser UI traffic (`:authority` header match).
		FrontendHost string `yaml:"frontendHost"`
	} `yaml:"bundle"`

	// Policy carries the shared cadence on which the policy service re-registers
	// its OWN routes and platform scope vocabulary. The registration is an
	// in-process call, but the scope upserts hit Postgres, so a database blip at
	// boot would otherwise leave the platform:* vocabulary unpublished for the
	// life of the process.
	Policy config.PolicyRegistrationConfig `yaml:"policy"`

	Observability struct {
		ServiceName string `yaml:"serviceName"`
	} `yaml:"observability"`
}

func (c *Config) ShutdownTimeout() time.Duration {
	return time.Duration(c.Server.ShutdownTimeoutSecs) * time.Second
}

// Current holds non-secret configuration for the policy-service.
var Current = mustLoad()

func mustLoad() *Config {
	cfg := config.MustLoadService[Config]("cmd/policy-service", config.Environment())
	// Allow the Helm chart to inject the frontend hostname at deploy time without
	// rebuilding the image. The base.yaml default is intentionally empty so that
	// production deployments with a missing override fail-safe (Envoy-level bypass
	// still handles frontend traffic; the Rego bypass simply never fires).
	if v := os.Getenv("POLICY_SERVICE_BUNDLE_FRONTEND_HOST"); v != "" {
		cfg.Bundle.FrontendHost = v
	}
	return cfg
}
