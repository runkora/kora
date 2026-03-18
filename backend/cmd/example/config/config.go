// Package exampleConfig holds the configuration for the example service binary.
package exampleConfig

import (
	"time"

	"github.com/clear-route/orbit/internal/platform/config"
)

// Config holds the example service's own configuration values.
// Platform-wide settings (OTLP endpoint, sampling rate, log format) are
// available via [config.Platform] and are not repeated here.
//
// Values are loaded from:
//
//  1. cmd/example/config/base.yaml        — service defaults
//  2. cmd/example/config/{env}.yaml       — service env overrides (optional)
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

// ShutdownTimeout returns the server shutdown timeout as a time.Duration.
func (c *Config) ShutdownTimeout() time.Duration {
	return time.Duration(c.Server.ShutdownTimeoutSecs) * time.Second
}

// Current holds all non-secret configuration for the example service.
// Secrets must be supplied via environment variables — never via config files.
var Current = config.MustLoadService[Config]("cmd/example", config.Environment())
