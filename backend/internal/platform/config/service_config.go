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
