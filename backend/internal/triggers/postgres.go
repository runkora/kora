package triggers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/clear-route/orbit/internal/platform/storage"
)

type postgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository creates a Repository backed by PostgreSQL.
func NewPostgresRepository(pool *pgxpool.Pool) Repository {
	return &postgresRepository{pool: pool}
}

// getOne runs a single-row query, mapping pgx.ErrNoRows to notFound and
// wrapping anything else as "get {what}". One implementation of the
// QueryRow→scan→sentinel dance the getters otherwise each hand-copy.
func getOne[T any](ctx context.Context, pool *pgxpool.Pool, what string, notFound error, scan func(pgx.Row) (*T, error), sql string, args ...any) (*T, error) {
	v, err := scan(pool.QueryRow(ctx, sql, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, notFound
		}
		return nil, fmt.Errorf("get %s: %w", what, err)
	}
	return v, nil
}

// queryList runs a query and scans one T per row, closing rows and checking
// rows.Err. One implementation of the Query→Next→Scan→Err loop the list
// queries otherwise each hand-copy; `what` names the rows once for all three
// wraps. The SQL stays at each call site — this is a scan helper, not a
// query builder.
func queryList[T any](ctx context.Context, pool *pgxpool.Pool, what string, scan func(pgx.Row) (*T, error), sql string, args ...any) ([]T, error) {
	rows, err := pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", what, err)
	}
	defer rows.Close()

	var out []T
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("scan %s: %w", what, err)
		}
		out = append(out, *v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s: %w", what, err)
	}
	return out, nil
}

// deref returns the value a nullable column scanned into, or the zero value
// for NULL. The read-side mirror of storage.NullIfZero: one spelling for the
// hand-rolled `if p != nil` blocks the scanners each carried.
func deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}

// ─── Sources ──────────────────────────────────────────────────────────────────

func (r *postgresRepository) CreateSource(ctx context.Context, s *WebhookSource) error {
	configJSON, err := json.Marshal(s.Config)
	if err != nil {
		return fmt.Errorf("marshal source config: %w", err)
	}
	// secret_rotated_at is stamped at creation: the secret a source starts with
	// was issued now, and dating it is what lets a later replay tell whether the
	// payload it wants to re-run predates the secret currently in force.
	_, err = r.pool.Exec(ctx, `
		INSERT INTO webhook_sources
			(id, tenant_id, name, provider, principal_id, grant_id, secret_ciphertext, config, enabled, secret_rotated_at)
		VALUES
			($1, $2, $3, $4, $5, $6, $7, $8, $9, now())
	`, s.ID, s.TenantID, s.Name, s.Provider, s.PrincipalID, s.GrantID, s.SecretCiphertext, configJSON, s.Enabled)
	if err != nil {
		return fmt.Errorf("create webhook source: %w", err)
	}
	return nil
}

const sourceColumns = `id, tenant_id, name, provider, principal_id, grant_id, secret_ciphertext, config, enabled, created_at, updated_at, rejection_count, last_rejection_at, last_rejection_reason, accepted_count, secret_rotated_at`

func scanSource(row pgx.Row) (*WebhookSource, error) {
	var s WebhookSource
	var configJSON []byte
	var lastRejectionReason *string
	if err := row.Scan(
		&s.ID, &s.TenantID, &s.Name, &s.Provider, &s.PrincipalID, &s.GrantID,
		&s.SecretCiphertext, &configJSON, &s.Enabled, &s.CreatedAt, &s.UpdatedAt,
		&s.RejectionCount, &s.LastRejectionAt, &lastRejectionReason,
		&s.AcceptedCount, &s.SecretRotatedAt,
	); err != nil {
		return nil, err
	}
	s.LastRejectionReason = deref(lastRejectionReason)
	if len(configJSON) > 0 {
		if err := json.Unmarshal(configJSON, &s.Config); err != nil {
			return nil, fmt.Errorf("unmarshal source config: %w", err)
		}
	}
	return &s, nil
}

func (r *postgresRepository) GetSource(ctx context.Context, tenantID, id string) (*WebhookSource, error) {
	return getOne(ctx, r.pool, "webhook source", ErrSourceNotFound, scanSource,
		`SELECT `+sourceColumns+` FROM webhook_sources WHERE id = $1 AND tenant_id = $2`, id, tenantID)
}

func (r *postgresRepository) GetSourceByID(ctx context.Context, id string) (*WebhookSource, error) {
	return getOne(ctx, r.pool, "webhook source by id", ErrSourceNotFound, scanSource,
		`SELECT `+sourceColumns+` FROM webhook_sources WHERE id = $1`, id)
}

func (r *postgresRepository) ListSources(ctx context.Context, tenantID string) ([]WebhookSource, error) {
	return queryList(ctx, r.pool, "webhook sources", scanSource,
		`SELECT `+sourceColumns+` FROM webhook_sources WHERE tenant_id = $1 ORDER BY created_at DESC`, tenantID)
}

