package orbitConfig

import (
	"time"

	"github.com/clear-route/orbit/internal/platform/config"
)

// OrbitConfig is the top-level configuration structure for the Orbit binary.
type OrbitConfig struct {
	App struct {
		// Version is the application version string, e.g. "0.1.0".
		Version string `yaml:"version"`

		// LogFormat controls the logging output style.
		// Use "console" for human-readable development output,
		// or "json" for structured machine-parseable production output.
		LogFormat string `yaml:"logFormat"`
	} `yaml:"app"`

	Server struct {
		// Port is the TCP port the server listens on.
		Port int `yaml:"port"`

		// BasePath is the URL prefix for all API routes, e.g. "/api/v1".
		BasePath string `yaml:"basePath"`

		// ShutdownTimeoutSecs is the number of seconds to wait for in-flight
		// requests to complete before forcibly closing the server.
		ShutdownTimeoutSecs int `yaml:"shutdownTimeoutSecs"`
	} `yaml:"server"`

	Observability struct {
		// ServiceName is the OTEL service name used to identify this process in
		// distributed traces, e.g. "orbit".
		ServiceName string `yaml:"serviceName"`

		// OTLPEndpoint is the gRPC address of the OpenTelemetry collector, e.g.
		// "localhost:4317". Leave empty to disable OTLP export; traces will be
		// written to stdout in local/development environments, or discarded in
		// production.
		OTLPEndpoint string `yaml:"otlpEndpoint"`

		// SamplingRate is the fraction of root spans to sample (0.0–1.0).
		// Use 1.0 locally to capture every trace; lower values (e.g. 0.1) in
		// production to reduce collector load.
		// If omitted from config, defaults to 0.1 (10% sampling).
		// Set to 0.0 to disable tracing entirely.
		SamplingRate *float64 `yaml:"samplingRate,omitempty"`

		// OTLPInsecure disables TLS on the OTLP gRPC connection. Set to true
		// only for local development where the collector runs without TLS.
		OTLPInsecure bool `yaml:"otlpInsecure"`
	} `yaml:"observability"`
}

// ShutdownTimeout converts ShutdownTimeoutSecs to a time.Duration.
func (s OrbitConfig) ShutdownTimeout() time.Duration {
	return time.Duration(s.Server.ShutdownTimeoutSecs) * time.Second
}

// Environment returns the current deployment environment derived from APP_ENV.
// This is the same value used to select the YAML config file, and is passed
// to the observability layer so it can choose the appropriate exporter.
func (OrbitConfig) Environment() string {
	return config.Environment()
}

// Config holds all non-secret, environment-specific configuration for the
// Orbit control plane binary. Secrets (database passwords, API keys, etc.)
// must be supplied via environment variables or a secret manager — never here.
//
// The config file is resolved at startup from:
//
//	cmd/orbit/config/{APP_ENV}.yaml
//
// where APP_ENV defaults to "local" when unset.
var Config = config.MustLoad[OrbitConfig]("cmd/orbit", config.Environment())
