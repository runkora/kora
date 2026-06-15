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

func (r *PostgresRepository) CreateEnvironment(ctx context.Context, env Environment) error {
	if env.ID == "" {
		env.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	connectorsJSON, mErr := marshalConnectors(env.Connectors)
	if mErr != nil {
		return mErr
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO environments
			(id, tenant_id, name, description, egress_hosts, connectors, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7,$8)`,
		env.ID, env.TenantID, env.Name, env.Description, env.EgressHosts, connectorsJSON, now, now,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrEnvironmentNameTaken
		}
		return fmt.Errorf("insert environment: %w", err)
	}
	return nil
}

func (r *PostgresRepository) GetEnvironment(ctx context.Context, tenantID, id string) (*Environment, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, name, description, egress_hosts, connectors, created_at, updated_at
		FROM environments WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	return scanEnvironment(row)
}

func (r *PostgresRepository) ListEnvironments(ctx context.Context, tenantID string) ([]Environment, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, name, description, egress_hosts, connectors, created_at, updated_at
		FROM environments WHERE tenant_id = $1 ORDER BY name`, tenantID)
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

func (r *PostgresRepository) UpdateEnvironment(ctx context.Context, env Environment) error {
	connectorsJSON, mErr := marshalConnectors(env.Connectors)
	if mErr != nil {
		return mErr
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE environments
		SET name=$1, description=$2, egress_hosts=$3, connectors=$4::jsonb, updated_at=$5
		WHERE tenant_id=$6 AND id=$7`,
		env.Name, env.Description, env.EgressHosts, connectorsJSON, time.Now().UTC(), env.TenantID, env.ID,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrEnvironmentNameTaken
		}
		return fmt.Errorf("update environment: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PostgresRepository) DeleteEnvironment(ctx context.Context, tenantID, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM environments WHERE tenant_id=$1 AND id=$2`, tenantID, id)
	if err != nil {
		return fmt.Errorf("delete environment: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PostgresRepository) ListAgentTypesByEnvironment(ctx context.Context, tenantID, environmentID string) ([]AgentType, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, name, slug, description, system_prompt, framework, tool_scopes, grant_scopes, max_concurrent_runs_per_pod, state, model_id, created_at, updated_at, connectivity, connectors, environment_id
		FROM agent_types WHERE tenant_id = $1 AND environment_id = $2 ORDER BY created_at DESC`, tenantID, environmentID)
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
	err := s.Scan(&e.ID, &e.TenantID, &e.Name, &e.Description, &e.EgressHosts, &connectorsJSON, &e.CreatedAt, &e.UpdatedAt)
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
	if e.Connectors == nil {
		e.Connectors = []Connector{}
	}
	if e.EgressHosts == nil {
		e.EgressHosts = []string{}
	}
	return &e, nil
}