func (r *postgresRepository) UpdateSource(ctx context.Context, s *WebhookSource) error {
	configJSON, err := json.Marshal(s.Config)
	if err != nil {
		return fmt.Errorf("marshal source config: %w", err)
	}
	// secret_rotated_at moves only when the stored ciphertext actually changes.
	// Every SET expression is evaluated against the pre-update row, so the
	// comparison reads the old secret. Deriving it here rather than passing a
	// flag means an ordinary enable/disable can never accidentally date the
	// secret forward and invalidate every replay of an older payload.
	tag, err := r.pool.Exec(ctx, `
		UPDATE webhook_sources
		SET    name = $1, secret_ciphertext = $2, grant_id = $3, config = $4, enabled = $5,
		       secret_rotated_at = CASE WHEN secret_ciphertext IS DISTINCT FROM $2
		                                THEN now() ELSE secret_rotated_at END,
		       updated_at = now()
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
// pending_count is how many fires are waiting on this trigger right now.
//
// Only unclaimed rows count: a claimed row is mid-dispatch, and reporting it as
// waiting would tell an operator to expect work that has in fact already
// started. Written as a correlated subquery rather than a join so every existing
// trigger read picks it up unchanged — all of them select from an unaliased
// `triggers` — and it is served by the partial index the claim path already
// maintains on (trigger_id, enqueued_at) WHERE claimed_at IS NULL.
const triggerColumns = `id, tenant_id, kind, target_kind, source_id, name, agent_type_id, agent_slug, event_type,
	match_condition, prompt_template, kind_config, principal_id, grant_id, grant_id_svc, enabled, created_at, updated_at,
	max_parallel, overflow, max_queue_depth, timeout_seconds, dedupe_enabled, autopause_enabled,
	consecutive_failures, auto_paused_at,
	(SELECT count(*) FROM trigger_pending_deliveries p
	  WHERE p.trigger_id = triggers.id AND p.claimed_at IS NULL) AS pending_count`

func scanTrigger(row pgx.Row) (*Trigger, error) {
	var t Trigger
	var condJSON, kindConfigJSON []byte
	var sourceID, principalID *string
	if err := row.Scan(
		&t.ID, &t.TenantID, &t.Kind, &t.TargetKind, &sourceID, &t.Name, &t.AgentTypeID, &t.AgentSlug,
		&t.EventType, &condJSON, &t.PromptTemplate, &kindConfigJSON,
		&principalID, &t.GrantID, &t.GrantIDSvc, &t.Enabled, &t.CreatedAt, &t.UpdatedAt,
		&t.MaxParallel, &t.Overflow, &t.MaxQueueDepth, &t.TimeoutSeconds, &t.DedupeEnabled,
		&t.AutopauseEnabled, &t.ConsecutiveFailures, &t.AutoPausedAt, &t.PendingCount,
	); err != nil {
		return nil, err
	}
	t.SourceID = deref(sourceID)
	t.PrincipalID = deref(principalID)
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
			 overflow, max_parallel, max_queue_depth, timeout_seconds,
			 dedupe_enabled, autopause_enabled)
		VALUES
			($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22)
	`, t.ID, t.TenantID, t.Kind, defaultTargetKind(t.TargetKind), sourceID, t.Name, t.AgentTypeID, t.AgentSlug, t.EventType,
		condJSON, t.PromptTemplate, kindConfigJSON, storage.NullIfZero(t.PrincipalID), t.GrantID, t.GrantIDSvc, t.Enabled,
		t.Overflow, t.MaxParallel, t.MaxQueueDepth, t.TimeoutSeconds,
		t.DedupeEnabled, t.AutopauseEnabled)
	if err != nil {
		return fmt.Errorf("create trigger: %w", err)
	}
	return nil
}

func (r *postgresRepository) GetTrigger(ctx context.Context, tenantID, id string) (*Trigger, error) {
	return getOne(ctx, r.pool, "trigger", ErrTriggerNotFound, scanTrigger,
		`SELECT `+triggerColumns+` FROM triggers WHERE id = $1 AND tenant_id = $2`, id, tenantID)
}

func (r *postgresRepository) ListTriggers(ctx context.Context, tenantID, agentTypeID string) ([]Trigger, error) {
	query := `SELECT ` + triggerColumns + ` FROM triggers WHERE tenant_id = $1`
	args := []any{tenantID}
	if agentTypeID != "" {
		query += ` AND agent_type_id = $2`
		args = append(args, agentTypeID)
	}
	query += ` ORDER BY created_at DESC`

	return queryList(ctx, r.pool, "triggers", scanTrigger, query, args...)
}

// matchablePredicate is the load-bearing definition of "matchable": enabled,
// plus auto-paused. It appears in every matchable listing as one constant so
// the two listings cannot drift apart on what matchable means.
const matchablePredicate = `(enabled = true OR auto_paused_at IS NOT NULL)`

// ListMatchableTriggersForSource returns the triggers a delivery is matched
// against: enabled, plus auto-paused.
//
// An auto-paused trigger is carried through matching so admission can record
// 'paused' against the delivery. Filtering it out here — as the enabled-only
// query did — leaves an auto-paused trigger looking exactly like a deleted one
// in the delivery history, which is the failure this listing exists to avoid. A
// trigger a person disabled is still excluded: that silence was asked for.
func (r *postgresRepository) ListMatchableTriggersForSource(ctx context.Context, sourceID string) ([]Trigger, error) {
	return queryList(ctx, r.pool, "triggers for source", scanTrigger, `
		SELECT `+triggerColumns+`
		FROM   triggers
		WHERE  source_id = $1 AND kind = 'webhook'
		  AND  `+matchablePredicate, sourceID)
}

