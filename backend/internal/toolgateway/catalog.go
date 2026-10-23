// MCP tool catalog (Phase 3.4).
//
// The catalog is the tool-gateway's read-only projection of what each MCP server
// exposes — display metadata (description, input schema) for the admin UI. It is
// DERIVED state: the policy scope vocabulary remains the source of truth for
// authorisation; this table is rebuilt on each introspection.
//
// It lives with the tool-gateway (NOT kora-agents): the catalog is an MCP
// governance concern, and the tool-gateway is a plain-HTTP mesh service the BFF
// can reach directly — unlike the egress gateway (a CONNECT broker), which is
// why that one's catalog was offloaded. See the 2026-07-01 amendment to
// docs/adr/2026-06-29-mcp-server-registration-and-introspection.md.
package toolgateway

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// CatalogTool is one row of the mcp_tools catalog.
type CatalogTool struct {
	TenantID     string          `json:"tenant_id"`
	ServerSlug   string          `json:"server_slug"`
	ToolName     string          `json:"tool_name"`
	Scope        string          `json:"scope"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"input_schema"`
	DiscoveredAt time.Time       `json:"discovered_at"`
}

// CatalogRepository is the storage contract for the mcp_tools catalog.
type CatalogRepository interface {
	// Replace rebuilds the entire catalog transactionally from the current
	// introspection. The tool-gateway publisher is the single writer (replicas=1).
	Replace(ctx context.Context, tools []CatalogTool) error
	// ListByTenant returns the tenant's own tools plus all global ("") tools.
	ListByTenant(ctx context.Context, tenantID string) ([]CatalogTool, error)
}

// PostgresCatalog implements CatalogRepository against a pgxpool.
type PostgresCatalog struct {
	pool *pgxpool.Pool
}

// NewPostgresCatalog creates a catalog repository backed by the given pool.
func NewPostgresCatalog(pool *pgxpool.Pool) *PostgresCatalog {
	return &PostgresCatalog{pool: pool}
}

// Replace rebuilds the catalog: delete every row, insert the current set, in one
// transaction so a reader never sees a half-rebuilt catalog.
func (c *PostgresCatalog) Replace(ctx context.Context, tools []CatalogTool) error {
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin catalog tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `DELETE FROM mcp_tools`); err != nil {
		return fmt.Errorf("clear mcp_tools: %w", err)
	}

	for _, t := range tools {
		schema := t.InputSchema
		if len(schema) == 0 {
			schema = json.RawMessage("{}")
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO mcp_tools (tenant_id, server_slug, tool_name, scope, description, input_schema, discovered_at)
			 VALUES ($1, $2, $3, $4, $5, $6, now())`,
			t.TenantID, t.ServerSlug, t.ToolName, t.Scope, t.Description, []byte(schema),
		); err != nil {
			return fmt.Errorf("insert mcp_tools row %q/%q: %w", t.ServerSlug, t.ToolName, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit catalog tx: %w", err)
	}
	return nil
}

// ListByTenant returns the tenant's own tools plus all global ("") tools,
// ordered for a stable UI.
func (c *PostgresCatalog) ListByTenant(ctx context.Context, tenantID string) ([]CatalogTool, error) {
	rows, err := c.pool.Query(ctx,
		`SELECT tenant_id, server_slug, tool_name, scope, description, input_schema, discovered_at
		   FROM mcp_tools
		  WHERE tenant_id = $1 OR tenant_id = ''
		  ORDER BY server_slug, tool_name`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("query mcp_tools: %w", err)
	}
	defer rows.Close()

	var out []CatalogTool
	for rows.Next() {
		var t CatalogTool
		var schema []byte
		if err := rows.Scan(&t.TenantID, &t.ServerSlug, &t.ToolName, &t.Scope, &t.Description, &schema, &t.DiscoveredAt); err != nil {
			return nil, fmt.Errorf("scan mcp_tools row: %w", err)
		}
		t.InputSchema = schema
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate mcp_tools rows: %w", err)
	}
	return out, nil
}

// CatalogToolsFromSnapshots flattens the introspection snapshots into catalog
// rows, computing each tool's leaf scope. Only servers with tools (OK or
// degraded-with-retained) contribute.
func CatalogToolsFromSnapshots(snapshots []ServerSnapshot) []CatalogTool {
	var out []CatalogTool
	for si := range snapshots {
		snap := &snapshots[si]
		for _, t := range snap.Tools {
			scope := scopeString(toolScopePath(snap.Server.Name, t.Name))
			out = append(out, CatalogTool{
				TenantID:    snap.Server.TenantID,
				ServerSlug:  snap.Server.Name,
				ToolName:    t.Name,
				Scope:       scope,
				Description: t.Description,
				InputSchema: t.InputSchema,
			})
		}
	}
	return out
}
