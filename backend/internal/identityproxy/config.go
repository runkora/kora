package identityproxy

import "time"

// Config holds the runtime tunables for the identity-proxy sidecar.
// Values are populated by the service binary from the YAML config files under
// cmd/identity-proxy/config/. Tests supply values directly via a local helper.
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
	// proactively re-exchange the token (ADR-0021: expiry minus 60 s).
	RefreshThreshold time.Duration

	// HealthAddr is the TCP address the HTTP health server listens on.
	// Istio probes it via the sidecar at /app-health/identity-proxy/readyz.
	HealthAddr string

	// EnvoyReadyTimeout is the maximum time to wait for Envoy to signal it is
	// fully initialized (xDS received, mTLS cert loaded) before giving up.
	EnvoyReadyTimeout time.Duration
}