// ListMatchableScheduledTriggers returns the scheduled triggers the cron loop
// evaluates: enabled, plus auto-paused, on the same reasoning as
// ListMatchableTriggersForSource. A paused schedule records that its fire was
// due and refused rather than going quiet.
func (r *postgresRepository) ListMatchableScheduledTriggers(ctx context.Context) ([]Trigger, error) {
	return queryList(ctx, r.pool, "scheduled triggers", scanTrigger, `
		SELECT `+triggerColumns+`
		FROM   triggers
		WHERE  kind = 'scheduled'
		  AND  `+matchablePredicate)
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
	// auto_paused_at is written here, not only by AutoPause. Re-enabling a
	// trigger clears the marker in the service, and leaving that unpersisted
	// meant a re-enabled trigger stayed paused as far as admission was
	// concerned — enabled in the console, refusing every fire.
	tag, err := r.pool.Exec(ctx, `
		UPDATE triggers
		SET    name = $1, event_type = $2, match_condition = $3, prompt_template = $4,
		       kind_config = $5, enabled = $6, dedupe_enabled = $7, autopause_enabled = $8,
		       max_parallel = $9, overflow = $10, max_queue_depth = $11, timeout_seconds = $12,
		       auto_paused_at = $13, updated_at = now()
		WHERE  id = $14 AND tenant_id = $15
	`, t.Name, t.EventType, condJSON, t.PromptTemplate, kindConfigJSON, t.Enabled,
		t.DedupeEnabled, t.AutopauseEnabled,
		t.MaxParallel, t.Overflow, t.MaxQueueDepth,
		t.TimeoutSeconds, t.AutoPausedAt, t.ID, t.TenantID)
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

// deliveryInsertColumns is the row a delivery is written as, shared by both
// insert paths so a new column cannot be added to one and forgotten in the
// other.
const deliveryInsertColumns = `
		INSERT INTO trigger_deliveries
			(id, tenant_id, source_id, trigger_id, trigger_name, kind, provider_delivery_id, event_type,
			 status, run_id, error, payload_excerpt, fired_by, scheduled_at,
			 payload, payload_bytes, reason, redelivery_of)
		VALUES
			($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
		`

// insertDelivery writes a delivery under the given ON CONFLICT clause, which is
// the only thing that differs between the two arrival kinds.
//
// Reports whether the row landed. That (bool, error) shape is the point rather
// than an oddity: the partial unique indexes are the concurrency control, so
// "was this the first arrival?" is answered by the database, not by a read the
// caller races against.
func (r *postgresRepository) insertDelivery(ctx context.Context, d *Delivery, onConflict, what string) (bool, error) {
	var sourceID *string
	if d.SourceID != "" {
		sourceID = &d.SourceID
	}
	tag, err := r.pool.Exec(ctx, deliveryInsertColumns+onConflict,
		d.ID, d.TenantID, sourceID, d.TriggerID, storage.NullIfZero(d.TriggerName), d.Kind, d.ProviderDeliveryID, d.EventType,
		d.Status, d.RunID, d.Error, d.PayloadExcerpt, storage.NullIfZero(d.FiredBy), d.ScheduledAt,
		d.Payload, storage.NullIfZero(d.PayloadBytes), d.Reason, d.RedeliveryOf)
	if err != nil {
		return false, fmt.Errorf("%s: %w", what, err)
	}
	return tag.RowsAffected() > 0, nil
}

// InsertDelivery inserts an inbound arrival, deduplicated on the provider's own
// delivery id.
//
// The arbiter predicate matches the partial index exactly, which is also what
// makes a lineage row insertable: a row naming the delivery it descends from is
// not an original arrival, so it is outside the uniqueness guarantee rather than
// blocked by it.
func (r *postgresRepository) InsertDelivery(ctx context.Context, d *Delivery) (bool, error) {
	return r.insertDelivery(ctx, d, `
		ON CONFLICT (source_id, provider_delivery_id)
			WHERE provider_delivery_id <> '' AND redelivery_of IS NULL
			DO NOTHING`, "insert delivery")
}

// InsertScheduledDelivery inserts a delivery for a scheduled trigger tick, using
// the scheduled_at dedup index to prevent a double fire on pod restart and to
// settle the race between replicas ticking the same minute.
func (r *postgresRepository) InsertScheduledDelivery(ctx context.Context, d *Delivery) (bool, error) {
	return r.insertDelivery(ctx, d, `
		ON CONFLICT (trigger_id, scheduled_at) WHERE scheduled_at IS NOT NULL DO NOTHING`,
		"insert scheduled delivery")
}

// UpdateDeliveryOutcome records the run a delivery produced.
//
// The arrival-level statuses are preserved, matching DeriveDeliveryStatus: what
// a duplicate or a missed fire IS does not change because a trigger later acted
// on it. A duplicate that re-ran (a trigger with dedupe off) stays 'duplicate'
// at the arrival level and carries 'dispatched' on its fan-out, which is the
// only shape that answers both "was this a repeat?" and "did anything run?".
func (r *postgresRepository) UpdateDeliveryOutcome(ctx context.Context, id, status string, triggerID, runID, errMsg *string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE trigger_deliveries d
		SET    status = CASE WHEN d.status IN ('duplicate', 'missed') THEN d.status ELSE $1 END,
		       trigger_id = COALESCE($2, d.trigger_id), run_id = $3, error = $4
		WHERE  d.id = $5
	`, status, triggerID, runID, errMsg, id)
	if err != nil {
		return fmt.Errorf("update delivery outcome: %w", err)
	}
	return nil
}

// deliveryColumns is the full read projection for a delivery row.
const deliveryColumns = `id, tenant_id, source_id, trigger_id, trigger_name, kind, provider_delivery_id, event_type,
	status, run_id, error, payload_excerpt, fired_by, scheduled_at, received_at,
	payload, payload_bytes, payload_purged, reason, redelivery_of`

func scanDelivery(row pgx.Row) (*Delivery, error) {
	var d Delivery
	var sourceID, firedBy, triggerName *string
	var payloadBytes *int
	if err := row.Scan(
		&d.ID, &d.TenantID, &sourceID, &d.TriggerID, &triggerName, &d.Kind, &d.ProviderDeliveryID, &d.EventType,
		&d.Status, &d.RunID, &d.Error, &d.PayloadExcerpt, &firedBy, &d.ScheduledAt, &d.ReceivedAt,
		&d.Payload, &payloadBytes, &d.PayloadPurged, &d.Reason, &d.RedeliveryOf,
	); err != nil {
		return nil, err
	}
	d.SourceID = deref(sourceID)
	d.FiredBy = deref(firedBy)
	d.TriggerName = deref(triggerName)
	d.PayloadBytes = deref(payloadBytes)
	return &d, nil
}

func (r *postgresRepository) GetDelivery(ctx context.Context, tenantID, id string) (*Delivery, error) {
	return getOne(ctx, r.pool, "delivery", ErrDeliveryNotFound, scanDelivery,
		`SELECT `+deliveryColumns+` FROM trigger_deliveries WHERE id = $1 AND tenant_id = $2`, id, tenantID)
}

// GetDeliveryByProviderID resolves the ORIGINAL delivery behind a provider
// delivery id. Lineage rows are excluded so a source that has already been
// replayed still resolves to the one arrival everything else descends from.
func (r *postgresRepository) GetDeliveryByProviderID(ctx context.Context, sourceID, providerDeliveryID string) (*Delivery, error) {
	return getOne(ctx, r.pool, "delivery by provider id", ErrDeliveryNotFound, scanDelivery, `
		SELECT `+deliveryColumns+`
		FROM   trigger_deliveries
		WHERE  source_id = $1 AND provider_delivery_id = $2 AND redelivery_of IS NULL
	`, sourceID, providerDeliveryID)
}

func (r *postgresRepository) ListDeliveries(ctx context.Context, tenantID string, filter DeliveryFilter) ([]Delivery, error) {
	query := `
		SELECT ` + deliveryColumns + `
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
	// filter.Limit is already clamped by the service; the storage layer trusts
	// its caller on business bounds and only refuses the nonsensical.
	limit := max(filter.Limit, 1)
	query += fmt.Sprintf(` LIMIT %d`, limit)

	return queryList(ctx, r.pool, "deliveries", scanDelivery, query, args...)
}

func (r *postgresRepository) InsertDeliveryRun(ctx context.Context, dr *DeliveryRun) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO trigger_delivery_runs
			(id, tenant_id, delivery_id, trigger_id, trigger_name, run_id, status, error)
		VALUES
			($1, $2, $3, $4, $5, $6, $7, $8)
	`, dr.ID, dr.TenantID, dr.DeliveryID, dr.TriggerID, storage.NullIfZero(dr.TriggerName), dr.RunID, dr.Status, dr.Error)
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
		SELECT id, tenant_id, delivery_id, trigger_id, trigger_name, run_id, status, error, created_at
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
		var triggerName *string
		if err := rows.Scan(
			&dr.ID, &dr.TenantID, &dr.DeliveryID, &dr.TriggerID, &triggerName,
			&dr.RunID, &dr.Status, &dr.Error, &dr.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan delivery run: %w", err)
		}
		dr.TriggerName = deref(triggerName)
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

// defaultTargetKind fills in the only target kind that exists today. Triggers
// are the front door for starting work; when the workflow engine lands, a
// trigger can name it here without changing anything else.
func defaultTargetKind(k string) string {
	if k == "" {
		return TargetKindAgent
	}
	return k
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

// RecordAccepted counts an inbound request that authenticated and was taken.
//
// The counterpart to RecordSourceRejection. A rejection count on its own cannot
// be read: it takes the accepted count beside it to tell "a handful of probes
// against a busy source" from "this source's signing secret is wrong".
func (r *postgresRepository) RecordAccepted(ctx context.Context, sourceID string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE webhook_sources SET accepted_count = accepted_count + 1 WHERE id = $1
	`, sourceID)
	if err != nil {
		return fmt.Errorf("record accepted delivery: %w", err)
	}
	return nil
}

