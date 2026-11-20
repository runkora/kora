package agents

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/runkora/kora/backend/internal/platform/declarative"
	"github.com/runkora/kora/backend/internal/platform/events"
	"github.com/runkora/kora/backend/internal/platform/storage"
)

const graderColumns = `g.id, g.tenant_id, g.name, g.agent_type_id, a.slug, g.description, g.min_pass_rate, g.current_version, g.resource_uid, g.resource_generation, COALESCE(g.repository, '')`

// GetGraderByName returns a tenant's Grader by name.
func (r *PostgresRepository) GetGraderByName(ctx context.Context, tenantID, name string) (*Grader, error) {
	var g Grader
	var minPassRate *int16
	err := r.pool.QueryRow(ctx, `SELECT `+graderColumns+` FROM graders g JOIN agent_types a ON a.id = g.agent_type_id
		WHERE g.tenant_id = $1 AND g.name = $2`, tenantID, name).Scan(
		&g.ID, &g.TenantID, &g.Name, &g.AgentTypeID, &g.AgentSlug, &g.Description, &minPassRate, &g.CurrentVersion, &g.ResourceUID, &g.ResourceGeneration, &g.Repository)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get grader: %w", err)
	}
	if minPassRate != nil {
		g.MinPassRate = new(int(*minPassRate))
	}
	return &g, nil
}

// PutGrader stores the version spec once and upserts the Grader by tenant and name. A recreated
// resource takes over the row and keeps its id. It returns the stored Grader and records a created
// or updated event in the same transaction.
func (r *PostgresRepository) PutGrader(ctx context.Context, g Grader, versionSpec []byte) (*Grader, error) {
	var out *Grader
	err := storage.WithTransaction(ctx, r.pool, func(ctx context.Context, _ pgx.Tx) error {
		before, err := r.GetGraderByName(ctx, g.TenantID, g.Name)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		if _, err = r.pool.Exec(ctx, `INSERT INTO grader_versions (version, spec) VALUES ($1, $2::jsonb) ON CONFLICT (version) DO NOTHING`,
			g.CurrentVersion, versionSpec); err != nil {
			return fmt.Errorf("insert grader version: %w", err)
		}
		if _, err = r.pool.Exec(ctx, `
			INSERT INTO graders (id, tenant_id, name, agent_type_id, description, min_pass_rate, current_version, resource_uid, resource_generation, repository)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NULLIF($10, ''))
			ON CONFLICT (tenant_id, name) DO UPDATE SET
				agent_type_id = EXCLUDED.agent_type_id, description = EXCLUDED.description, min_pass_rate = EXCLUDED.min_pass_rate,
				current_version = EXCLUDED.current_version, resource_uid = EXCLUDED.resource_uid,
				resource_generation = EXCLUDED.resource_generation, repository = EXCLUDED.repository, updated_at = now()`,
			uuid.NewString(), g.TenantID, g.Name, g.AgentTypeID, g.Description, g.MinPassRate, g.CurrentVersion, g.ResourceUID, g.ResourceGeneration, g.Repository); err != nil {
			return fmt.Errorf("upsert grader: %w", err)
		}
		if out, err = r.GetGraderByName(ctx, g.TenantID, g.Name); err != nil {
			return err
		}
		if before != nil && before.CurrentVersion != out.CurrentVersion {
			if err := r.regradeOnNewVersion(ctx, out.ID, before.CurrentVersion, out.CurrentVersion); err != nil {
				return err
			}
		}
		if before == nil {
			return events.Publish(ctx, r.pool, graderEvent(events.GraderCreated, *out, nil))
		}
		changed := events.Changed(newGraderEntity(*before), newGraderEntity(*out))
		if len(changed) == 0 {
			return nil
		}
		return events.Publish(ctx, r.pool, graderEvent(events.GraderUpdated, *out, changed))
	})
	return out, err
}

// regradeOnNewVersion queues the regrade of a Grader that moved from one version to another.
func (r *PostgresRepository) regradeOnNewVersion(ctx context.Context, graderID, from, to string) error {
	fromSpec, err := r.graderVersionSpec(ctx, from)
	if err != nil {
		return err
	}
	toSpec, err := r.graderVersionSpec(ctx, to)
	if err != nil {
		return err
	}
	return r.enqueueRegrade(ctx, graderID, from, to, fromSpec, toSpec)
}

// DeleteGrader deletes a tenant's Grader and records a deleted event. Its versions stay. A
// missing Grader is no error and records nothing.
func (r *PostgresRepository) DeleteGrader(ctx context.Context, tenantID, name string) error {
	return storage.WithTransaction(ctx, r.pool, func(ctx context.Context, _ pgx.Tx) error {
		before, err := r.GetGraderByName(ctx, tenantID, name)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := r.pool.Exec(ctx, `DELETE FROM graders WHERE id = $1`, before.ID); err != nil {
			return fmt.Errorf("delete grader: %w", err)
		}
		return events.Publish(ctx, r.pool, graderEvent(events.GraderDeleted, *before, nil))
	})
}

// ListDeclaredGraders returns the name and resource UID of a tenant's Graders.
func (r *PostgresRepository) ListDeclaredGraders(ctx context.Context, tenantID string) ([]declarative.Declared, error) {
	return r.listDeclared(ctx, `SELECT name, resource_uid FROM graders WHERE tenant_id = $1 ORDER BY name`, tenantID)
}
