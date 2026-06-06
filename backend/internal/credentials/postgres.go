package credentials

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// postgresRepository is the Postgres-backed ConnectionRepository.
type postgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository creates a ConnectionRepository backed by PostgreSQL.
func NewPostgresRepository(pool *pgxpool.Pool) ConnectionRepository {
	return &postgresRepository{pool: pool}
}

func (r *postgresRepository) Create(ctx context.Context, c *Connection) error {
	c.ID = uuid.New().String()
	now := time.Now().UTC()
	c.CreatedAt = now
	c.UpdatedAt = now

	ceiling, err := json.Marshal(c.ScopeCeiling)
	if err != nil {
		return fmt.Errorf("marshal scope ceiling: %w", err)
	}
	config := c.Config
	if len(config) == 0 {
		config = json.RawMessage("{}")
	}

	_, err = r.pool.Exec(ctx, `
		INSERT INTO connections
			(id, tenant_id, name, adapter, base_host, secret_ref, scope_ceiling, config, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, c.ID, c.TenantID, c.Name, string(c.Adapter), c.BaseHost, c.SecretRef, ceiling, []byte(config), c.CreatedAt, c.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create connection: %w", err)
	}
	return nil
}

func (r *postgresRepository) GetByID(ctx context.Context, id, tenantID string) (*Connection, error) {
	row := r.pool.QueryRow(ctx, selectColumns+` WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	return scanConnection(row)
}

func (r *postgresRepository) GetByName(ctx context.Context, tenantID, name string) (*Connection, error) {
	row := r.pool.QueryRow(ctx, selectColumns+` WHERE tenant_id = $1 AND name = $2`, tenantID, name)
	return scanConnection(row)
}

func (r *postgresRepository) GetByHost(ctx context.Context, tenantID, host string) (*Connection, error) {
	row := r.pool.QueryRow(ctx, selectColumns+` WHERE tenant_id = $1 AND base_host = $2`, tenantID, host)
	return scanConnection(row)
}

func (r *postgresRepository) ListByTenant(ctx context.Context, tenantID string) ([]*Connection, error) {
	rows, err := r.pool.Query(ctx, selectColumns+` WHERE tenant_id = $1 ORDER BY name`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list connections: %w", err)
	}
	defer rows.Close()

	var conns []*Connection
	for rows.Next() {
		c, err := scanConnection(rows)
		if err != nil {
			return nil, err
		}
		conns = append(conns, c)
	}
	return conns, rows.Err()
}

func (r *postgresRepository) Delete(ctx context.Context, id, tenantID string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM connections WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	if err != nil {
		return fmt.Errorf("delete connection: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrConnectionNotFound
	}
	return nil
}

const selectColumns = `
	SELECT id, tenant_id, name, adapter, base_host, secret_ref, scope_ceiling, config, created_at, updated_at
	FROM connections`

// scanConnection scans a single row into a Connection. Accepts pgx.Row so it
// serves both QueryRow and Query iteration.
func scanConnection(row pgx.Row) (*Connection, error) {
	var (
		c           Connection
		adapter     string
		ceilingJSON []byte
		configJSON  []byte
	)
	err := row.Scan(&c.ID, &c.TenantID, &c.Name, &adapter, &c.BaseHost, &c.SecretRef, &ceilingJSON, &configJSON, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrConnectionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan connection: %w", err)
	}

	c.Adapter = AdapterType(adapter)
	if len(ceilingJSON) > 0 {
		if err := json.Unmarshal(ceilingJSON, &c.ScopeCeiling); err != nil {
			return nil, fmt.Errorf("unmarshal scope ceiling: %w", err)
		}
	}
	c.Config = json.RawMessage(configJSON)
	return &c, nil
}
