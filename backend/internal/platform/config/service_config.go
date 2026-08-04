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

// defaultReregistrationInterval is how often a service re-publishes its route
// table and scope vocabulary when nothing overrides it. The policy service
// holds registrations in memory, so a restart there drops every service's
// routes and scopes until each one publishes them again, and this bounds that
// gap. Five minutes trades a negligible amount of traffic — one idempotent call
// per service — against the window in which scoped requests are denied.
const defaultReregistrationInterval = 5 * time.Minute

// PolicyRegistrationConfig holds the re-registration cadence, shared by every
// binary that keeps itself registered with the policy service. Embed it into a
// service's policy block with yaml:",squash" so the key stays
// policy.reregistrationIntervalSecs and the accessor is promoted onto the block.
//
// The value lives in one place because nothing would set it per service: the
// recovery window after a policy-service restart is a property of the platform,
// not of any one binary.
type PolicyRegistrationConfig struct {
	// ReregistrationIntervalSecs overrides the cadence, in seconds. Normally
	// unset, in which case defaultReregistrationInterval applies.
	ReregistrationIntervalSecs int `yaml:"reregistrationIntervalSecs"`
}

// ReregistrationInterval returns how often to re-publish routes and scopes,
// falling back to defaultReregistrationInterval when unset or non-positive.
func (c PolicyRegistrationConfig) ReregistrationInterval() time.Duration {
	if c.ReregistrationIntervalSecs <= 0 {
		return defaultReregistrationInterval
	}
	return time.Duration(c.ReregistrationIntervalSecs) * time.Second
}
