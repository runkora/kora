// Package policyServiceConfig holds the configuration for the policy-service binary.
package policyServiceConfig

import (
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

	Observability struct {
		ServiceName string `yaml:"serviceName"`
	} `yaml:"observability"`
}

func (c *Config) ShutdownTimeout() time.Duration {
	return time.Duration(c.Server.ShutdownTimeoutSecs) * time.Second
}

// Current holds non-secret configuration for the policy-service.
var Current = config.MustLoadService[Config]("cmd/policy-service", config.Environment())
