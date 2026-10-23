package observability

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/runkora/kora/backend/internal/platform/config"
)

// SamplingRateString renders the configured sampling rate for passing to a
// workload this process provisions or injects, so it samples at the same rate.
// Empty when unset, which leaves that image on its own per-environment default.
func SamplingRateString() string {
	if DefaultConfig.SamplingRate == nil {
		return ""
	}
	return strconv.FormatFloat(*DefaultConfig.SamplingRate, 'f', -1, 64)
}

// otlpTarget splits a configured endpoint into the gRPC address and whether the
// connection is plaintext, from the scheme alone as the OpenTelemetry SDKs do.
// An endpoint without a scheme is rejected: guessing either fails the handshake
// against a plaintext collector or ships telemetry in the clear to a TLS one.
func otlpTarget(endpoint string) (addr string, insecure bool, err error) {
	switch {
	case strings.HasPrefix(endpoint, "http://"):
		addr, insecure = strings.TrimPrefix(endpoint, "http://"), true
	case strings.HasPrefix(endpoint, "https://"):
		addr, insecure = strings.TrimPrefix(endpoint, "https://"), false
	default:
		return "", false, fmt.Errorf("otlpEndpoint %q needs an http:// or https:// scheme", endpoint)
	}

	// A bare scheme leaves nothing to dial. Rejecting it here stops the empty
	// address reaching the gRPC client, which would fall back to its own
	// default target instead of failing.
	if addr = strings.TrimSuffix(addr, "/"); addr == "" {
		return "", false, fmt.Errorf("otlpEndpoint %q has no host", endpoint)
	}
	return addr, insecure, nil
}

// TenantOTLPEndpoint is the collector tenant pods export to. The chart renders
// it from the global endpoint only, so a per-service override cannot split the
// tenant export, the identity-proxy and the policy bundle across collectors.
func TenantOTLPEndpoint() string {
	if DefaultConfig.TenantOTLPEndpoint != "" {
		return DefaultConfig.TenantOTLPEndpoint
	}
	return DefaultConfig.OTLPEndpoint
}

// CollectorAuthority returns the tenant collector's host:port, the :authority
// its gRPC exporters send. Empty when no valid endpoint is set. Tenant sidecars
// use it to recognize telemetry export calls; see
// docs/adr/2026-09-18-telemetry-collector-platform-destination.md.
func CollectorAuthority() string {
	addr, _, err := otlpTarget(TenantOTLPEndpoint())
	if err != nil {
		return ""
	}
	return addr
}

// ObservabilityConfig holds the shared observability tunables that are identical
// across every service binary. Loaded from
// internal/platform/observability/config/base.yaml, its environment overlay and
// the file the chart renders.
type ObservabilityConfig struct {
	// OTLPEndpoint is the OpenTelemetry collector's gRPC URL, e.g.
	// http://otel-collector.observability.svc:4317. The scheme decides whether
	// the connection is plaintext or TLS. Leave empty to disable OTLP export.
	OTLPEndpoint string `yaml:"otlpEndpoint"`

	// TenantOTLPEndpoint overrides OTLPEndpoint for tenant pods; see TenantOTLPEndpoint().
	TenantOTLPEndpoint string `yaml:"tenantOtlpEndpoint"`

	// SamplingRate is the fraction of root spans to sample (0.0–1.0).
	SamplingRate *float64 `yaml:"samplingRate,omitempty"`

	// LogFormat controls log output style: "console" or "json".
	LogFormat string `yaml:"logFormat"`
}

// DefaultConfig holds the shared observability configuration loaded at startup.
// Service binaries and the logging package read from this instead of
// re-declaring platform tunables in every service config.
var DefaultConfig = mustLoadConfig()

func mustLoadConfig() *ObservabilityConfig {
	cfg, err := config.LoadService[ObservabilityConfig]("internal/platform/observability", config.Environment())
	if err == nil {
		err = applyGoSetEnv(cfg, os.Getenv)
	}
	if err != nil {
		panic(fmt.Errorf("observability: load config: %w", err))
	}
	return cfg
}

// applyGoSetEnv reads the collector settings kora-agents and the operator set
// on the session-manager and identity-proxy containers they build, which have
// no rendered config file (docs/adr/2026-10-06-rendered-service-config-files.md).
func applyGoSetEnv(cfg *ObservabilityConfig, getenv func(string) string) error {
	if v := getenv("KORA_OTLP_ENDPOINT"); v != "" {
		cfg.OTLPEndpoint = v
	}
	if v := getenv("KORA_SAMPLING_RATE"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return fmt.Errorf("KORA_SAMPLING_RATE %q is not a number: %w", v, err)
		}
		cfg.SamplingRate = &f
	}
	return nil
}
