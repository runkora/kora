// Package metrics bridges a service's OpenTelemetry instruments to a
// pull-based Prometheus /metrics endpoint. See
// docs/adr/2026-10-06-prometheus-metrics-endpoints.md.
package metrics

import "time"

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

	// ShutdownTimeoutSecs bounds graceful shutdown of the metrics server.
	ShutdownTimeoutSecs int `yaml:"shutdownTimeoutSecs"`
}

// ShutdownTimeout converts ShutdownTimeoutSecs to a time.Duration.
func (c Config) ShutdownTimeout() time.Duration {
	return time.Duration(c.ShutdownTimeoutSecs) * time.Second
}
