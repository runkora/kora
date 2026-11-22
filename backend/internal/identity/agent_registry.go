package identity

import (
	"context"
	"fmt"

	"github.com/runkora/kora/backend/internal/platform/storage"
)

// AgentRegistry reports whether a slug names a live agent in a tenant.
type AgentRegistry interface {
	AgentLive(ctx context.Context, tenantID, slug string) (bool, error)
}

type postgresAgentRegistry struct {
	pool *storage.Pool
}

// NewPostgresAgentRegistry reads agent_types, the table kora-agents owns.
// A row exists before the agent's first pod and keeps deleted_at once the agent is deleted.
func NewPostgresAgentRegistry(pool *storage.Pool) AgentRegistry {
	return &postgresAgentRegistry{pool: pool}
}

func (r *postgresAgentRegistry) AgentLive(ctx context.Context, tenantID, slug string) (bool, error) {
	var live bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM agent_types
			WHERE tenant_id = $1 AND slug = $2 AND deleted_at IS NULL
		)`, tenantID, slug).Scan(&live)
	if err != nil {
		return false, fmt.Errorf("look up agent: %w", err)
	}
	return live, nil
}
