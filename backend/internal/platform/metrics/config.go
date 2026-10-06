// Package metrics bridges a service's OpenTelemetry instruments to a
// pull-based Prometheus /metrics endpoint. See
// docs/adr/2026-10-06-prometheus-metrics-endpoints.md.
package metrics

import (
	"os"
	"strconv"
	"time"
)

// Config is one service's Prometheus-scrape tunables. Unlike
// observability.DefaultConfig this is not a package-level singleton: whether
// metrics are enabled, and on which port, is a per-service decision (an
// operator may want it off for one noisy internal service), so each service's
// own cmd/{service}/config/config.go embeds an instance of this struct.
//
// The zero value is disabled: a service that embeds Config and forgets to set
// it gets no new listening port by default, rather than silently exposing one.
type Config struct {
	// Enabled turns the Prometheus reader and its HTTP server on.
	Enabled bool `yaml:"enabled"`

	// Port is the dedicated metrics server's TCP port, separate from the
	// service's main API port so scrape traffic never shares a listener (or a
	// NetworkPolicy rule) with authenticated application traffic.
	Port int `yaml:"port"`

	// Path is the scrape path. Defaults to "/metrics" when empty.
	Path string `yaml:"path"`

	// ShutdownTimeoutSecs bounds graceful shutdown of the metrics server.
	ShutdownTimeoutSecs int `yaml:"shutdownTimeoutSecs"`

	// Namespace is an optional prefix the Prometheus exporter prepends to
	// every metric name. Left empty in every known use today: instrument
	// names already carry an "orbit.<service>." prefix (the OTel dotted
	// convention, rendered with underscores by the exporter), so a configured
	// namespace here would double up (e.g. orbit_orbit_triggers_...). Exists
	// for the rare instrument created without that prefix.
	Namespace string `yaml:"namespace"`

	// CollectGoRuntimeMetrics registers the standard Go collector (GC pause
	// times, goroutine count, memstats) on this service's registry.
	CollectGoRuntimeMetrics bool `yaml:"collectGoRuntimeMetrics"`

	// CollectProcessMetrics registers the standard process collector (RSS,
	// open FDs, CPU seconds) on this service's registry.
	CollectProcessMetrics bool `yaml:"collectProcessMetrics"`
}

// ShutdownTimeout converts ShutdownTimeoutSecs to a time.Duration.
func (c Config) ShutdownTimeout() time.Duration {
	return time.Duration(c.ShutdownTimeoutSecs) * time.Second
}

// path returns the configured scrape path, defaulting to "/metrics".
func (c Config) path() string {
	if c.Path == "" {
		return "/metrics"
	}
	return c.Path
}

// ApplyEnvOverrides lets the chart (or local dev tooling) flip metrics on/off
// or re-port it without a YAML edit, mirroring the ORBIT_OTLP_ENDPOINT-style
// overrides in observability/config.go. Each service calls this once, right
// after loading its own config.
func (c *Config) ApplyEnvOverrides() {
	if v := os.Getenv("ORBIT_METRICS_ENABLED"); v != "" {
		c.Enabled = v == "true"
	}
	if v := os.Getenv("ORBIT_METRICS_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			c.Port = p
		}
	}
}
