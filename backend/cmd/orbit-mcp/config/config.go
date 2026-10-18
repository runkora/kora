// Package orbitMCPConfig holds the configuration for the orbit-mcp binary.
package orbitMCPConfig

import (
	"time"

	"github.com/clear-route/orbit/internal/platform/config"
	platformmetrics "github.com/clear-route/orbit/internal/platform/metrics"
)

// Config holds orbit-mcp configuration. Loaded from:
//  1. cmd/orbit-mcp/config/base.yaml      — service defaults
//  2. cmd/orbit-mcp/config/{env}.yaml     — env overrides (optional)
type Config struct {
	App struct {
		Version string `yaml:"version"`
	} `yaml:"app"`

	Server struct {
		HTTPPort            int    `yaml:"httpPort"`
		BasePath            string `yaml:"basePath"`
		ShutdownTimeoutSecs int    `yaml:"shutdownTimeoutSecs"`
	} `yaml:"server"`

	// Backends are the in-cluster base URLs of the kora services the tools call.
	Backends struct {
		Agents      string `yaml:"agents"`
		Policy      string `yaml:"policy"`
		Inference   string `yaml:"inference"`
		Identity    string `yaml:"identity"`
		Triggers    string `yaml:"triggers"`
		ToolGateway string `yaml:"toolGateway"`
	} `yaml:"backends"`

	Observability struct {
		ServiceName string `yaml:"serviceName"`
	} `yaml:"observability"`

	Metrics platformmetrics.Config `yaml:"metrics"`
}

func (c *Config) ShutdownTimeout() time.Duration {
	return time.Duration(c.Server.ShutdownTimeoutSecs) * time.Second
}

// Current holds non-secret configuration for orbit-mcp.
var Current = config.MustLoadService[Config]("cmd/orbit-mcp", config.Environment())
