// Package policyServiceConfig holds the configuration for the policy-service binary.
package policyServiceConfig

import (
	"time"

	"github.com/clear-route/orbit/internal/platform/config"
	platformmetrics "github.com/clear-route/orbit/internal/platform/metrics"
)

// Config holds policy-service configuration values.
//
// Values are loaded from:
//
//  1. cmd/policy-service/config/base.yaml        — service defaults
//  2. cmd/policy-service/config/{env}.yaml       — env overrides (optional)
//  3. /etc/orbit/config/cmd/policy-service.yaml  — rendered by the chart
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
		JWKSURL string `yaml:"jwksURL"`
		// Issuer is the expected JWT issuer claim.
		Issuer string `yaml:"issuer"`
		// Audience is the expected JWT audience claim.
		Audience string `yaml:"audience"`
		// FrontendHost is the hostname (without port) of the frontend service.
		// Compiled into the gateway bundle so authz_gateway.rego can bypass
		// OPA enforcement for browser UI traffic (`:authority` header match).
		FrontendHost string `yaml:"frontendHost"`
		// TenantsPath is the operator's tenant registry; the gateway bundle
		// lists every tenant in it.
		TenantsPath string `yaml:"tenantsPath"`
	} `yaml:"bundle"`

	Observability struct {
		ServiceName string `yaml:"serviceName"`
	} `yaml:"observability"`

	Metrics platformmetrics.Config `yaml:"metrics"`
}

func (c *Config) ShutdownTimeout() time.Duration {
	return time.Duration(c.Server.ShutdownTimeoutSecs) * time.Second
}

// Current holds non-secret configuration for the policy-service.
var Current = config.MustLoadService[Config]("cmd/policy-service", config.Environment())
