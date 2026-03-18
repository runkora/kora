package config

import "time"

// AppConfig holds application-level metadata present in every service binary.
type AppConfig struct {
	// Version is the application version string, e.g. "0.1.0".
	Version string `yaml:"version"`
}

// ServerConfig holds HTTP server settings present in every service binary.
type ServerConfig struct {
	// Port is the TCP port the server listens on.
	Port int `yaml:"port"`

	// BasePath is the URL prefix for all API routes, e.g. "/api/v1".
	BasePath string `yaml:"basePath"`

	// ShutdownTimeoutSecs is the number of seconds to wait for in-flight
	// requests to complete before forcibly closing the server.
	ShutdownTimeoutSecs int `yaml:"shutdownTimeoutSecs"`
}

// ShutdownTimeout converts ShutdownTimeoutSecs to a time.Duration.
func (c ServerConfig) ShutdownTimeout() time.Duration {
	return time.Duration(c.ShutdownTimeoutSecs) * time.Second
}

// PlatformConfig holds the shared platform-level defaults loaded from
// internal/platform/config/base.yaml and its environment overrides.
// It is available as [Platform] and is the authoritative source for all
// settings that are identical across every service binary.
type PlatformConfig struct {
	Observability struct {
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
	} `yaml:"observability"`
}
