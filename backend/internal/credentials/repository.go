package credentials

import "context"

// ConnectionRepository is the read contract for tenant-scoped connection
// registrations the egress gateway serves. Since Amendment 3 connections are
// GitOps CRs aggregated by the operator into a ConfigMap the gateway mounts
// (see configmap.go), so this interface is read-only — the broker resolves a
// connection by host, and the ScopePublisher enumerates connections to publish
// the scope vocabulary. Mutation lives in the CR lifecycle, not here.
//
// Cross-tenant lookups do not exist on GetByHost; every host lookup is scoped by
// tenant.
type ConnectionRepository interface {
	// GetByHost retrieves the connection whose base_host matches host within a
	// tenant. Returns ErrConnectionNotFound if no connection serves that host.
	// This is the lookup the egress gateway performs on every outbound call —
	// the agent's request carries the real upstream Host (github.com,
	// api.amazonaws.com, …), and the gateway resolves the connection from it
	// without any special headers.
	GetByHost(ctx context.Context, tenantID, host string) (*Connection, error)

	// ListByTenant returns all connections registered for a tenant. An empty
	// tenantID returns every connection across all tenants, which the
	// ScopePublisher uses to publish the full connection scope vocabulary.
	ListByTenant(ctx context.Context, tenantID string) ([]*Connection, error)
}
