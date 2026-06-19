package triggers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type postgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository creates a Repository backed by PostgreSQL.
func NewPostgresRepository(pool *pgxpool.Pool) Repository {
	return &postgresRepository{pool: pool}
}

// ─── Sources ──────────────────────────────────────────────────────────────────

func (r *postgresRepository) CreateSource(ctx context.Context, s *WebhookSource) error {
	configJSON, err := json.Marshal(s.Config)
	if err != nil {
		return fmt.Errorf("marshal source config: %w", err)
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO webhook_sources
			(id, tenant_id, name, provider, principal_id, grant_id, secret_ciphertext, config, enabled)
		VALUES
			($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, s.ID, s.TenantID, s.Name, s.Provider, s.PrincipalID, s.GrantID, s.SecretCiphertext, configJSON, s.Enabled)
	if err != nil {
		return fmt.Errorf("create webhook source: %w", err)
	}
	return nil
}

const sourceColumns = `id, tenant_id, name, provider, principal_id, grant_id, secret_ciphertext, config, enabled, created_at, updated_at`

func scanSource(row pgx.Row) (*WebhookSource, error) {
	var s WebhookSource
	var configJSON []byte
	if err := row.Scan(
		&s.ID, &s.TenantID, &s.Name, &s.Provider, &s.PrincipalID, &s.GrantID,
		&s.SecretCiphertext, &configJSON, &s.Enabled, &s.CreatedAt, &s.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if len(configJSON) > 0 {
		if err := json.Unmarshal(configJSON, &s.Config); err != nil {
			return nil, fmt.Errorf("unmarshal source config: %w", err)
		}
	}
	return &s, nil
}

func (r *postgresRepository) GetSource(ctx context.Context, tenantID, id string) (*WebhookSource, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+sourceColumns+` FROM webhook_sources WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	s, err := scanSource(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSourceNotFound
		}
		return nil, fmt.Errorf("get webhook source: %w", err)
	}
	return s, nil
}

func (r *postgresRepository) GetSourceByID(ctx context.Context, id string) (*WebhookSource, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+sourceColumns+` FROM webhook_sources WHERE id = $1`, id)
	s, err := scanSource(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSourceNotFound
		}
		return nil, fmt.Errorf("get webhook source by id: %w", err)
	}
	return s, nil
}

func (r *postgresRepository) ListSources(ctx context.Context, tenantID string) ([]WebhookSource, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+sourceColumns+` FROM webhook_sources WHERE tenant_id = $1 ORDER BY created_at DESC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list webhook sources: %w", err)
	}
	defer rows.Close()

	var out []WebhookSource
	for rows.Next() {
		s, err := scanSource(rows)
		if err != nil {
			return nil, fmt.Errorf("scan webhook source: %w", err)
		}
		out = append(out, *s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate webhook sources: %w", err)
	}
	return out, nil
}

func (r *postgresRepository) UpdateSource(ctx context.Context, s *WebhookSource) error {
	configJSON, err := json.Marshal(s.Config)
	if err != nil {
		return fmt.Errorf("marshal source config: %w", err)
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE webhook_sources
		SET    name = $1, secret_ciphertext = $2, grant_id = $3, config = $4, enabled = $5, updated_at = now()
		WHERE  id = $6 AND tenant_id = $7
	`, s.Name, s.SecretCiphertext, s.GrantID, configJSON, s.Enabled, s.ID, s.TenantID)
	if err != nil {
		return fmt.Errorf("update webhook source: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrSourceNotFound
	}
	return nil
}

func (r *postgresRepository) DeleteSource(ctx context.Context, tenantID, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM webhook_sources WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	if err != nil {
		return fmt.Errorf("delete webhook source: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrSourceNotFound
	}
	return nil
}

// ─── Triggers ─────────────────────────────────────────────────────────────────

const triggerColumns = `id, tenant_id, source_id, name, agent_type_id, agent_slug, event_type, match_condition, prompt_template, grant_id, enabled, created_at, updated_at`

func scanTrigger(row pgx.Row) (*Trigger, error) {
	var t Trigger
	var condJSON []byte
	if err := row.Scan(
		&t.ID, &t.TenantID, &t.SourceID, &t.Name, &t.AgentTypeID, &t.AgentSlug,
		&t.EventType, &condJSON, &t.PromptTemplate, &t.GrantID, &t.Enabled, &t.CreatedAt, &t.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if len(condJSON) > 0 {
		if err := json.Unmarshal(condJSON, &t.MatchCondition); err != nil {
			return nil, fmt.Errorf("unmarshal match condition: %w", err)
		}
	}
	return &t, nil
}

func (r *postgresRepository) CreateTrigger(ctx context.Context, t *Trigger) error {
	condJSON, err := json.Marshal(t.MatchCondition)
	if err != nil {
		return fmt.Errorf("marshal match condition: %w", err)
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO triggers
			(id, tenant_id, source_id, name, agent_type_id, agent_slug, event_type, match_condition, prompt_template, grant_id, enabled)
		VALUES
			($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`, t.ID, t.TenantID, t.SourceID, t.Name, t.AgentTypeID, t.AgentSlug, t.EventType, condJSON, t.PromptTemplate, t.GrantID, t.Enabled)
	if err != nil {
		return fmt.Errorf("create trigger: %w", err)
	}
	return nil
}

func (r *postgresRepository) GetTrigger(ctx context.Context, tenantID, id string) (*Trigger, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+triggerColumns+` FROM triggers WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	t, err := scanTrigger(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTriggerNotFound
		}
		return nil, fmt.Errorf("get trigger: %w", err)
	}
	return t, nil
}

func (r *postgresRepository) ListTriggers(ctx context.Context, tenantID, agentTypeID string) ([]Trigger, error) {
	query := `SELECT ` + triggerColumns + ` FROM triggers WHERE tenant_id = $1`
	args := []any{tenantID}
	if agentTypeID != "" {
		query += ` AND agent_type_id = $2`
		args = append(args, agentTypeID)
	}
	query += ` ORDER BY created_at DESC`

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list triggers: %w", err)
	}
	defer rows.Close()

	var out []Trigger
	for rows.Next() {
		t, err := scanTrigger(rows)
		if err != nil {
			return nil, fmt.Errorf("scan trigger: %w", err)
		}
		out = append(out, *t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate triggers: %w", err)
	}
	return out, nil
}

func (r *postgresRepository) ListEnabledTriggersForSource(ctx context.Context, sourceID string) ([]Trigger, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+triggerColumns+` FROM triggers WHERE source_id = $1 AND enabled = true`, sourceID)
	if err != nil {
		return nil, fmt.Errorf("list triggers for source: %w", err)
	}
	defer rows.Close()

	var out []Trigger
	for rows.Next() {
		t, err := scanTrigger(rows)
		if err != nil {
			return nil, fmt.Errorf("scan trigger: %w", err)
		}
		out = append(out, *t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate triggers for source: %w", err)
	}
	return out, nil
}

func (r *postgresRepository) UpdateTrigger(ctx context.Context, t *Trigger) error {
	condJSON, err := json.Marshal(t.MatchCondition)
	if err != nil {
		return fmt.Errorf("marshal match condition: %w", err)
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE triggers
		SET    name = $1, event_type = $2, match_condition = $3, prompt_template = $4, enabled = $5, updated_at = now()
		WHERE  id = $6 AND tenant_id = $7
	`, t.Name, t.EventType, condJSON, t.PromptTemplate, t.Enabled, t.ID, t.TenantID)
	if err != nil {
		return fmt.Errorf("update trigger: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrTriggerNotFound
	}
	return nil
}

func (r *postgresRepository) DeleteTrigger(ctx context.Context, tenantID, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM triggers WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	if err != nil {
		return fmt.Errorf("delete trigger: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrTriggerNotFound
	}
	return nil
}

// ─── Deliveries ───────────────────────────────────────────────────────────────

func (r *postgresRepository) InsertDelivery(ctx context.Context, d *Delivery) (bool, error) {
	// ON CONFLICT targets the partial unique index on (source_id,
	// provider_delivery_id) — a redelivery of the same provider delivery id is a
	// no-op, signaled by RowsAffected == 0. Deliveries with an empty
	// provider_delivery_id are outside the partial index and never conflict.
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO trigger_deliveries
			(id, tenant_id, source_id, provider_delivery_id, event_type, status, trigger_id, run_id, error, payload_excerpt)
		VALUES
			($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (source_id, provider_delivery_id) WHERE provider_delivery_id <> '' DO NOTHING
	`, d.ID, d.TenantID, d.SourceID, d.ProviderDeliveryID, d.EventType, d.Status, d.TriggerID, d.RunID, d.Error, d.PayloadExcerpt)
	if err != nil {
		return false, fmt.Errorf("insert delivery: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func (r *postgresRepository) UpdateDeliveryOutcome(ctx context.Context, id, status string, triggerID, runID, errMsg *string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE trigger_deliveries
		SET    status = $1, trigger_id = $2, run_id = $3, error = $4
		WHERE  id = $5
	`, status, triggerID, runID, errMsg, id)
	if err != nil {
		return fmt.Errorf("update delivery outcome: %w", err)
	}
	return nil
}

func (r *postgresRepository) ListDeliveries(ctx context.Context, tenantID string, filter DeliveryFilter) ([]Delivery, error) {
	query := `
		SELECT id, tenant_id, source_id, provider_delivery_id, event_type, status, trigger_id, run_id, error, payload_excerpt, received_at
		FROM   trigger_deliveries
		WHERE  tenant_id = $1`
	args := []any{tenantID}
	if filter.SourceID != "" {
		query += ` AND source_id = $2`
		args = append(args, filter.SourceID)
	}
	query += ` ORDER BY received_at DESC`
	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query += fmt.Sprintf(` LIMIT %d`, limit)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list deliveries: %w", err)
	}
	defer rows.Close()

	var out []Delivery
	for rows.Next() {
		var d Delivery
		if err := rows.Scan(
			&d.ID, &d.TenantID, &d.SourceID, &d.ProviderDeliveryID, &d.EventType,
			&d.Status, &d.TriggerID, &d.RunID, &d.Error, &d.PayloadExcerpt, &d.ReceivedAt,
		); err != nil {
			return nil, fmt.Errorf("scan delivery: %w", err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate deliveries: %w", err)
	}
	return out, nil
}

func (r *postgresRepository) InsertDeliveryRun(ctx context.Context, dr *DeliveryRun) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO trigger_delivery_runs
			(id, tenant_id, delivery_id, trigger_id, run_id, status, error)
		VALUES
			($1, $2, $3, $4, $5, $6, $7)
	`, dr.ID, dr.TenantID, dr.DeliveryID, dr.TriggerID, dr.RunID, dr.Status, dr.Error)
	if err != nil {
		return fmt.Errorf("insert delivery run: %w", err)
	}
	return nil
}

func (r *postgresRepository) ListDeliveryRunsForDeliveries(ctx context.Context, tenantID string, deliveryIDs []string) (map[string][]DeliveryRun, error) {
	out := make(map[string][]DeliveryRun)
	if len(deliveryIDs) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, delivery_id, trigger_id, run_id, status, error, created_at
		FROM   trigger_delivery_runs
		WHERE  tenant_id = $1 AND delivery_id = ANY($2)
		ORDER BY created_at ASC
	`, tenantID, deliveryIDs)
	if err != nil {
		return nil, fmt.Errorf("list delivery runs: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var dr DeliveryRun
		if err := rows.Scan(
			&dr.ID, &dr.TenantID, &dr.DeliveryID, &dr.TriggerID,
			&dr.RunID, &dr.Status, &dr.Error, &dr.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan delivery run: %w", err)
		}
		out[dr.DeliveryID] = append(out[dr.DeliveryID], dr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate delivery runs: %w", err)
	}
	return out, nil
}
