package triggers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

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

const sourceColumns = `id, tenant_id, name, provider, principal_id, grant_id, secret_ciphertext, config, enabled, created_at, updated_at, rejection_count, last_rejection_at, last_rejection_reason`

func scanSource(row pgx.Row) (*WebhookSource, error) {
	var s WebhookSource
	var configJSON []byte
	var lastRejectionReason *string
	if err := row.Scan(
		&s.ID, &s.TenantID, &s.Name, &s.Provider, &s.PrincipalID, &s.GrantID,
		&s.SecretCiphertext, &configJSON, &s.Enabled, &s.CreatedAt, &s.UpdatedAt,
		&s.RejectionCount, &s.LastRejectionAt, &lastRejectionReason,
	); err != nil {
		return nil, err
	}
	if lastRejectionReason != nil {
		s.LastRejectionReason = *lastRejectionReason
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

// triggerColumns lists all columns for a trigger row.
// kind, kind_config, principal_id, and grant_id_svc are the new columns added
// in migration 20260620100000_extend_trigger_kinds.sql.
const triggerColumns = `id, tenant_id, kind, target_kind, source_id, name, agent_type_id, agent_slug, event_type,
	match_condition, prompt_template, kind_config, principal_id, grant_id, grant_id_svc, enabled, created_at, updated_at,
	max_parallel, overflow, max_queue_depth, timeout_seconds, dedupe_enabled, autopause_enabled,
	consecutive_failures, auto_paused_at`

func scanTrigger(row pgx.Row) (*Trigger, error) {
	var t Trigger
	var condJSON, kindConfigJSON []byte
	var sourceID, principalID *string
	if err := row.Scan(
		&t.ID, &t.TenantID, &t.Kind, &t.TargetKind, &sourceID, &t.Name, &t.AgentTypeID, &t.AgentSlug,
		&t.EventType, &condJSON, &t.PromptTemplate, &kindConfigJSON,
		&principalID, &t.GrantID, &t.GrantIDSvc, &t.Enabled, &t.CreatedAt, &t.UpdatedAt,
		&t.MaxParallel, &t.Overflow, &t.MaxQueueDepth, &t.TimeoutSeconds, &t.DedupeEnabled,
		&t.AutopauseEnabled, &t.ConsecutiveFailures, &t.AutoPausedAt,
	); err != nil {
		return nil, err
	}
	if sourceID != nil {
		t.SourceID = *sourceID
	}
	if principalID != nil {
		t.PrincipalID = *principalID
	}
	if len(condJSON) > 0 {
		if err := json.Unmarshal(condJSON, &t.MatchCondition); err != nil {
			return nil, fmt.Errorf("unmarshal match condition: %w", err)
		}
	}
	if len(kindConfigJSON) > 0 && string(kindConfigJSON) != "{}" {
		// kind_config is polymorphic: its shape follows the trigger's kind.
		switch t.Kind {
		case KindScheduled:
			var sc ScheduleConfig
			if err := json.Unmarshal(kindConfigJSON, &sc); err != nil {
				return nil, fmt.Errorf("unmarshal schedule config: %w", err)
			}
			if sc.CronExpression != "" {
				t.ScheduleConfig = &sc
			}
		case KindManual:
			var mc ManualConfig
			if err := json.Unmarshal(kindConfigJSON, &mc); err != nil {
				return nil, fmt.Errorf("unmarshal manual config: %w", err)
			}
			if len(mc.Inputs) > 0 {
				t.ManualConfig = &mc
			}
		}
	}
	return &t, nil
}

func (r *postgresRepository) CreateTrigger(ctx context.Context, t *Trigger) error {
	condJSON, err := json.Marshal(t.MatchCondition)
	if err != nil {
		return fmt.Errorf("marshal match condition: %w", err)
	}
	kindConfigJSON, err := marshalKindConfig(t)
	if err != nil {
		return err
	}

	var sourceID *string
	if t.SourceID != "" {
		sourceID = &t.SourceID
	}

	_, err = r.pool.Exec(ctx, `
		INSERT INTO triggers
			(id, tenant_id, kind, target_kind, source_id, name, agent_type_id, agent_slug, event_type,
			 match_condition, prompt_template, kind_config, principal_id, grant_id, grant_id_svc, enabled,
			 overflow, max_parallel, max_queue_depth, timeout_seconds)
		VALUES
			($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
	`, t.ID, t.TenantID, t.Kind, defaultTargetKind(t.TargetKind), sourceID, t.Name, t.AgentTypeID, t.AgentSlug, t.EventType,
		condJSON, t.PromptTemplate, kindConfigJSON, nullableStr(t.PrincipalID), t.GrantID, t.GrantIDSvc, t.Enabled,
		defaultOverflow(t), defaultInt16(t.MaxParallel, 4), defaultInt16(t.MaxQueueDepth, 32), defaultInt16(t.TimeoutSeconds, 30))
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
	rows, err := r.pool.Query(ctx, `SELECT `+triggerColumns+` FROM triggers WHERE source_id = $1 AND kind = 'webhook' AND enabled = true`, sourceID)
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

func (r *postgresRepository) ListEnabledScheduledTriggers(ctx context.Context) ([]Trigger, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+triggerColumns+` FROM triggers WHERE kind = 'scheduled' AND enabled = true`)
	if err != nil {
		return nil, fmt.Errorf("list scheduled triggers: %w", err)
	}
	defer rows.Close()

	var out []Trigger
	for rows.Next() {
		t, err := scanTrigger(rows)
		if err != nil {
			return nil, fmt.Errorf("scan scheduled trigger: %w", err)
		}
		out = append(out, *t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate scheduled triggers: %w", err)
	}
	return out, nil
}

func (r *postgresRepository) UpdateTrigger(ctx context.Context, t *Trigger) error {
	condJSON, err := json.Marshal(t.MatchCondition)
	if err != nil {
		return fmt.Errorf("marshal match condition: %w", err)
	}
	kindConfigJSON, err := marshalKindConfig(t)
	if err != nil {
		return err
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE triggers
		SET    name = $1, event_type = $2, match_condition = $3, prompt_template = $4,
		       kind_config = $5, enabled = $6, updated_at = now()
		WHERE  id = $7 AND tenant_id = $8
	`, t.Name, t.EventType, condJSON, t.PromptTemplate, kindConfigJSON, t.Enabled, t.ID, t.TenantID)
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
	// Two dedup strategies depending on kind:
	//   webhook:   ON CONFLICT (source_id, provider_delivery_id) WHERE provider_delivery_id <> ''
	//   scheduled: ON CONFLICT (trigger_id, scheduled_at)        WHERE scheduled_at IS NOT NULL
	// Both partial unique indexes use DO NOTHING; RowsAffected == 0 signals duplicate.
	var sourceID *string
	if d.SourceID != "" {
		sourceID = &d.SourceID
	}
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO trigger_deliveries
			(id, tenant_id, source_id, trigger_id, kind, provider_delivery_id, event_type,
			 status, run_id, error, payload_excerpt, fired_by, scheduled_at,
			 payload, payload_bytes, reason, redelivery_of)
		VALUES
			($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		ON CONFLICT (source_id, provider_delivery_id) WHERE provider_delivery_id <> '' DO NOTHING
	`, d.ID, d.TenantID, sourceID, d.TriggerID, d.Kind, d.ProviderDeliveryID, d.EventType,
		d.Status, d.RunID, d.Error, d.PayloadExcerpt, nullableStr(d.FiredBy), d.ScheduledAt,
		d.Payload, nullableInt(d.PayloadBytes), d.Reason, d.RedeliveryOf)
	if err != nil {
		return false, fmt.Errorf("insert delivery: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// InsertScheduledDelivery inserts a delivery for a scheduled trigger tick,
// using the scheduled_at dedup index to prevent double-fire on pod restart.
func (r *postgresRepository) InsertScheduledDelivery(ctx context.Context, d *Delivery) (bool, error) {
	var sourceID *string
	if d.SourceID != "" {
		sourceID = &d.SourceID
	}
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO trigger_deliveries
			(id, tenant_id, source_id, trigger_id, kind, provider_delivery_id, event_type,
			 status, run_id, error, payload_excerpt, fired_by, scheduled_at,
			 payload, payload_bytes, reason, redelivery_of)
		VALUES
			($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		ON CONFLICT (trigger_id, scheduled_at) WHERE scheduled_at IS NOT NULL DO NOTHING
	`, d.ID, d.TenantID, sourceID, d.TriggerID, d.Kind, d.ProviderDeliveryID, d.EventType,
		d.Status, d.RunID, d.Error, d.PayloadExcerpt, nullableStr(d.FiredBy), d.ScheduledAt,
		d.Payload, nullableInt(d.PayloadBytes), d.Reason, d.RedeliveryOf)
	if err != nil {
		return false, fmt.Errorf("insert scheduled delivery: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func (r *postgresRepository) UpdateDeliveryOutcome(ctx context.Context, id, status string, triggerID, runID, errMsg *string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE trigger_deliveries
		SET    status = $1, trigger_id = COALESCE($2, trigger_id), run_id = $3, error = $4
		WHERE  id = $5
	`, status, triggerID, runID, errMsg, id)
	if err != nil {
		return fmt.Errorf("update delivery outcome: %w", err)
	}
	return nil
}

func (r *postgresRepository) ListDeliveries(ctx context.Context, tenantID string, filter DeliveryFilter) ([]Delivery, error) {
	query := `
		SELECT id, tenant_id, source_id, trigger_id, kind, provider_delivery_id, event_type,
		       status, run_id, error, payload_excerpt, fired_by, scheduled_at, received_at,
		       payload, payload_bytes, payload_purged, reason, redelivery_of
		FROM   trigger_deliveries
		WHERE  tenant_id = $1`
	args := []any{tenantID}
	idx := 2
	if filter.SourceID != "" {
		query += fmt.Sprintf(` AND source_id = $%d`, idx)
		args = append(args, filter.SourceID)
		idx++
	}
	if filter.TriggerID != "" {
		query += fmt.Sprintf(` AND trigger_id = $%d`, idx)
		args = append(args, filter.TriggerID)
		idx++
	}
	if filter.Kind != "" {
		query += fmt.Sprintf(` AND kind = $%d`, idx)
		args = append(args, filter.Kind)
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
		var sourceID *string
		var firedBy *string
		var payloadBytes *int
		if err := rows.Scan(
			&d.ID, &d.TenantID, &sourceID, &d.TriggerID, &d.Kind, &d.ProviderDeliveryID, &d.EventType,
			&d.Status, &d.RunID, &d.Error, &d.PayloadExcerpt, &firedBy, &d.ScheduledAt, &d.ReceivedAt,
			&d.Payload, &payloadBytes, &d.PayloadPurged, &d.Reason, &d.RedeliveryOf,
		); err != nil {
			return nil, fmt.Errorf("scan delivery: %w", err)
		}
		if sourceID != nil {
			d.SourceID = *sourceID
		}
		if firedBy != nil {
			d.FiredBy = *firedBy
		}
		if payloadBytes != nil {
			d.PayloadBytes = *payloadBytes
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

// ─── helpers ──────────────────────────────────────────────────────────────────

func marshalKindConfig(t *Trigger) ([]byte, error) {
	switch {
	case t.Kind == KindScheduled && t.ScheduleConfig != nil:
		b, err := json.Marshal(t.ScheduleConfig)
		if err != nil {
			return nil, fmt.Errorf("marshal schedule config: %w", err)
		}
		return b, nil
	case t.Kind == KindManual && t.ManualConfig != nil:
		b, err := json.Marshal(t.ManualConfig)
		if err != nil {
			return nil, fmt.Errorf("marshal manual config: %w", err)
		}
		return b, nil
	}
	return []byte("{}"), nil
}

// defaultOverflow gives each kind the overflow behavior that suits it when the
// caller did not choose one. A webhook waits (the sender was acknowledged and
// dropping its event loses work), a schedule replaces (the newest tick is the
// state worth acting on), a manual fire refuses (a person is waiting).
func defaultOverflow(t *Trigger) string {
	if t.Overflow != "" {
		return t.Overflow
	}
	return DefaultOverflowForKind(t.Kind)
}

// defaultInt16 returns v, or fallback when v is unset.
func defaultInt16(v, fallback int16) int16 {
	if v <= 0 {
		return fallback
	}
	return v
}

// nullableInt maps a zero size to SQL NULL, so "no payload captured" is
// distinguishable from "an empty payload".
// defaultTargetKind fills in the only target kind that exists today. Triggers
// are the front door for starting work; when the workflow engine lands, a
// trigger can name it here without changing anything else.
func defaultTargetKind(k string) string {
	if k == "" {
		return TargetKindAgent
	}
	return k
}

func nullableInt(n int) *int {
	if n == 0 {
		return nil
	}
	return &n
}

func nullableStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// scheduledAtMinute truncates t to minute precision for the cron dedup index.
func scheduledAtMinute(t time.Time) time.Time {
	return t.UTC().Truncate(time.Minute)
}

// DeriveDeliveryStatus recomputes a delivery's status from its per-trigger
// outcomes rather than writing it inline during the fan-out.
//
// Writing the summary from the first successful dispatch, as the original
// implementation did, cannot express "started two runs, denied by a third": the
// first success wins and the denial disappears from the delivery's own view.
// Recomputing after the fan-out completes keeps the summary honest and leaves
// the detail on trigger_delivery_runs (ADR Decision 3).
//
// Deliveries that never reached matching (duplicate, missed) have no runs and
// are left untouched: their status describes the arrival, not a fan-out.
func (r *postgresRepository) DeriveDeliveryStatus(ctx context.Context, deliveryID string) error {
	// 'filtered' is deliberately not a failure. A trigger that declined an event
	// via its own match_condition worked exactly as configured, so a delivery
	// whose only outcomes are filtered is 'skipped' — nothing was ever going to
	// run — not 'failed'.
	_, err := r.pool.Exec(ctx, `
		UPDATE trigger_deliveries d
		SET status = CASE
				WHEN EXISTS (
					SELECT 1 FROM trigger_delivery_runs
					WHERE delivery_id = d.id AND status = 'dispatched'
				) THEN 'dispatched'
				WHEN EXISTS (
					SELECT 1 FROM trigger_delivery_runs
					WHERE delivery_id = d.id
					  AND status IN ('failed', 'denied', 'dropped')
				) THEN 'failed'
				ELSE 'skipped'
			END
		WHERE d.id = $1
		  AND d.status NOT IN ('duplicate', 'missed')
	`, deliveryID)
	if err != nil {
		return fmt.Errorf("derive delivery status: %w", err)
	}
	return nil
}

// RecordSourceRejection counts a request that failed authentication against its
// source, keeping the most recent reason.
//
// No delivery row is created. The request is unauthenticated, so one row per
// request would be an attacker-controlled write; one UPDATE to a row the tenant
// already owns is bounded no matter what arrives (ADR Decision 4).
func (r *postgresRepository) RecordSourceRejection(ctx context.Context, sourceID, reason string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE webhook_sources
		SET rejection_count       = rejection_count + 1,
		    last_rejection_at     = now(),
		    last_rejection_reason = $2
		WHERE id = $1
	`, sourceID, reason)
	if err != nil {
		return fmt.Errorf("record source rejection: %w", err)
	}
	return nil
}

// DueScheduledMinutesWithoutDelivery lists the minutes in [since, now) that have
// no delivery for this trigger.
//
// It generates the candidate minutes rather than reading them, because the
// interesting rows are the ones that do not exist: a fire missed during an
// outage left nothing behind to query. The caller filters these against the
// trigger's cron expression, so this only has to bound the search.
func (r *postgresRepository) DueScheduledMinutesWithoutDelivery(ctx context.Context, triggerID string, since time.Time) ([]time.Time, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT m
		FROM   generate_series(
		           date_trunc('minute', $2::timestamptz),
		           date_trunc('minute', now()) - interval '1 minute',
		           interval '1 minute'
		       ) AS m
		WHERE NOT EXISTS (
		    SELECT 1 FROM trigger_deliveries d
		    WHERE d.trigger_id = $1 AND d.scheduled_at = m
		)
		ORDER BY m
	`, triggerID, since)
	if err != nil {
		return nil, fmt.Errorf("find missed scheduled minutes: %w", err)
	}
	defer rows.Close()

	var out []time.Time
	for rows.Next() {
		var m time.Time
		if err := rows.Scan(&m); err != nil {
			return nil, fmt.Errorf("scan missed minute: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate missed minutes: %w", err)
	}
	return out, nil
}

// ─── Admission control ───────────────────────────────────────────────────────

func (r *postgresRepository) EnqueuePending(ctx context.Context, p *PendingDelivery) error {
	fieldsJSON, err := json.Marshal(p.Fields)
	if err != nil {
		return fmt.Errorf("marshal pending fields: %w", err)
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO trigger_pending_deliveries (id, tenant_id, trigger_id, delivery_id, fields)
		VALUES ($1, $2, $3, $4, $5)
	`, p.ID, p.TenantID, p.TriggerID, p.DeliveryID, fieldsJSON)
	if err != nil {
		return fmt.Errorf("enqueue pending delivery: %w", err)
	}
	return nil
}

// ClaimPending takes work for triggers that are under their ceiling.
//
// SKIP LOCKED is what makes this safe to run on every replica: each worker takes
// rows nobody else holds rather than blocking on them, so adding replicas adds
// throughput instead of contention.
//
// The in-flight count is computed inside the same statement as the claim.
// Reading it separately would leave a window where two workers each see a free
// slot and both take it, which is exactly the over-admission the ceiling exists
// to prevent.
func (r *postgresRepository) ClaimPending(ctx context.Context, limit int) ([]PendingDelivery, error) {
	rows, err := r.pool.Query(ctx, `
		WITH in_flight AS (
		    SELECT dr.trigger_id, count(*) AS n
		    FROM   trigger_delivery_runs dr
		    WHERE  dr.status = 'dispatched'
		      AND  dr.created_at > now() - interval '1 hour'
		    GROUP BY dr.trigger_id
		),
		claimable AS (
		    SELECT p.id
		    FROM   trigger_pending_deliveries p
		    JOIN   triggers t ON t.id = p.trigger_id
		    LEFT   JOIN in_flight f ON f.trigger_id = p.trigger_id
		    WHERE  p.claimed_at IS NULL
		      AND  t.enabled
		      AND  COALESCE(f.n, 0) < t.max_parallel
		    ORDER  BY p.enqueued_at
		    LIMIT  $1
		    FOR UPDATE OF p SKIP LOCKED
		)
		UPDATE trigger_pending_deliveries p
		SET    claimed_at = now()
		FROM   claimable c
		WHERE  p.id = c.id
		RETURNING p.id, p.tenant_id, p.trigger_id, p.delivery_id, p.fields, p.enqueued_at
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("claim pending deliveries: %w", err)
	}
	defer rows.Close()
	return scanPending(rows)
}

func (r *postgresRepository) ReleasePending(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM trigger_pending_deliveries WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("release pending delivery: %w", err)
	}
	return nil
}

// InFlightCount counts runs this trigger started that have not settled.
//
// Bounded by an hour so a run whose terminal outcome was never recorded (a lost
// pod, a dropped callback) cannot hold a slot forever. A stuck slot would
// silently throttle a healthy trigger to nothing, which is worse than briefly
// over-admitting.
func (r *postgresRepository) InFlightCount(ctx context.Context, triggerID string) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM   trigger_delivery_runs
		WHERE  trigger_id = $1
		  AND  status = 'dispatched'
		  AND  created_at > now() - interval '1 hour'
	`, triggerID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count in-flight runs: %w", err)
	}
	return n, nil
}

func (r *postgresRepository) SupersedePending(ctx context.Context, triggerID string) ([]PendingDelivery, error) {
	rows, err := r.pool.Query(ctx, `
		DELETE FROM trigger_pending_deliveries
		WHERE  trigger_id = $1 AND claimed_at IS NULL
		RETURNING id, tenant_id, trigger_id, delivery_id, fields, enqueued_at
	`, triggerID)
	if err != nil {
		return nil, fmt.Errorf("supersede pending deliveries: %w", err)
	}
	defer rows.Close()
	return scanPending(rows)
}

// TrimPending evicts the oldest waiting rows beyond depth.
//
// Oldest rather than newest: when a trigger is behind, the freshest events are
// the ones still worth acting on.
func (r *postgresRepository) TrimPending(ctx context.Context, triggerID string, depth int) ([]PendingDelivery, error) {
	rows, err := r.pool.Query(ctx, `
		DELETE FROM trigger_pending_deliveries
		WHERE  id IN (
		    SELECT id FROM trigger_pending_deliveries
		    WHERE  trigger_id = $1 AND claimed_at IS NULL
		    ORDER  BY enqueued_at DESC
		    OFFSET $2
		)
		RETURNING id, tenant_id, trigger_id, delivery_id, fields, enqueued_at
	`, triggerID, depth)
	if err != nil {
		return nil, fmt.Errorf("trim pending deliveries: %w", err)
	}
	defer rows.Close()
	return scanPending(rows)
}

func (r *postgresRepository) DrainPending(ctx context.Context, triggerID string) ([]PendingDelivery, error) {
	rows, err := r.pool.Query(ctx, `
		DELETE FROM trigger_pending_deliveries
		WHERE  trigger_id = $1
		RETURNING id, tenant_id, trigger_id, delivery_id, fields, enqueued_at
	`, triggerID)
	if err != nil {
		return nil, fmt.Errorf("drain pending deliveries: %w", err)
	}
	defer rows.Close()
	return scanPending(rows)
}

func scanPending(rows pgx.Rows) ([]PendingDelivery, error) {
	var out []PendingDelivery
	for rows.Next() {
		var p PendingDelivery
		var fieldsJSON []byte
		if err := rows.Scan(&p.ID, &p.TenantID, &p.TriggerID, &p.DeliveryID, &fieldsJSON, &p.EnqueuedAt); err != nil {
			return nil, fmt.Errorf("scan pending delivery: %w", err)
		}
		if len(fieldsJSON) > 0 {
			if err := json.Unmarshal(fieldsJSON, &p.Fields); err != nil {
				return nil, fmt.Errorf("unmarshal pending fields: %w", err)
			}
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending deliveries: %w", err)
	}
	return out, nil
}

// ─── Delivery safety ─────────────────────────────────────────────────────────

func (r *postgresRepository) BumpConsecutiveFailures(ctx context.Context, triggerID string) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `
		UPDATE triggers
		SET    consecutive_failures = consecutive_failures + 1
		WHERE  id = $1
		RETURNING consecutive_failures
	`, triggerID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("bump consecutive failures: %w", err)
	}
	return n, nil
}

func (r *postgresRepository) ResetConsecutiveFailures(ctx context.Context, triggerID string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE triggers SET consecutive_failures = 0 WHERE id = $1 AND consecutive_failures <> 0
	`, triggerID)
	if err != nil {
		return fmt.Errorf("reset consecutive failures: %w", err)
	}
	return nil
}

func (r *postgresRepository) AutoPause(ctx context.Context, triggerID string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE triggers SET enabled = false, auto_paused_at = now() WHERE id = $1
	`, triggerID)
	if err != nil {
		return fmt.Errorf("auto-pause trigger: %w", err)
	}
	return nil
}

// PurgePayloads nulls payload bodies past each tenant's retention window.
//
// Only the body goes. Every other column survives, so a purged delivery still
// answers "did this fire, and what happened" for an auditor — the retention
// promise is about third-party payload data, not about erasing history.
func (r *postgresRepository) PurgePayloads(ctx context.Context, defaultDays int) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE trigger_deliveries d
		SET    payload = NULL, payload_purged = TRUE
		FROM  (SELECT $1::int AS days) def
		LEFT   JOIN trigger_retention_settings s ON TRUE
		WHERE  d.payload IS NOT NULL
		  AND  d.received_at < now() - make_interval(days => COALESCE(
		           (SELECT payload_days FROM trigger_retention_settings WHERE tenant_id = d.tenant_id),
		           def.days))
	`, defaultDays)
	if err != nil {
		return 0, fmt.Errorf("purge payloads: %w", err)
	}
	return tag.RowsAffected(), nil
}