// ClaimReplayKey records a body fingerprint for a source and says whether this
// source may take that body under this delivery id.
//
// One statement, so concurrent copies of a replayed body race each other in the
// database rather than in the process: the insert either wins or conflicts, and
// exactly one caller is told the body is fresh.
//
// Keyed per source, which is what makes the same body legitimately fresh for a
// different source. Sources are separate trust boundaries with separate signing
// secrets, so a body captured from one cannot verify against another in the
// first place; scoping the ledger any wider would let one source's traffic
// refuse another's. Reading a cross-source sequence as a replay failure is a
// misreading of that boundary, not a hole in it.
//
// Two ON CONFLICT escapes, both deliberate. A key older than the window is
// refreshed and accepted, because the window is the whole point of not keeping
// this ledger forever. A key whose delivery id matches is accepted, because that
// is a provider retrying an unacknowledged delivery — an ordinary duplicate the
// delivery history already records as one, not the attack. The attack is the
// same body under a delivery id the sender chose freshly, and that is the only
// case left refused.
func (r *postgresRepository) ClaimReplayKey(ctx context.Context, sourceID string, bodyHash []byte, providerDeliveryID string, window time.Duration) (bool, error) {
	var seenAt time.Time
	err := r.pool.QueryRow(ctx, `
		INSERT INTO trigger_delivery_replay_keys (source_id, body_hash, provider_delivery_id)
		VALUES ($1, $2, $4)
		ON CONFLICT (source_id, body_hash) DO UPDATE
		    SET seen_at = now(), provider_delivery_id = EXCLUDED.provider_delivery_id
		    WHERE trigger_delivery_replay_keys.seen_at < now() - make_interval(secs => $3)
		       OR trigger_delivery_replay_keys.provider_delivery_id = EXCLUDED.provider_delivery_id
		RETURNING seen_at
	`, sourceID, bodyHash, window.Seconds(), providerDeliveryID).Scan(&seenAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("claim replay key: %w", err)
	}
	return true, nil
}

