package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ── Environments ────────────────────────────────────────────────────────────

// name holds the slug and display_name the name for people.
const environmentColumns = `id, tenant_id, name, display_name, description, sandbox_image, cpu, memory, disk, egress_hosts, connectors, created_at, updated_at`

func (r *PostgresRepository) CreateEnvironment(ctx context.Context, env Environment) error {
	if env.ID == "" {
		env.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	connectorsJSON, err := marshalConnectors(env.Connectors)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO environments (`+environmentColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb,$12,$13)`,
		env.ID, env.TenantID, env.Slug, env.Name, env.Description, env.SandboxImage,
		env.Size.CPU, env.Size.Memory, env.Size.Disk, env.EgressHosts, connectorsJSON, now, now,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrEnvironmentSlugTaken
		}
		return fmt.Errorf("insert environment: %w", err)
	}
	return nil
}

func (r *PostgresRepository) GetEnvironment(ctx context.Context, tenantID, id string) (*Environment, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+environmentColumns+` FROM environments WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	return scanEnvironment(row)
}

func (r *PostgresRepository) ListEnvironments(ctx context.Context, tenantID string) ([]Environment, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+environmentColumns+` FROM environments WHERE tenant_id = $1 ORDER BY name`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("query environments: %w", err)
	}
	defer rows.Close()

	var out []Environment
	for rows.Next() {
		e, err := scanEnvironment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

// ListEnvironmentsByIDs returns a tenant's environments with the given IDs, in
// the order of ids. An ID the tenant has no environment for is left out.
func (r *PostgresRepository) ListEnvironmentsByIDs(ctx context.Context, tenantID string, ids []string) ([]Environment, error) {
	if len(ids) == 0 {
		return []Environment{}, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+environmentColumns+` FROM environments
		WHERE tenant_id = $1 AND id = ANY($2)
		ORDER BY array_position($2, id)`, tenantID, ids)
	if err != nil {
		return nil, fmt.Errorf("query environments by id: %w", err)
	}
	defer rows.Close()

	out := []Environment{}
	for rows.Next() {
		e, err := scanEnvironment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) UpdateEnvironment(ctx context.Context, env Environment) error {
	connectorsJSON, err := marshalConnectors(env.Connectors)
	if err != nil {
		return err
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE environments
		SET display_name=$1, description=$2, sandbox_image=$3, cpu=$4, memory=$5, disk=$6, egress_hosts=$7, connectors=$8::jsonb, updated_at=$9
		WHERE tenant_id=$10 AND id=$11`,
		env.Name, env.Description, env.SandboxImage, env.Size.CPU, env.Size.Memory, env.Size.Disk,
		env.EgressHosts, connectorsJSON, time.Now().UTC(), env.TenantID, env.ID,
	)
	if err != nil {
		return fmt.Errorf("update environment: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteEnvironment refuses an environment an agent has attached.
func (r *PostgresRepository) DeleteEnvironment(ctx context.Context, tenantID, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM environments WHERE tenant_id=$1 AND id=$2`, tenantID, id)
	if err != nil {
		if isForeignKeyViolation(err) {
			return ErrEnvironmentInUse
		}
		return fmt.Errorf("delete environment: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListAgentTypesByEnvironment returns the agents in a tenant an environment is attached to.
func (r *PostgresRepository) ListAgentTypesByEnvironment(ctx context.Context, tenantID, environmentID string) ([]AgentType, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+agentTypeColumns+`
		FROM agent_types
		WHERE tenant_id = $1 AND deleted_at IS NULL
		  AND id IN (SELECT agent_type_id FROM agent_environments WHERE environment_id = $2)
		ORDER BY created_at DESC`, tenantID, environmentID)
	if err != nil {
		return nil, fmt.Errorf("query agent_types by environment: %w", err)
	}
	defer rows.Close()

	var out []AgentType
	for rows.Next() {
		at, err := scanAgentType(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *at)
	}
	return out, rows.Err()
}

// marshalConnectors serializes a connector slice to JSONB text, normalizing nil
// to [] so the column never stores a JSON null.
func marshalConnectors(connectors []Connector) (string, error) {
	if connectors == nil {
		connectors = []Connector{}
	}
	b, err := json.Marshal(connectors)
	if err != nil {
		return "", fmt.Errorf("marshal connectors: %w", err)
	}
	return string(b), nil
}

func scanEnvironment(s scanner) (*Environment, error) {
	var e Environment
	var connectorsJSON []byte
	err := s.Scan(&e.ID, &e.TenantID, &e.Slug, &e.Name, &e.Description, &e.SandboxImage,
		&e.Size.CPU, &e.Size.Memory, &e.Size.Disk, &e.EgressHosts, &connectorsJSON, &e.CreatedAt, &e.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan environment: %w", err)
	}
	if len(connectorsJSON) > 0 {
		if uErr := json.Unmarshal(connectorsJSON, &e.Connectors); uErr != nil {
			return nil, fmt.Errorf("unmarshal environment connectors: %w", uErr)
		}
	}
	// A row the previous release wrote has no display name.
	if e.Name == "" {
		e.Name = e.Slug
	}
	if e.Connectors == nil {
		e.Connectors = []Connector{}
	}
	if e.EgressHosts == nil {
		e.EgressHosts = []string{}
	}
	return &e, nil
}
