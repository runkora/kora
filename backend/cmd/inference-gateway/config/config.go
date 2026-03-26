package config

import (
	"time"

	"github.com/clear-route/orbit/internal/platform/config"
)

// Config is the root configuration for the inference-gateway service.
//
// Values are loaded from cmd/inference-gateway/config/{APP_ENV}.yaml at
// startup. The YAML provides build-time defaults that are baked into the
type Config struct {
	App           config.AppConfig    `yaml:"app"`
	Server        config.ServerConfig `yaml:"server"`
	Observability struct {
		ServiceName string `yaml:"serviceName"`
	} `yaml:"observability"`
	Pricing PricingConfig `yaml:"pricing"`
}

// PricingConfig configures the static pricing cache.
type PricingConfig struct {
	// RefreshIntervalSecs is how often the pricing cache refreshes.
	RefreshIntervalSecs int `yaml:"refreshIntervalSecs"`
}

// ShutdownTimeout returns the server shutdown timeout as a time.Duration.
func (c *Config) ShutdownTimeout() time.Duration {
	return time.Duration(c.Server.ShutdownTimeoutSecs) * time.Second
}

// Current is the loaded configuration singleton, populated at init time.
var Current *Config

func init() {
	Current = config.MustLoadService[Config]("cmd/inference-gateway", config.Environment())
}