// PruneReplayKeys drops fingerprints past the replay window, keeping the table a
// window rather than a permanent record of every body ever received.
func (r *postgresRepository) PruneReplayKeys(ctx context.Context, window time.Duration) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM trigger_delivery_replay_keys WHERE seen_at < now() - make_interval(secs => $1)
	`, window.Seconds())
	if err != nil {
		return 0, fmt.Errorf("prune replay keys: %w", err)
	}
	return tag.RowsAffected(), nil
}

// CountRedeliveries is how many replays already descend from one delivery.
func (r *postgresRepository) CountRedeliveries(ctx context.Context, originalID string) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `
		SELECT count(*) FROM trigger_deliveries WHERE redelivery_of = $1
	`, originalID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count redeliveries: %w", err)
	}
	return n, nil
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

// settledRunStatesSQL is the SQL value list of run states that no longer occupy
// one of a trigger's parallel slots.
//
// 'idle' belongs here even though orbit-agents does not treat it as terminal. An
// idle run has finished its turn and is waiting for a message that will never
// come: a trigger-started run has no person on the other end. Counting it as
// in-flight until the 30-minute run reaper collects it throttles a healthy
// frequent trigger to zero, and then evicts real work at the backlog ceiling —
// the failure the ceiling exists to prevent, caused by the ceiling itself.
//
// One constant, interpolated into both the claim query and InFlightCount, so the
// two cannot disagree about what "in flight" means. It is a compile-time literal
// with no caller input in it.
const settledRunStatesSQL = `('completed', 'failed', 'budget_exhausted', 'idle')`

// EnqueueWithinCeiling admits a fire only if the trigger is under its ceiling,
// deciding and inserting under one lock so the two cannot disagree.
//
// Counting and then inserting as separate statements is a time-of-check race:
// two fires a moment apart both read an occupancy below the ceiling and both
// enqueue, so a trigger set to refuse quietly queues one instead. Actual
// concurrency is still bounded, because ClaimPending ranks per trigger and
// will not dispatch past max_parallel, but the fire was told "accepted" when
// refuse promises "not now", and those are different answers to the sender.
//
// Serialized on the trigger row rather than an advisory lock: the row is
// already the thing whose max_parallel is being read, the lock is scoped to one
// trigger so unrelated triggers never contend, and it needs no hashing scheme
// to collide over. Returns false when the fire was refused.
func (r *postgresRepository) EnqueueWithinCeiling(ctx context.Context, p *PendingDelivery, maxParallel int) (bool, error) {
	fieldsJSON, marshalErr := json.Marshal(p.Fields)
	if marshalErr != nil {
		return false, fmt.Errorf("marshal pending fields: %w", marshalErr)
	}

	admitted := false
	txErr := storage.WithTransaction(ctx, r.pool, func(tx pgx.Tx) error {
		var lockedID string
		if err := tx.QueryRow(ctx,
			`SELECT id FROM triggers WHERE id = $1 FOR UPDATE`, p.TriggerID).Scan(&lockedID); err != nil {
			return fmt.Errorf("lock trigger for admission: %w", err)
		}

		var occupied int
		if err := tx.QueryRow(ctx, occupancySQL, p.TriggerID, StaleClaimWindow.Seconds()).Scan(&occupied); err != nil {
			return fmt.Errorf("count occupancy: %w", err)
		}
		if occupied >= maxParallel {
			// Refused, not failed: commit the (empty) transaction and report it.
			return nil
		}

		if _, err := tx.Exec(ctx, pendingInsertSQL,
			p.ID, p.TenantID, p.TriggerID, p.DeliveryID, fieldsJSON); err != nil {
			return fmt.Errorf("enqueue pending delivery: %w", err)
		}
		admitted = true
		return nil
	})
	if txErr != nil {
		return false, txErr
	}
	return admitted, nil
}

// pendingInsertSQL is the one statement that enqueues a pending row, shared by
// the ceiling-checked and unconditional paths so a new column cannot be added
// to one and forgotten in the other (the failure deliveryInsertColumns exists
// to prevent for deliveries).
const pendingInsertSQL = `
	INSERT INTO trigger_pending_deliveries (id, tenant_id, trigger_id, delivery_id, fields)
	VALUES ($1, $2, $3, $4, $5)
