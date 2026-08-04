// Package triggersConfig holds the configuration for the orbit-triggers binary.
package triggersConfig

import (
	"time"

	"github.com/clear-route/orbit/internal/platform/config"
)

// Config holds orbit-triggers configuration values.
//
// Values are loaded from:
//
//  1. cmd/orbit-triggers/config/base.yaml      — service defaults
//  2. cmd/orbit-triggers/config/{env}.yaml     — env overrides (optional)
//
// orbit-triggers owns the platform's inbound surfaces for starting work: the
// webhook ingestion endpoint, the trigger management API, the cron scheduler,
// and the manual fire endpoint. It is deliberately separate from identity so
// that unauthenticated, internet-reachable ingestion does not share a process
// with the platform's token authority.
// See docs/adr/2026-06-20-extended-trigger-types.md, Decision 1.
type Config struct {
	App struct {
		Version string `yaml:"version"`
	} `yaml:"app"`

	Server struct {
		// Port serves webhook ingestion, the management API, and health.
		Port int `yaml:"port"`
		// BasePath prefixes every route. The webhook ingestion path is
		// BasePath + "/webhooks/", which the api VirtualService routes here.
		// It must stay stable: registered sources post to it.
		BasePath            string `yaml:"basePath"`
		ShutdownTimeoutSecs int    `yaml:"shutdownTimeoutSecs"`
	} `yaml:"server"`

	Identity struct {
		// InternalURL is the in-cluster base URL for identity. Used to mint
		// each trigger's short-lived SERVICE token. Authentication is the
		// mesh's mTLS peer identity (SPIRE SVID via XFCC), so no credential
		// material is configured here.
		InternalURL string `yaml:"internalURL"`
	} `yaml:"identity"`

	Agents struct {
		// InternalURL is the in-cluster base URL for orbit-agents, the
		// dispatch target for every trigger kind.
		InternalURL string `yaml:"internalURL"`
	} `yaml:"agents"`

	Policy struct {
		// PolicyServiceInternalURL is the base URL for route+scope registration.
		PolicyServiceInternalURL string `yaml:"policyServiceInternalURL"`
		// BundleEndpoint is the policy-service bundle-polling endpoint the
		// in-process evaluator polls for authorization decisions.
		BundleEndpoint string `yaml:"bundleEndpoint"`
		// BundlePollIntervalSecs is how often the evaluator polls for bundles.
		// Kept short so a grant created alongside a trigger is visible to the
		// next delivery rather than up to a poll interval later.
		BundlePollIntervalSecs int `yaml:"bundlePollIntervalSecs"`
		// PolicyRegistrationConfig carries the shared cadence on which this
		// service re-registers its route+scope vocabulary, to self-heal after a
		// policy-service restart drops its in-memory registration store.
		config.PolicyRegistrationConfig `yaml:",squash"`
	} `yaml:"policy"`

	Observability struct {
		ServiceName string `yaml:"serviceName"`
	} `yaml:"observability"`
}

// ShutdownTimeout returns the configured graceful-shutdown timeout.
func (c *Config) ShutdownTimeout() time.Duration {
	return time.Duration(c.Server.ShutdownTimeoutSecs) * time.Second
}

// BundlePollInterval returns the evaluator bundle-poll interval, default 30s.
func (c *Config) BundlePollInterval() time.Duration {
	if c.Policy.BundlePollIntervalSecs <= 0 {
		return 30 * time.Second
	}
	return time.Duration(c.Policy.BundlePollIntervalSecs) * time.Second
}

// Current holds non-secret configuration for orbit-triggers.
var Current = config.MustLoadService[Config]("cmd/orbit-triggers", config.Environment())
