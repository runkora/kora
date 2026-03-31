package observability

import (
	"fmt"
	"os"
	"strconv"

	"github.com/clear-route/orbit/internal/platform/config"
)

// ObservabilityConfig holds the shared observability tunables that are identical
// across every service binary. Loaded from
// internal/platform/observability/config/base.yaml and its environment overrides.
type ObservabilityConfig struct {
	// OTLPEndpoint is the gRPC address of the OpenTelemetry collector.
	// Leave empty to disable OTLP export.
	OTLPEndpoint string `yaml:"otlpEndpoint"`

	// SamplingRate is the fraction of root spans to sample (0.0–1.0).
	SamplingRate *float64 `yaml:"samplingRate,omitempty"`

	// OTLPInsecure disables TLS on the OTLP gRPC connection.
	// Set to true only for local development.
	OTLPInsecure bool `yaml:"otlpInsecure"`

	// LogFormat controls log output style: "console" or "json".
	LogFormat string `yaml:"logFormat"`
}

// DefaultConfig holds the shared observability configuration loaded at startup.
// Service binaries and the logging package read from this instead of
// re-declaring platform tunables in every service config.
var DefaultConfig = mustLoadConfig()

func mustLoadConfig() *ObservabilityConfig {
	cfg, err := config.LoadService[ObservabilityConfig]("internal/platform/observability", config.Environment())
	if err != nil {
		panic(fmt.Errorf("observability: load config: %w", err))
	}
	applyEnvOverrides(cfg)
	return cfg
}

// applyEnvOverrides applies explicit environment variable overrides to the
// observability config after YAML loading. These let local dev tooling tweak
// individual settings (e.g. ORBIT_LOG_FORMAT=console) without APP_ENV branching.
func applyEnvOverrides(cfg *ObservabilityConfig) {
	if v := os.Getenv("ORBIT_OTLP_ENDPOINT"); v != "" {
		cfg.OTLPEndpoint = v
	}
	if v := os.Getenv("ORBIT_OTLP_INSECURE"); v != "" {
		cfg.OTLPInsecure = v == "true" || v == "1"
	}
	if v := os.Getenv("ORBIT_LOG_FORMAT"); v != "" {
		cfg.LogFormat = v
	}
	if v := os.Getenv("ORBIT_SAMPLING_RATE"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.SamplingRate = &f
		}
	}
}
