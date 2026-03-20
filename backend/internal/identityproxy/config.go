package identityproxy

import "time"

// Config holds the runtime tunables for the identity-proxy sidecar.
// All values have sensible defaults via DefaultConfig — override only what
// differs per environment via the YAML config files under cmd/identity-proxy/config/.
type Config struct {
	// SocketPath is the Unix domain socket the gRPC ext_proc server listens on.
	// Envoy's EnvoyFilter cluster definition must point at the same path.
	SocketPath string

	// ReadyTimeout is the maximum time we wait for the initial token exchange
	// to succeed before refusing to start the gRPC server. If the token cannot
	// be obtained within this window the pod fails fast so the kubelet restarts it.
	ReadyTimeout time.Duration

	// BackoffInitial is the starting retry interval when an exchange call fails.
	BackoffInitial time.Duration

	// BackoffMax is the ceiling on retry intervals (exponential backoff).
	BackoffMax time.Duration

	// PollInterval is how often the background goroutine wakes to check whether
	// the cached token is approaching expiry.
	PollInterval time.Duration

	// RefreshThreshold is how far before expiry the background goroutine should
	// proactively re-exchange the token (ADR-0021: expiry − 60 s).
	RefreshThreshold time.Duration
}

// DefaultConfig returns production-ready defaults matching the values
// documented in ADR-0021 and encoded in cmd/identity-proxy/config/base.yaml.
func DefaultConfig() Config {
	return Config{
		SocketPath:       "/run/orbit/identity-proxy.sock",
		ReadyTimeout:     25 * time.Second,
		BackoffInitial:   500 * time.Millisecond,
		BackoffMax:       30 * time.Second,
		PollInterval:     15 * time.Second,
		RefreshThreshold: 60 * time.Second,
	}
}