`

func (r *postgresRepository) EnqueuePending(ctx context.Context, p *PendingDelivery) error {
	fieldsJSON, err := json.Marshal(p.Fields)
	if err != nil {
		return fmt.Errorf("marshal pending fields: %w", err)
	}
	if _, err := r.pool.Exec(ctx, pendingInsertSQL,
		p.ID, p.TenantID, p.TriggerID, p.DeliveryID, fieldsJSON); err != nil {
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
//
// The outer UPDATE repeats `claimed_at IS NULL` even though the CTE already
// filtered on it. Under READ COMMITTED, a row locked by a concurrent claim is
// re-read after that claim commits (EvalPlanQual), and the re-read row can be
// one this statement's snapshot saw as unclaimed. Re-checking at the point of
// write is what makes a double claim impossible rather than merely unlikely.
func (r *postgresRepository) ClaimPending(ctx context.Context, limit int) ([]PendingDelivery, error) {
	// in_flight counts dispatched fan-out rows with non-terminal runs plus
	// already-claimed pendings. The claimed half is bounded by the same window
	// the reaper uses: a claim older than that belongs to a worker that died,
	// and letting it count would keep costing the trigger a slot in the interval
	// before the reaper takes the row back. ranked gives each waiting row a per-trigger
	// position so a single batch admits at most (max_parallel - in_flight)
	// rows per trigger. Without the ranking, every waiting row of an
	// under-ceiling trigger satisfies the capacity check at once, and a burst
	// blows straight through the ceiling.
	rows, err := r.pool.Query(ctx, `
		WITH in_flight AS (
		    SELECT trigger_id, count(*) AS n FROM (
		        SELECT dr.trigger_id
		        FROM   trigger_delivery_runs dr
		        LEFT   JOIN runs ru ON ru.id = dr.run_id
		        WHERE  dr.status = 'dispatched'
		          AND  dr.created_at > now() - interval '1 hour'
		          AND  (ru.id IS NULL OR ru.state NOT IN `+settledRunStatesSQL+`)
		        UNION ALL
		        SELECT trigger_id
		        FROM   trigger_pending_deliveries
		        WHERE  claimed_at IS NOT NULL
		          AND  claimed_at > now() - make_interval(secs => $2)
		    ) x GROUP BY trigger_id
		),
		ranked AS (
		    SELECT p.id, p.enqueued_at,
		           row_number() OVER (PARTITION BY p.trigger_id ORDER BY p.enqueued_at) AS position,
		           COALESCE(f.n, 0) AS in_flight,
		           t.max_parallel
		    FROM   trigger_pending_deliveries p
		    JOIN   triggers t ON t.id = p.trigger_id
		    LEFT   JOIN in_flight f ON f.trigger_id = p.trigger_id
		    WHERE  p.claimed_at IS NULL
		      AND  t.enabled
		),
		claimable AS (
		    SELECT p.id
		    FROM   trigger_pending_deliveries p
		    JOIN   ranked k ON k.id = p.id
		    WHERE  k.in_flight + k.position <= k.max_parallel
		    ORDER  BY k.enqueued_at
		    LIMIT  $1
		    FOR UPDATE OF p SKIP LOCKED
		)
		UPDATE trigger_pending_deliveries p
		SET    claimed_at = now()
		FROM   claimable c
		WHERE  p.id = c.id AND p.claimed_at IS NULL
		RETURNING p.id, p.tenant_id, p.trigger_id, p.delivery_id, p.fields, p.enqueued_at
	`, limit, StaleClaimWindow.Seconds())
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

// ReclaimStalePending returns fires whose worker never came back to the queue.
//
// The row is un-claimed rather than deleted: the fire was acknowledged and has
// not run, so it is owed a dispatch. Claiming it again is the normal path, and
// the next tick picks it up like any other waiting fire.
func (r *postgresRepository) ReclaimStalePending(ctx context.Context, olderThan time.Duration) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
		UPDATE trigger_pending_deliveries
		SET    claimed_at = NULL
		WHERE  claimed_at IS NOT NULL
		  AND  claimed_at < now() - make_interval(secs => $1)
		RETURNING id
	`, olderThan.Seconds())
	if err != nil {
		return nil, fmt.Errorf("reclaim stale pending deliveries: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan reclaimed pending delivery: %w", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate reclaimed pending deliveries: %w", err)
	}
	return out, nil
}

// InFlightCount counts runs this trigger started that have not settled.
//
// Bounded by an hour so a run whose terminal outcome was never recorded (a lost
// pod, a dropped callback) cannot hold a slot forever. A stuck slot would
// silently throttle a healthy trigger to nothing, which is worse than briefly
// over-admitting.
func (r *postgresRepository) InFlightCount(ctx context.Context, triggerID string) (int, error) {
	// In flight = dispatched fan-out rows whose run has not settled (see
	// settledRunStatesSQL, which counts 'idle' as settled because a
	// trigger-started run has nobody to send it the next message), plus
	// pending rows a worker has claimed but not yet dispatched
	// (invisible to the fan-out until the outcome commits). The runs table is
	// owned by orbit-agents; this read-only join is a deliberate deployment
	// coupling (one Postgres, pooled migrations) so max_parallel can mean
	// "runs actually running" rather than "started within the horizon". The
	// hour horizon stays as the backstop for a run whose terminal state was
	// lost: over-admit beats wedging the trigger forever. The claimed-pending
	// half is bounded on the same principle: a claim older than the reaper's
	// window belongs to a worker that died, and counting it would let one dead
	// pod permanently cost the trigger a parallel slot.
	var n int
	err := r.pool.QueryRow(ctx, inFlightSQL, triggerID, StaleClaimWindow.Seconds()).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count in-flight runs: %w", err)
	}
	return n, nil
}

