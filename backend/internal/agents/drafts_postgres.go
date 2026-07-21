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

// ── Agent type drafts ─────────────────────────────────────────────────────────

func (r *PostgresRepository) CreateDraft(ctx context.Context, d AgentTypeDraft) error {
	if d.ID == "" {
		d.ID = uuid.NewString()
	}
	if len(d.Payload) == 0 {
		d.Payload = json.RawMessage(`{}`)
	}
	now := time.Now().UTC()
	_, err := r.pool.Exec(ctx, `
		INSERT INTO agent_type_drafts
			(id, tenant_id, created_by, payload, created_at, updated_at)
		VALUES ($1,$2,$3,$4::jsonb,$5,$6)`,
		d.ID, d.TenantID, d.CreatedBy, string(d.Payload), now, now,
	)
	if err != nil {
		return fmt.Errorf("insert agent_type_draft: %w", err)
	}
	return nil
}

func (r *PostgresRepository) GetDraft(ctx context.Context, id string) (*AgentTypeDraft, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, created_by, payload, created_at, updated_at
		FROM agent_type_drafts WHERE id = $1`, id)
	return scanDraft(row)
}

func (r *PostgresRepository) ListDrafts(ctx context.Context, tenantID, createdBy string) ([]AgentTypeDraft, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, created_by, payload, created_at, updated_at
		FROM agent_type_drafts
		WHERE tenant_id = $1 AND created_by = $2
		ORDER BY updated_at DESC`, tenantID, createdBy)
	if err != nil {
		return nil, fmt.Errorf("query agent_type_drafts: %w", err)
	}
	defer rows.Close()

	var out []AgentTypeDraft
	for rows.Next() {
		d, err := scanDraft(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) UpdateDraft(ctx context.Context, id string, payload json.RawMessage) error {
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE agent_type_drafts SET payload=$1::jsonb, updated_at=$2
		WHERE id=$3`,
		string(payload), time.Now().UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("update agent_type_draft: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrDraftNotFound
	}
	return nil
}

func (r *PostgresRepository) DeleteDraft(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM agent_type_drafts WHERE id=$1`, id)
	if err != nil {
		return fmt.Errorf("delete agent_type_draft: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrDraftNotFound
	}
	return nil
}

func (r *PostgresRepository) DeleteDraftsOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM agent_type_drafts WHERE updated_at < $1`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("delete stale agent_type_drafts: %w", err)
	}
	return tag.RowsAffected(), nil
}

func scanDraft(s scanner) (*AgentTypeDraft, error) {
	var d AgentTypeDraft
	var payload []byte
	err := s.Scan(&d.ID, &d.TenantID, &d.CreatedBy, &payload, &d.CreatedAt, &d.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrDraftNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan agent_type_draft: %w", err)
	}
	d.Payload = json.RawMessage(payload)
	if len(d.Payload) == 0 {
		d.Payload = json.RawMessage(`{}`)
	}
	return &d, nil
}
