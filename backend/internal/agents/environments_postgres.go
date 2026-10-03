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

const environmentColumns = `id, tenant_id, name, description, sandbox_image, cpu, memory, disk, egress_hosts, connectors, created_at, updated_at`

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
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11,$12)`,
		env.ID, env.TenantID, env.Name, env.Description, env.SandboxImage,
		env.Size.CPU, env.Size.Memory, env.Size.Disk, env.EgressHosts, connectorsJSON, now, now,
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

func (r *PostgresRepository) UpdateEnvironment(ctx context.Context, env Environment) error {
	connectorsJSON, err := marshalConnectors(env.Connectors)
	if err != nil {
		return err
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE environments
		SET name=$1, description=$2, sandbox_image=$3, cpu=$4, memory=$5, disk=$6, egress_hosts=$7, connectors=$8::jsonb, updated_at=$9
		WHERE tenant_id=$10 AND id=$11`,
		env.Name, env.Description, env.SandboxImage, env.Size.CPU, env.Size.Memory, env.Size.Disk,
		env.EgressHosts, connectorsJSON, time.Now().UTC(), env.TenantID, env.ID,
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
		WHERE tenant_id = $1 AND id IN (SELECT agent_type_id FROM agent_environments WHERE environment_id = $2)
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
	err := s.Scan(&e.ID, &e.TenantID, &e.Name, &e.Description, &e.SandboxImage,
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
	if e.Connectors == nil {
		e.Connectors = []Connector{}
	}
	if e.EgressHosts == nil {
		e.EgressHosts = []string{}
	}
	// A row written before the image was stored explicitly runs the base image.
	e.SandboxImage = imageOrBase(e.SandboxImage)
	return &e, nil
}