// inFlightSQL counts the work a trigger currently has running. $1 is the trigger
// id, $2 the stale-claim window in seconds.
const inFlightSQL = `
	SELECT
	    (SELECT count(*)
	     FROM   trigger_delivery_runs dr
	     LEFT   JOIN runs ru ON ru.id = dr.run_id
	     WHERE  dr.trigger_id = $1
	       AND  dr.status = 'dispatched'
	       AND  dr.created_at > now() - interval '1 hour'
	       AND  (ru.id IS NULL OR ru.state NOT IN ` + settledRunStatesSQL + `))
	  + (SELECT count(*)
	     FROM   trigger_pending_deliveries
	     WHERE  trigger_id = $1
	       AND  claimed_at IS NOT NULL
	       AND  claimed_at > now() - make_interval(secs => $2))
`

// occupancySQL is what "refuse" measures against the ceiling: work in flight
// plus fires still waiting. Waiting fires count because in-flight alone only
// sees what a worker has already picked up, and the worker polls on an interval,
// so two fires a moment apart would both read an empty count. Same parameters as
// inFlightSQL.
const occupancySQL = inFlightSQL + `
	  + (SELECT count(*)
	     FROM   trigger_pending_deliveries
	     WHERE  trigger_id = $1 AND claimed_at IS NULL)
`

// SupersedePending and DrainPending share one implementation: both remove
// every waiting row. The two names stay at the interface because they carry
// different intent — replace-on-overflow versus disable-and-drain — and the
// delivery history records different reasons for each.
func (r *postgresRepository) SupersedePending(ctx context.Context, triggerID string) ([]PendingDelivery, error) {
	return r.deleteUnclaimedPending(ctx, triggerID, "supersede")
}

// TrimPending evicts the oldest waiting rows beyond depth, leaving room for the
// one row the caller is about to enqueue.
//
// Oldest rather than newest: when a trigger is behind, the freshest events are
// the ones still worth acting on.
//
// depth is the ceiling INCLUDING the fire about to be added, so the offset is
// depth-1. Trimming to depth and then inserting left the backlog one over its
// declared maximum, permanently — a max_queue_depth of 32 that always held 33.
// The trim and the insert are not one transaction, so under a burst several
// admissions can interleave between a trim and its insert and the depth can
// briefly exceed the ceiling; the next admission trims it back. Bounding the
// backlog is the guarantee, not bounding it to the exact row.
func (r *postgresRepository) TrimPending(ctx context.Context, triggerID string, depth int) ([]PendingDelivery, error) {
	offset := max(depth-1, 0)
	rows, err := r.pool.Query(ctx, `
		DELETE FROM trigger_pending_deliveries
		WHERE  id IN (
		    SELECT id FROM trigger_pending_deliveries
		    WHERE  trigger_id = $1 AND claimed_at IS NULL
		    ORDER  BY enqueued_at DESC
		    OFFSET $2
		)
		RETURNING id, tenant_id, trigger_id, delivery_id, fields, enqueued_at
	`, triggerID, offset)
	if err != nil {
		return nil, fmt.Errorf("trim pending deliveries: %w", err)
	}
	defer rows.Close()
	return scanPending(rows)
}

// DrainPending removes everything still WAITING for a trigger.
//
// Claimed rows are left alone. A claimed row is being dispatched right now by a
// worker that will record its own outcome and then release it; deleting it from
// under that dispatch produced two fan-out rows for one fire (a 'dropped' from
// the drain and a 'failed' or 'dispatched' from the live worker), which is a
// delivery history that contradicts itself. The claimed fire finishes on its own
// terms; it is at most one per parallel slot, and DispatchPending re-reads the
// trigger, so a disabled one is refused there anyway.
func (r *postgresRepository) DrainPending(ctx context.Context, triggerID string) ([]PendingDelivery, error) {
	return r.deleteUnclaimedPending(ctx, triggerID, "drain")
}

// deleteUnclaimedPending removes every waiting (unclaimed) row for a trigger
// and returns them. Claimed rows are left alone — see DrainPending's doc for
// why a mid-dispatch row must finish on its own terms.
func (r *postgresRepository) deleteUnclaimedPending(ctx context.Context, triggerID, what string) ([]PendingDelivery, error) {
	rows, err := r.pool.Query(ctx, `
		DELETE FROM trigger_pending_deliveries
		WHERE  trigger_id = $1 AND claimed_at IS NULL
		RETURNING id, tenant_id, trigger_id, delivery_id, fields, enqueued_at
	`, triggerID)
	if err != nil {
		return nil, fmt.Errorf("%s pending deliveries: %w", what, err)
	}
	defer rows.Close()
	return scanPending(rows)
}

