package credentials

import "context"

// ConnectionRepository is the storage contract for tenant-scoped connection
// registrations. Cross-tenant lookups do not exist; every method is scoped by
// tenant.
type ConnectionRepository interface {
	// Create persists a new connection. The ID is generated internally.
	Create(ctx context.Context, c *Connection) error

	// GetByID retrieves a connection by ID within a tenant. Returns
	// ErrConnectionNotFound if absent.
	GetByID(ctx context.Context, id, tenantID string) (*Connection, error)

	// GetByName retrieves a connection by its agent-facing name within a tenant.
	// Returns ErrConnectionNotFound if absent. Used by the admin/registry CRUD
	// surface, which addresses connections by their human-friendly name.
	GetByName(ctx context.Context, tenantID, name string) (*Connection, error)

	// GetByHost retrieves the connection whose base_host matches host within a
	// tenant. Returns ErrConnectionNotFound if no connection serves that host.
	// This is the lookup the egress gateway performs on every outbound call —
	// the agent's request carries the real upstream Host (github.com,
	// api.amazonaws.com, …), and the gateway resolves the connection from it
	// without any special headers.
	GetByHost(ctx context.Context, tenantID, host string) (*Connection, error)

	// ListByTenant returns all connections registered for a tenant.
	ListByTenant(ctx context.Context, tenantID string) ([]*Connection, error)

	// Delete removes a connection by ID within a tenant. Returns
	// ErrConnectionNotFound if absent.
	Delete(ctx context.Context, id, tenantID string) error
}