// DrainOrphanedPending collects waiting fires whose trigger can no longer run
// them: it was deleted, or it is switched off.
//
// The lifecycle paths that disable a trigger drain its backlog themselves, so
// this is the backstop for the windows they cannot cover — a fire admitted in
// the instant between the disable committing and the drain running, or a trigger
// row deleted by a path that never went through the service. Nothing else would
// ever collect those: ClaimPending requires t.enabled, and the stale-claim reaper
// only touches rows a worker already claimed. Without this they wait forever and
// count against the queue depth gauge for good.
//
// Claimed rows are excluded on the same reasoning as DrainPending.
func (r *postgresRepository) DrainOrphanedPending(ctx context.Context, limit int) ([]OrphanedPending, error) {
	rows, err := r.pool.Query(ctx, `
		DELETE FROM trigger_pending_deliveries p
		USING (
		    SELECT p2.id, t.id AS found_id, t.name AS trigger_name
		    FROM   trigger_pending_deliveries p2
		    LEFT   JOIN triggers t ON t.id = p2.trigger_id
		    WHERE  p2.claimed_at IS NULL
		      AND  (t.id IS NULL OR t.enabled = false)
		    ORDER  BY p2.enqueued_at
		    LIMIT  $1
		) g
		WHERE  p.id = g.id
		RETURNING p.id, p.tenant_id, p.trigger_id, p.delivery_id, p.fields, p.enqueued_at,
		          g.trigger_name, (g.found_id IS NOT NULL)
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("drain orphaned pending deliveries: %w", err)
	}
	defer rows.Close()

	var out []OrphanedPending
	for rows.Next() {
		var o OrphanedPending
		var fieldsJSON []byte
		var triggerName *string
		if err := rows.Scan(&o.ID, &o.TenantID, &o.TriggerID, &o.DeliveryID, &fieldsJSON,
			&o.EnqueuedAt, &triggerName, &o.TriggerExists); err != nil {
			return nil, fmt.Errorf("scan orphaned pending delivery: %w", err)
		}
		if len(fieldsJSON) > 0 {
			if err := json.Unmarshal(fieldsJSON, &o.Fields); err != nil {
				return nil, fmt.Errorf("unmarshal orphaned pending fields: %w", err)
			}
		}
		o.TriggerName = deref(triggerName)
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate orphaned pending deliveries: %w", err)
	}
	return out, nil
}

// UnclaimedPendingCount is how many fires are waiting on a trigger right now.
func (r *postgresRepository) UnclaimedPendingCount(ctx context.Context, triggerID string) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `
		SELECT count(*) FROM trigger_pending_deliveries
		WHERE  trigger_id = $1 AND claimed_at IS NULL
	`, triggerID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count unclaimed pending deliveries: %w", err)
	}
	return n, nil
}

// TouchPendingClaim re-stamps a claim immediately before its dispatch begins.
//
// A batch is claimed in one statement and dispatched one row at a time, each
// bounded by the trigger's own timeout. Without this, the last row of a full
// batch carries a claim stamped before the first row's dispatch even started,
// which can be far older than the stale-claim window by the time it runs — and
// another replica's reaper would take it back and dispatch it a second time,
// mid-flight. Re-stamping makes the window mean what its name says: how long
// THIS dispatch has been running.
func (r *postgresRepository) TouchPendingClaim(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE trigger_pending_deliveries SET claimed_at = now()
		WHERE  id = $1 AND claimed_at IS NOT NULL
	`, id)
	if err != nil {
		return fmt.Errorf("re-stamp pending claim: %w", err)
	}
	return nil
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

// GetRetention reads a tenant's chosen retention window.
//
// The absence of a row is meaningful and is reported as such: a tenant who has
// never chosen inherits the platform default, and would move with it. Returning
// the default as though it were stored would erase that difference.
func (r *postgresRepository) GetRetention(ctx context.Context, tenantID string) (int, bool, error) {
	var days int
	err := r.pool.QueryRow(ctx, `SELECT payload_days FROM trigger_retention_settings WHERE tenant_id = $1`, tenantID).Scan(&days)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("get retention settings: %w", err)
	}
	return days, true, nil
}

// SetRetention upserts a tenant's retention window. The CHECK constraint is the
// backstop, not the validation: the service rejects out-of-range values first,
// so a caller gets a message about days rather than a constraint violation.
func (r *postgresRepository) SetRetention(ctx context.Context, tenantID string, days int) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO trigger_retention_settings (tenant_id, payload_days)
		VALUES ($1, $2)
		ON CONFLICT (tenant_id) DO UPDATE
		SET payload_days = EXCLUDED.payload_days, updated_at = now()
	`, tenantID, days)
	if err != nil {
		return fmt.Errorf("set retention settings: %w", err)
	}
	return nil
}

// ObservedEventTypes lists what a source has actually delivered.
//
// Read from history rather than from a counter kept on the source: the history
// is already the record, and a counter would be a second thing to keep true.
func (r *postgresRepository) ObservedEventTypes(ctx context.Context, sourceID string) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT event_type
		FROM   trigger_deliveries
		WHERE  source_id = $1 AND event_type <> ''
		ORDER  BY event_type
	`, sourceID)
	if err != nil {
		return nil, fmt.Errorf("list observed event types: %w", err)
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var et string
		if err := rows.Scan(&et); err != nil {
			return nil, fmt.Errorf("scan observed event type: %w", err)
		}
		out = append(out, et)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate observed event types: %w", err)
	}
	return out, nil
}

// PendingStats summarizes the queue in a single round trip.
//
// The oldest waiting fire's age is the half that matters: depth alone cannot
// distinguish a busy queue that drains from a stalled one that does not.
func (r *postgresRepository) PendingStats(ctx context.Context) (PendingStats, error) {
	var waiting, claimed int64
	var oldestSeconds float64
	err := r.pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE claimed_at IS NULL),
		       count(*) FILTER (WHERE claimed_at IS NOT NULL),
		       COALESCE(EXTRACT(EPOCH FROM (now() - min(enqueued_at) FILTER (WHERE claimed_at IS NULL))), 0)
		FROM   trigger_pending_deliveries
	`).Scan(&waiting, &claimed, &oldestSeconds)
	if err != nil {
		return PendingStats{}, fmt.Errorf("read pending stats: %w", err)
	}
	return PendingStats{
		Waiting:            waiting,
		Claimed:            claimed,
		OldestUnclaimedAge: time.Duration(oldestSeconds * float64(time.Second)),
	}, nil
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
