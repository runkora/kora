package triggers

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/runkora/kora/backend/internal/platform/declarative"
	"github.com/runkora/kora/backend/internal/platform/storage"
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
			(id, tenant_id, name, provider, principal_id, grant_id, secret_ciphertext, config, enabled, secret_rotated_at,
			 source, resource_uid, repository)
		VALUES
			($1, $2, $3, $4, $5, $6, $7, $8, $9, now(), $10, $11, $12)
	`, s.ID, s.TenantID, s.Name, s.Provider, s.PrincipalID, s.GrantID, s.SecretCiphertext, configJSON, s.Enabled,
		cmp.Or(s.Source, declarative.SourceAPI), storage.NullIfZero(s.ResourceUID), storage.NullIfZero(s.Repository))
	if err != nil {
		return sourceWriteError("create webhook source", err)
	}
	return nil
}

// sourceNameKey is the unique constraint on a source's name within its tenant.
const sourceNameKey = "webhook_sources_tenant_id_name_key"

// sourceWriteError maps a duplicate source name to ErrSourceNameTaken and
// wraps any other error with what.
func sourceWriteError(what string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == sourceNameKey {
		return ErrSourceNameTaken
	}
	return fmt.Errorf("%s: %w", what, err)
}

const sourceColumns = `id, tenant_id, name, provider, principal_id, grant_id, secret_ciphertext, config, enabled, created_at, updated_at, rejection_count, last_rejection_at, last_rejection_reason, accepted_count, secret_rotated_at, source, resource_uid, repository`

func scanSource(row pgx.Row) (*WebhookSource, error) {
	var s WebhookSource
	var configJSON []byte
	var lastRejectionReason, resourceUID, repository *string
	if err := row.Scan(
		&s.ID, &s.TenantID, &s.Name, &s.Provider, &s.PrincipalID, &s.GrantID,
		&s.SecretCiphertext, &configJSON, &s.Enabled, &s.CreatedAt, &s.UpdatedAt,
		&s.RejectionCount, &s.LastRejectionAt, &lastRejectionReason,
		&s.AcceptedCount, &s.SecretRotatedAt, &s.Source, &resourceUID, &repository,
	); err != nil {
		return nil, err
	}
	s.LastRejectionReason = deref(lastRejectionReason)
	s.ResourceUID, s.Repository = deref(resourceUID), deref(repository)
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

func (r *postgresRepository) GetSourceByName(ctx context.Context, tenantID, name string) (*WebhookSource, error) {
	return getOne(ctx, r.pool, "webhook source by name", ErrSourceNotFound, scanSource,
		`SELECT `+sourceColumns+` FROM webhook_sources WHERE tenant_id = $1 AND name = $2`, tenantID, name)
}

func (r *postgresRepository) ListDeclaredSources(ctx context.Context, tenantID string) ([]declarative.Declared, error) {
	rows, err := r.pool.Query(ctx, `SELECT name, resource_uid FROM webhook_sources WHERE tenant_id = $1 AND source = 'crd'`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list declared webhook sources: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[declarative.Declared])
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
		SET    name = $1, secret_ciphertext = $2, grant_id = $3, config = $4, enabled = $5, repository = $8,
		       secret_rotated_at = CASE WHEN secret_ciphertext IS DISTINCT FROM $2
		                                THEN now() ELSE secret_rotated_at END,
		       updated_at = now()
		WHERE  id = $6 AND tenant_id = $7
	`, s.Name, s.SecretCiphertext, s.GrantID, configJSON, s.Enabled, s.ID, s.TenantID, storage.NullIfZero(s.Repository))
	if err != nil {
		return sourceWriteError("update webhook source", err)
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
	consecutive_failures, auto_paused_at, source, resource_uid, repository,
	(SELECT count(*) FROM trigger_pending_deliveries p
	  WHERE p.trigger_id = triggers.id AND p.claimed_at IS NULL) AS pending_count`

func scanTrigger(row pgx.Row) (*Trigger, error) {
	var t Trigger
	var condJSON, kindConfigJSON []byte
	var sourceID, principalID, agentTypeID, agentSlug, resourceUID, repository *string
	if err := row.Scan(
		&t.ID, &t.TenantID, &t.Kind, &t.TargetKind, &sourceID, &t.Name, &agentTypeID, &agentSlug,
		&t.EventType, &condJSON, &t.PromptTemplate, &kindConfigJSON,
		&principalID, &t.GrantID, &t.GrantIDSvc, &t.Enabled, &t.CreatedAt, &t.UpdatedAt,
		&t.MaxParallel, &t.Overflow, &t.MaxQueueDepth, &t.TimeoutSeconds, &t.DedupeEnabled,
		&t.AutopauseEnabled, &t.ConsecutiveFailures, &t.AutoPausedAt, &t.Source, &resourceUID, &repository, &t.PendingCount,
	); err != nil {
		return nil, err
	}
	t.ResourceUID, t.Repository = deref(resourceUID), deref(repository)
	t.SourceID = deref(sourceID)
	t.SourceMissing = t.Kind == KindWebhook && sourceID == nil
	t.PrincipalID = deref(principalID)
	t.AgentTypeID = deref(agentTypeID)
	t.AgentSlug = deref(agentSlug)
	if len(condJSON) > 0 {
		if err := json.Unmarshal(condJSON, &t.MatchCondition); err != nil {
			return nil, fmt.Errorf("unmarshal match condition: %w", err)
		}
	}
	if err := decodeKindConfig(&t, kindConfigJSON); err != nil {
		return nil, err
	}
	return &t, nil
}

// decodeKindConfig reads kind_config, whose shape follows the trigger's kind.
func decodeKindConfig(t *Trigger, raw []byte) error {
	if len(raw) == 0 || string(raw) == "{}" {
		return nil
	}
	switch t.Kind {
	case KindScheduled:
		var sc ScheduleConfig
		if err := json.Unmarshal(raw, &sc); err != nil {
			return fmt.Errorf("unmarshal schedule config: %w", err)
		}
		if sc.CronExpression != "" {
			t.ScheduleConfig = &sc
		}
	case KindManual:
		var mc ManualConfig
		if err := json.Unmarshal(raw, &mc); err != nil {
			return fmt.Errorf("unmarshal manual config: %w", err)
		}
		if len(mc.Inputs) > 0 {
			t.ManualConfig = &mc
		}
	case KindWebhook:
		var wc WebhookConfig
		if err := json.Unmarshal(raw, &wc); err != nil {
			return fmt.Errorf("unmarshal webhook config: %w", err)
		}
		if sr := (*SenderRestriction)(wc.SenderRestriction); !sr.empty() {
			t.SenderRestriction = sr
		}
		if len(wc.MatchContains) > 0 {
			t.MatchContains = wc.MatchContains
		}
		if wc.Coalesce.Enabled() {
			t.Coalesce = wc.Coalesce
		}
	}
	return nil
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

	return storage.WithTransaction(ctx, r.pool, func(tx pgx.Tx) error {
		if err := claimTriggerName(ctx, tx, t); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO triggers
				(id, tenant_id, kind, target_kind, source_id, name, agent_type_id, agent_slug, event_type,
				 match_condition, prompt_template, kind_config, principal_id, grant_id, grant_id_svc, enabled,
				 overflow, max_parallel, max_queue_depth, timeout_seconds,
				 dedupe_enabled, autopause_enabled, source, resource_uid, repository)
			VALUES
				($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25)
		`, t.ID, t.TenantID, t.Kind, defaultTargetKind(t.TargetKind), sourceID, t.Name,
			storage.NullIfZero(t.AgentTypeID), storage.NullIfZero(t.AgentSlug), t.EventType,
			condJSON, t.PromptTemplate, kindConfigJSON, storage.NullIfZero(t.PrincipalID), t.GrantID, t.GrantIDSvc, t.Enabled,
			t.Overflow, t.MaxParallel, t.MaxQueueDepth, t.TimeoutSeconds,
			t.DedupeEnabled, t.AutopauseEnabled, cmp.Or(t.Source, declarative.SourceAPI), storage.NullIfZero(t.ResourceUID), storage.NullIfZero(t.Repository)); err != nil {
			return fmt.Errorf("create trigger: %w", err)
		}
		return writeRoute(ctx, tx, t)
	})
}

// claimTriggerName serializes writes of one trigger name in a tenant and
// refuses a name a trigger of the other source holds. API triggers may share a
// name with each other.
func claimTriggerName(ctx context.Context, tx pgx.Tx, t *Trigger) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1 || '/' || $2))`, t.TenantID, t.Name); err != nil {
		return fmt.Errorf("lock trigger name: %w", err)
	}
	source := cmp.Or(t.Source, declarative.SourceAPI)
	var held bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM triggers WHERE tenant_id = $1 AND name = $2 AND source <> $3 AND id <> $4)`,
		t.TenantID, t.Name, source, t.ID).Scan(&held); err != nil {
		return fmt.Errorf("check trigger name: %w", err)
	}
	switch {
	case !held:
		return nil
	case source == declarative.SourceAPI:
		return ErrDeclaredNameTaken
	default:
		return ErrAPINameTaken
	}
}

// writeRoute replaces a routed trigger's settings and candidates. Other
// triggers have none to write.
func writeRoute(ctx context.Context, tx pgx.Tx, t *Trigger) error {
	if t.Routing == nil {
		return nil
	}
	rt := t.Routing
	if _, err := tx.Exec(ctx, `
		INSERT INTO trigger_routes (trigger_id, tenant_id, model, confidence, fallback_agent_type_id, join_runs, join_key)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (trigger_id) DO UPDATE
		SET model = EXCLUDED.model, confidence = EXCLUDED.confidence,
		    fallback_agent_type_id = EXCLUDED.fallback_agent_type_id,
		    join_runs = EXCLUDED.join_runs, join_key = EXCLUDED.join_key
	`, t.ID, t.TenantID, rt.Model, rt.Confidence, rt.FallbackAgentTypeID, rt.joins(), storage.NullIfZero(rt.JoinKey)); err != nil {
		return fmt.Errorf("write trigger route: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM trigger_route_candidates WHERE trigger_id = $1`, t.ID); err != nil {
		return fmt.Errorf("clear route candidates: %w", err)
	}
	for _, id := range rt.Candidates {
		if _, err := tx.Exec(ctx, `
			INSERT INTO trigger_route_candidates (trigger_id, agent_type_id, agent_slug) VALUES ($1, $2, $3)
		`, t.ID, id, rt.candidateSlugs[id]); err != nil {
			return fmt.Errorf("write route candidate: %w", err)
		}
	}
	return nil
}

// attachRoutes reads the routing object of every routed trigger in ts.
func (r *postgresRepository) attachRoutes(ctx context.Context, ts []*Trigger) error {
	byID := map[string]*Trigger{}
	for _, t := range ts {
		if t.TargetKind == TargetKindRouted {
			byID[t.ID] = t
		}
	}
	if len(byID) == 0 {
		return nil
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT rt.trigger_id, rt.model, rt.confidence, rt.fallback_agent_type_id, rt.join_runs, rt.join_key,
		       c.agent_type_id, c.agent_slug
		FROM   trigger_routes rt
		JOIN   trigger_route_candidates c ON c.trigger_id = rt.trigger_id
		WHERE  rt.trigger_id = ANY($1)
		ORDER  BY rt.trigger_id, c.agent_type_id
	`, ids)
	if err != nil {
		return fmt.Errorf("list trigger routes: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var triggerID, model, confidence, agentTypeID, slug string
		var fallback, joinKey *string
		var joinRuns bool
		if err := rows.Scan(&triggerID, &model, &confidence, &fallback, &joinRuns, &joinKey, &agentTypeID, &slug); err != nil {
			return fmt.Errorf("scan trigger route: %w", err)
		}
		t := byID[triggerID]
		if t.Routing == nil {
			t.Routing = &RoutingConfig{
				Model: model, Confidence: confidence, FallbackAgentTypeID: fallback,
				JoinRuns: &joinRuns, JoinKey: deref(joinKey), candidateSlugs: map[string]string{},
			}
		}
		t.Routing.Candidates = append(t.Routing.Candidates, agentTypeID)
		t.Routing.candidateSlugs[agentTypeID] = slug
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate trigger routes: %w", err)
	}
	return nil
}

// listTriggers runs a trigger listing and attaches the routing of routed triggers.
func (r *postgresRepository) listTriggers(ctx context.Context, what, sql string, args ...any) ([]Trigger, error) {
	ts, err := queryList(ctx, r.pool, what, scanTrigger, sql, args...)
	if err != nil {
		return nil, err
	}
	ptrs := make([]*Trigger, len(ts))
	for i := range ts {
		ptrs[i] = &ts[i]
	}
	if err := r.attachRoutes(ctx, ptrs); err != nil {
		return nil, err
	}
	return ts, nil
}

func (r *postgresRepository) GetTrigger(ctx context.Context, tenantID, id string) (*Trigger, error) {
	t, err := getOne(ctx, r.pool, "trigger", ErrTriggerNotFound, scanTrigger,
		`SELECT `+triggerColumns+` FROM triggers WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	if err != nil {
		return nil, err
	}
	if err := r.attachRoutes(ctx, []*Trigger{t}); err != nil {
		return nil, err
	}
	return t, nil
}

func (r *postgresRepository) ListTriggers(ctx context.Context, tenantID, agentTypeID string) ([]Trigger, error) {
	query := `SELECT ` + triggerColumns + ` FROM triggers WHERE tenant_id = $1`
	args := []any{tenantID}
	if agentTypeID != "" {
		// A routed trigger lists under each of its candidate agents.
		query += ` AND (agent_type_id = $2 OR id IN (SELECT trigger_id FROM trigger_route_candidates WHERE agent_type_id = $2))`
		args = append(args, agentTypeID)
	}
	query += ` ORDER BY created_at DESC`

	return r.listTriggers(ctx, "triggers", query, args...)
}

// GetDeclaredTrigger returns the trigger a Trigger resource projects to.
func (r *postgresRepository) GetDeclaredTrigger(ctx context.Context, tenantID, name string) (*Trigger, error) {
	t, err := getOne(ctx, r.pool, "declared trigger", ErrTriggerNotFound, scanTrigger,
		`SELECT `+triggerColumns+` FROM triggers WHERE tenant_id = $1 AND name = $2 AND source = 'crd'`, tenantID, name)
	if err != nil {
		return nil, err
	}
	if err := r.attachRoutes(ctx, []*Trigger{t}); err != nil {
		return nil, err
	}
	return t, nil
}

// TriggerNameTaken reports whether a trigger from source already has the name in the tenant.
func (r *postgresRepository) TriggerNameTaken(ctx context.Context, tenantID, name, source string) (bool, error) {
	var taken bool
	if err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM triggers WHERE tenant_id = $1 AND name = $2 AND source = $3)`,
		tenantID, name, source).Scan(&taken); err != nil {
		return false, fmt.Errorf("check trigger name: %w", err)
	}
	return taken, nil
}

func (r *postgresRepository) ListDeclaredTriggers(ctx context.Context, tenantID string) ([]declarative.Declared, error) {
	rows, err := r.pool.Query(ctx, `SELECT name, resource_uid FROM triggers WHERE tenant_id = $1 AND source = 'crd'`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list declared triggers: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[declarative.Declared])
}

// UpdateDeclaredTrigger writes every field a Trigger resource owns, including
// the agent, the source and the grant the API cannot change. enabled is left
// alone while the trigger is auto-paused: only clearing the pause re-enables it.
func (r *postgresRepository) UpdateDeclaredTrigger(ctx context.Context, t *Trigger) error {
	condJSON, err := json.Marshal(t.MatchCondition)
	if err != nil {
		return fmt.Errorf("marshal match condition: %w", err)
	}
	kindConfigJSON, err := marshalKindConfig(t)
	if err != nil {
		return err
	}
	return storage.WithTransaction(ctx, r.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE triggers
			SET    source_id = $1, agent_type_id = $2, agent_slug = $3, event_type = $4, match_condition = $5,
			       prompt_template = $6, kind_config = $7, grant_id = $8, dedupe_enabled = $9, autopause_enabled = $10,
			       max_parallel = $11, overflow = $12, max_queue_depth = $13, timeout_seconds = $14,
			       enabled = CASE WHEN auto_paused_at IS NULL THEN $15 ELSE enabled END,
			       repository = $18, updated_at = now()
			WHERE  id = $16 AND tenant_id = $17
		`, storage.NullIfZero(t.SourceID), storage.NullIfZero(t.AgentTypeID), storage.NullIfZero(t.AgentSlug),
			t.EventType, condJSON, t.PromptTemplate, kindConfigJSON, t.GrantID, t.DedupeEnabled, t.AutopauseEnabled,
			t.MaxParallel, t.Overflow, t.MaxQueueDepth, t.TimeoutSeconds, t.Enabled, t.ID, t.TenantID,
			storage.NullIfZero(t.Repository))
		if err != nil {
			return fmt.Errorf("update declared trigger: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrTriggerNotFound
		}
		return writeRoute(ctx, tx, t)
	})
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
	return r.listTriggers(ctx, "triggers for source", `
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

func (r *postgresRepository) UpdateTrigger(ctx context.Context, t *Trigger, writeEnablement, writeRouting bool) error {
	return r.updateTrigger(ctx, t, writeEnablement, writeRouting, false, nil)
}

// UpdateTriggerGrant writes the trigger only while its stored grant is still
// oldGrantID. A concurrent update that replaced the grant first wins, and this
// one returns ErrTriggerConflict.
func (r *postgresRepository) UpdateTriggerGrant(ctx context.Context, t *Trigger, writeEnablement bool, oldGrantID *string) error {
	return r.updateTrigger(ctx, t, writeEnablement, true, true, oldGrantID)
}

func (r *postgresRepository) updateTrigger(ctx context.Context, t *Trigger, writeEnablement, writeRouting, guardGrant bool, oldGrantID *string) error {
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
	//
	// But only when this edit actually toggled enablement (writeEnablement).
	// An edit that did not — a rename, a filter change — carries the enabled
	// and auto_paused_at it read at the start of the request, and writing that
	// stale pair back would revert an auto-pause that landed in between. The
	// CASE keeps the write conditional inside one static statement rather than
	// building two.
	//
	// grant_id is written only by the grant-guarded update, so an edit that did
	// not replace the grant cannot overwrite one a concurrent update swapped in.
	// The routing rows are replaced in the same transaction, and only when the
	// edit changed routing, so a stale copy cannot revert a concurrent change.
	return storage.WithTransaction(ctx, r.pool, func(tx pgx.Tx) error {
		if err := claimTriggerName(ctx, tx, t); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
			UPDATE triggers
			SET    name = $1, event_type = $2, match_condition = $3, prompt_template = $4,
			       kind_config = $5, dedupe_enabled = $7, autopause_enabled = $8,
			       max_parallel = $9, overflow = $10, max_queue_depth = $11, timeout_seconds = $12,
			       enabled = CASE WHEN $16 THEN $6 ELSE enabled END,
			       auto_paused_at = CASE WHEN $16 THEN $13 ELSE auto_paused_at END,
			       grant_id = CASE WHEN $18 THEN $17 ELSE grant_id END,
			       updated_at = now()
			WHERE  id = $14 AND tenant_id = $15
			  AND  (NOT $18 OR grant_id IS NOT DISTINCT FROM $19::text)
		`, t.Name, t.EventType, condJSON, t.PromptTemplate, kindConfigJSON, t.Enabled,
			t.DedupeEnabled, t.AutopauseEnabled,
			t.MaxParallel, t.Overflow, t.MaxQueueDepth,
			t.TimeoutSeconds, t.AutoPausedAt, t.ID, t.TenantID, writeEnablement, t.GrantID, guardGrant, oldGrantID)
		if err != nil {
			return fmt.Errorf("update trigger: %w", err)
		}
		if tag.RowsAffected() == 0 {
			if guardGrant {
				return grantGuardMiss(ctx, tx, t)
			}
			return ErrTriggerNotFound
		}
		if !writeRouting {
			return nil
		}
		return writeRoute(ctx, tx, t)
	})
}

// grantGuardMiss tells a concurrent grant swap (409) from a deleted trigger
// (404), so a caller is not told to retry an update that cannot land.
func grantGuardMiss(ctx context.Context, tx pgx.Tx, t *Trigger) error {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM triggers WHERE id = $1 AND tenant_id = $2)`,
		t.ID, t.TenantID).Scan(&exists); err != nil {
		return fmt.Errorf("check trigger exists: %w", err)
	}
	if !exists {
		return ErrTriggerNotFound
	}
	return ErrTriggerConflict
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
		       trigger_id = COALESCE($2, d.trigger_id), run_id = COALESCE($3, d.run_id), error = $4
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

// deliveryColumnsNoPayload is deliveryColumns with the payload body left out.
const deliveryColumnsNoPayload = `id, tenant_id, source_id, trigger_id, trigger_name, kind, provider_delivery_id, event_type,
	status, run_id, error, payload_excerpt, fired_by, scheduled_at, received_at,
	NULL::bytea AS payload, payload_bytes, payload_purged, reason, redelivery_of`

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
	columns := deliveryColumns
	if filter.OmitPayload {
		columns = deliveryColumnsNoPayload
	}
	query := `
		SELECT ` + columns + `
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
		idx++
	}
	if !filter.ReceivedAfter.IsZero() {
		query += fmt.Sprintf(` AND received_at >= $%d`, idx)
		args = append(args, filter.ReceivedAfter)
		idx++
	}
	if !filter.ReceivedBefore.IsZero() {
		query += fmt.Sprintf(` AND received_at < $%d`, idx)
		args = append(args, filter.ReceivedBefore)
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
			(id, tenant_id, delivery_id, trigger_id, trigger_name, run_id, status, error, superseded_by)
			VALUES
				($1, $2, $3, $4, $5, $6, $7, $8, $9)
		`, dr.ID, dr.TenantID, dr.DeliveryID, dr.TriggerID, storage.NullIfZero(dr.TriggerName), dr.RunID, dr.Status, dr.Error, dr.SupersededBy)
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
		SELECT dr.id, dr.tenant_id, dr.delivery_id, dr.trigger_id, dr.trigger_name, dr.run_id, dr.status, dr.error,
		       dr.created_at, dr.superseded_by,
		       rd.outcome, rd.key_matched, rd.join_key, rd.model, rd.agent_type_id, rd.probability, rd.threshold,
		       rd.scores, rd.fallback_reason, rd.error
		FROM   trigger_delivery_runs dr
		LEFT   JOIN trigger_routing_decisions rd ON rd.delivery_id = dr.delivery_id AND rd.trigger_id = dr.trigger_id
		WHERE  dr.tenant_id = $1 AND dr.delivery_id = ANY($2)
		ORDER BY dr.created_at ASC
	`, tenantID, deliveryIDs)
	if err != nil {
		return nil, fmt.Errorf("list delivery runs: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var dr DeliveryRun
		var triggerName, outcome *string
		var keyMatched *bool
		var scores []byte
		var rd RoutingDecision
		if err := rows.Scan(
			&dr.ID, &dr.TenantID, &dr.DeliveryID, &dr.TriggerID, &triggerName,
			&dr.RunID, &dr.Status, &dr.Error, &dr.CreatedAt, &dr.SupersededBy,
			&outcome, &keyMatched, &rd.JoinKey, &rd.Model, &rd.AgentTypeID, &rd.Probability, &rd.Threshold,
			&scores, &rd.FallbackReason, &rd.Error,
		); err != nil {
			return nil, fmt.Errorf("scan delivery run: %w", err)
		}
		dr.TriggerName = deref(triggerName)
		if outcome != nil {
			rd.Outcome, rd.KeyMatched = *outcome, deref(keyMatched)
			if err := json.Unmarshal(scores, &rd.Scores); err != nil {
				return nil, fmt.Errorf("unmarshal routing scores: %w", err)
			}
			dr.Routing = &rd
		}
		out[dr.DeliveryID] = append(out[dr.DeliveryID], dr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate delivery runs: %w", err)
	}
	return out, nil
}

func (r *postgresRepository) InsertDeliveryKey(ctx context.Context, k *DeliveryKey) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO trigger_delivery_keys (delivery_id, trigger_id, tenant_id, coalesce_key, key_too_long)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (delivery_id, trigger_id) DO NOTHING
	`, k.DeliveryID, k.TriggerID, k.TenantID, storage.NullIfZero(k.Key), k.KeyTooLong)
	if err != nil {
		return fmt.Errorf("insert delivery key: %w", err)
	}
	return nil
}

func (r *postgresRepository) ListDeliveryKeysForDeliveries(ctx context.Context, tenantID string, deliveryIDs []string) (map[string][]DeliveryKey, error) {
	out := make(map[string][]DeliveryKey)
	if len(deliveryIDs) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT delivery_id, trigger_id, tenant_id, coalesce_key, key_too_long
		FROM   trigger_delivery_keys
		WHERE  tenant_id = $1 AND delivery_id = ANY($2)
		ORDER BY created_at ASC, trigger_id ASC
	`, tenantID, deliveryIDs)
	if err != nil {
		return nil, fmt.Errorf("list delivery keys: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var k DeliveryKey
		var key *string
		if err := rows.Scan(&k.DeliveryID, &k.TriggerID, &k.TenantID, &key, &k.KeyTooLong); err != nil {
			return nil, fmt.Errorf("scan delivery key: %w", err)
		}
		k.Key = deref(key)
		out[k.DeliveryID] = append(out[k.DeliveryID], k)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate delivery keys: %w", err)
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
	case t.Kind == KindWebhook && (!t.SenderRestriction.empty() || len(t.MatchContains) > 0 || t.Coalesce.Enabled()):
		wc := WebhookConfig{MatchContains: t.MatchContains}
		if t.Coalesce.Enabled() {
			wc.Coalesce = t.Coalesce
		}
		if !t.SenderRestriction.empty() {
			wc.SenderRestriction = (*storedSenderRestriction)(t.SenderRestriction)
		}
		b, err := json.Marshal(wc)
		if err != nil {
			return nil, fmt.Errorf("marshal webhook config: %w", err)
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
//
// final tells the caller whether this is the delivery's last word: every
// matched trigger has either produced a terminal/dispatched outcome row or
// still owns a waiting or claimed row in trigger_pending_deliveries.
// excludePendingID discounts the caller's own pending-queue row from that
// check — see the Repository interface doc.
func (r *postgresRepository) DeriveDeliveryStatus(ctx context.Context, deliveryID, excludePendingID string) (status string, final bool, err error) {
	// 'filtered' is deliberately not a failure. A trigger that declined an event
	// via its own match_condition worked exactly as configured, so a delivery
	// whose only outcomes are filtered is 'skipped' — nothing was ever going to
	// run — not 'failed'.
	// computed is the status this delivery's outcomes resolve to today, worked
	// out once in a CTE so the UPDATE below can both apply it and compare
	// against it without repeating the CASE expression.
	queryErr := r.pool.QueryRow(ctx, `
		WITH computed AS (
			SELECT CASE
					WHEN EXISTS (
						SELECT 1 FROM trigger_delivery_runs
						WHERE delivery_id = $1 AND status IN ('dispatched', 'joined')
					) THEN 'dispatched'
					WHEN EXISTS (
						SELECT 1 FROM trigger_delivery_runs
						WHERE delivery_id = $1
						  AND status IN ('failed', 'denied', 'dropped')
					) THEN 'failed'
					ELSE 'skipped'
				END AS status
		)
		UPDATE trigger_deliveries d
		SET status = computed.status
		FROM computed
		WHERE d.id = $1
		  AND d.status NOT IN ('duplicate', 'missed')
		  AND d.status IS DISTINCT FROM computed.status
		RETURNING d.status,
			NOT EXISTS (
				SELECT 1 FROM trigger_pending_deliveries p
				WHERE p.delivery_id = $1 AND p.id <> $2
			)
	`, deliveryID, excludePendingID).Scan(&status, &final)
	if queryErr != nil {
		if errors.Is(queryErr, pgx.ErrNoRows) {
			// Nothing to derive: the delivery is already arrival-level
			// ('duplicate'/'missed') or does not exist, or the computed status
			// matches what is already stored — a repeat derive after an
			// unchanged outcome must not recount the delivery. Not an error —
			// the caller's metric simply has nothing to attribute.
			return "", false, nil
		}
		return "", false, fmt.Errorf("derive delivery status: %w", queryErr)
	}
	return status, final, nil
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
// 'idle' belongs here even though kora-agents does not treat it as terminal. An
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
		      AND  (p.dispatch_after IS NULL OR p.dispatch_after <= now())
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
		RETURNING p.id, p.tenant_id, p.trigger_id, p.delivery_id, p.fields, p.enqueued_at, p.hold_count
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
	// owned by kora-agents; this read-only join is a deliberate deployment
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

// SupersedeAndEnqueue replaces a replace-overflow trigger's waiting backlog
// with one new fire, atomically: it locks the trigger row, deletes every
// unclaimed row (returning them so the caller records each as superseded), and
// inserts the replacement, all in one transaction.
//
// The two guarantees are what a plain supersede-then-enqueue could not give.
// The lock serializes concurrent replace fires on the same trigger, so two
// cannot each supersede the other's nothing and both enqueue, leaving a "keep
// only the newest" trigger holding two waiting rows. The single transaction
// means a crash cannot commit the supersede — recording the displaced fires as
// replaced — and then fail to enqueue the replacement that was supposed to
// replace them.
func (r *postgresRepository) SupersedeAndEnqueue(ctx context.Context, p *PendingDelivery) ([]PendingDelivery, error) {
	fieldsJSON, err := json.Marshal(p.Fields)
	if err != nil {
		return nil, fmt.Errorf("marshal pending fields: %w", err)
	}

	var superseded []PendingDelivery
	txErr := storage.WithTransaction(ctx, r.pool, func(tx pgx.Tx) error {
		var lockedID string
		if err := tx.QueryRow(ctx,
			`SELECT id FROM triggers WHERE id = $1 FOR UPDATE`, p.TriggerID).Scan(&lockedID); err != nil {
			return fmt.Errorf("lock trigger for supersede: %w", err)
		}

		rows, err := tx.Query(ctx, `
			DELETE FROM trigger_pending_deliveries
			WHERE  trigger_id = $1 AND claimed_at IS NULL
			RETURNING id, tenant_id, trigger_id, delivery_id, fields, enqueued_at, hold_count
		`, p.TriggerID)
		if err != nil {
			return fmt.Errorf("supersede pending deliveries: %w", err)
		}
		superseded, err = scanPending(rows)
		// Close before the next query: pgx allows only one active result per
		// connection, and the insert below runs on this same transaction.
		rows.Close()
		if err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, pendingInsertSQL,
			p.ID, p.TenantID, p.TriggerID, p.DeliveryID, fieldsJSON); err != nil {
			return fmt.Errorf("enqueue replacement: %w", err)
		}
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}
	return superseded, nil
}

// EnqueueCoalescing enqueues a fire that waits out a coalescing window, and
// replaces the waiting fire with the same key if its window is still open.
//
// The window is fixed from the first fire: a replacement keeps the row's
// dispatch_after and takes only the newest fire's delivery id and fields, so a
// steady stream cannot hold a key back forever.
//
// The trigger row is locked first, the same serialization SupersedeAndEnqueue
// uses. Two concurrent first fires with one key would otherwise both find no
// open row and both insert, and a window cannot be a unique index because its
// end is a moment in time. A row a worker has claimed, or one whose window has
// closed, is not replaced: the new fire opens its own window.
//
// The UPDATE repeats claimed_at IS NULL in its own WHERE for the reason
// ClaimPending re-checks its predicate: under READ COMMITTED a concurrent claim
// can stamp the row after the CTE read it.
func (r *postgresRepository) EnqueueCoalescing(ctx context.Context, p *PendingDelivery, window time.Duration) (string, error) {
	fieldsJSON, err := json.Marshal(p.Fields)
	if err != nil {
		return "", fmt.Errorf("marshal pending fields: %w", err)
	}

	var replaced string
	txErr := storage.WithTransaction(ctx, r.pool, func(tx pgx.Tx) error {
		var lockedID string
		if err := tx.QueryRow(ctx,
			`SELECT id FROM triggers WHERE id = $1 FOR UPDATE`, p.TriggerID).Scan(&lockedID); err != nil {
			return fmt.Errorf("lock trigger for coalescing: %w", err)
		}

		err := tx.QueryRow(ctx, `
			WITH open AS (
			    SELECT id, delivery_id FROM trigger_pending_deliveries
			    WHERE  trigger_id = $1 AND coalesce_key = $2
			      AND  claimed_at IS NULL AND dispatch_after > now()
			    FOR UPDATE
			)
			UPDATE trigger_pending_deliveries p
			SET    delivery_id = $3, fields = $4, enqueued_at = now()
			FROM   open
			WHERE  p.id = open.id AND p.claimed_at IS NULL
			RETURNING open.delivery_id
		`, p.TriggerID, p.CoalesceKey, p.DeliveryID, fieldsJSON).Scan(&replaced)
		if err == nil {
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("replace coalesced fire: %w", err)
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO trigger_pending_deliveries
				(id, tenant_id, trigger_id, delivery_id, fields, coalesce_key, dispatch_after)
			VALUES ($1, $2, $3, $4, $5, $6, now() + make_interval(secs => $7))
		`, p.ID, p.TenantID, p.TriggerID, p.DeliveryID, fieldsJSON, p.CoalesceKey, window.Seconds()); err != nil {
			return fmt.Errorf("enqueue coalescing fire: %w", err)
		}
		return nil
	})
	if txErr != nil {
		return "", txErr
	}
	return replaced, nil
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
//
// The outer DELETE repeats claimed_at IS NULL rather than trusting the
// subquery's copy of it, for the same reason ClaimPending re-checks its own
// predicate: under READ COMMITTED a concurrent claim can lock and stamp a row
// after the subquery selected it, and the EvalPlanQual recheck only re-tests
// the outer WHERE. Without the outer predicate the just-claimed row is deleted
// anyway, giving one fire both a 'dropped' (evicted) outcome here and a live
// worker's 'dispatched' — the self-contradicting history the queue avoids.
func (r *postgresRepository) TrimPending(ctx context.Context, triggerID string, depth int) ([]PendingDelivery, error) {
	offset := max(depth-1, 0)
	rows, err := r.pool.Query(ctx, `
		DELETE FROM trigger_pending_deliveries
		WHERE  claimed_at IS NULL
		  AND  id IN (
		    SELECT id FROM trigger_pending_deliveries
		    WHERE  trigger_id = $1 AND claimed_at IS NULL
		    ORDER  BY enqueued_at DESC
		    OFFSET $2
		)
		RETURNING id, tenant_id, trigger_id, delivery_id, fields, enqueued_at, hold_count
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
		RETURNING id, tenant_id, trigger_id, delivery_id, fields, enqueued_at, hold_count
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

// TouchPendingClaims re-stamps the given claims, keeping a batch's still-waiting
// rows inside the stale-claim window.
//
// A batch is claimed in one statement and dispatched one row at a time, each
// bounded by the trigger's own timeout. Without re-stamping, a row waiting
// behind slow dispatches carries a claim stamped before the first dispatch even
// began — far older than the stale-claim window by the time its turn comes, so
// another replica's reaper takes it back and dispatches it a second time,
// mid-flight. The worker re-stamps the rows still ahead of it before each
// dispatch; one dispatch is bounded below the window (timeout_seconds caps at
// 600s, under the 900s window), so while the worker lives the whole remaining
// batch stays inside it.
func (r *postgresRepository) TouchPendingClaims(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE trigger_pending_deliveries SET claimed_at = now()
		WHERE  id = ANY($1) AND claimed_at IS NOT NULL
	`, ids)
	if err != nil {
		return fmt.Errorf("re-stamp pending claims: %w", err)
	}
	return nil
}

func scanPending(rows pgx.Rows) ([]PendingDelivery, error) {
	var out []PendingDelivery
	for rows.Next() {
		var p PendingDelivery
		var fieldsJSON []byte
		if err := rows.Scan(&p.ID, &p.TenantID, &p.TriggerID, &p.DeliveryID, &fieldsJSON, &p.EnqueuedAt, &p.HoldCount); err != nil {
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
// distinguish a busy queue that drains from a stalled one that does not. A fire
// held in a coalescing window ages from the moment it became claimable, so a
// long window does not read as a stalled worker.
func (r *postgresRepository) PendingStats(ctx context.Context) (PendingStats, error) {
	var waiting, claimed int64
	var oldestSeconds float64
	err := r.pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE claimed_at IS NULL),
		       count(*) FILTER (WHERE claimed_at IS NOT NULL),
		       GREATEST(COALESCE(EXTRACT(EPOCH FROM (now() - min(GREATEST(enqueued_at, dispatch_after)) FILTER (WHERE claimed_at IS NULL))), 0), 0)
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

// ─── Routing ──────────────────────────────────────────────────────────────────

// LatestJoinKeyRun reads the newest decision of one trigger with this join key
// and a run, through trigger_routing_decisions_join_key_idx. Keys of other
// triggers are never compared (trigger routing ADR, Decision 7).
func (r *postgresRepository) LatestJoinKeyRun(ctx context.Context, tenantID, triggerID, joinKey string) (string, string, bool, error) {
	var runID string
	var agentTypeID *string
	err := r.pool.QueryRow(ctx, `
		SELECT run_id, agent_type_id
		FROM   trigger_routing_decisions
		WHERE  trigger_id = $1 AND tenant_id = $2 AND join_key = $3 AND run_id IS NOT NULL
		ORDER  BY created_at DESC
		LIMIT  1
	`, triggerID, tenantID, joinKey).Scan(&runID, &agentTypeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, fmt.Errorf("look up join key: %w", err)
	}
	return runID, deref(agentTypeID), true, nil
}

func (r *postgresRepository) InsertRoutingDecision(ctx context.Context, d *RoutingDecision) error {
	scores, err := json.Marshal(d.Scores)
	if err != nil {
		return fmt.Errorf("marshal routing scores: %w", err)
	}
	var startFields []byte
	if len(d.StartFields) > 0 {
		if startFields, err = json.Marshal(d.StartFields); err != nil {
			return fmt.Errorf("marshal start fields: %w", err)
		}
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO trigger_routing_decisions
			(delivery_id, trigger_id, tenant_id, question_version, model, outcome, join_key, key_matched,
			 agent_type_id, run_id, threshold, probability, confidence, scores, fallback_reason, error, start_fields)
		VALUES
			($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		ON CONFLICT (delivery_id, trigger_id) DO NOTHING
	`, d.DeliveryID, d.TriggerID, d.TenantID, d.QuestionVersion, d.Model, d.Outcome, d.JoinKey, d.KeyMatched,
		d.AgentTypeID, d.RunID, d.Threshold, d.Probability, d.Confidence, scores, d.FallbackReason, d.Error, startFields)
	if err != nil {
		return fmt.Errorf("insert routing decision: %w", err)
	}
	return nil
}

func (r *postgresRepository) StartFieldsByDelivery(ctx context.Context, tenantID, triggerID string, deliveryIDs []string) (map[string]map[string]string, error) {
	out := map[string]map[string]string{}
	if len(deliveryIDs) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT delivery_id, start_fields
		FROM   trigger_routing_decisions
		WHERE  tenant_id = $1 AND trigger_id = $2 AND delivery_id = ANY($3) AND start_fields IS NOT NULL
	`, tenantID, triggerID, deliveryIDs)
	if err != nil {
		return nil, fmt.Errorf("read start fields: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, fmt.Errorf("scan start fields: %w", err)
		}
		var fields map[string]string
		if err := json.Unmarshal(raw, &fields); err != nil || len(fields) == 0 {
			continue
		}
		out[id] = fields
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read start fields: %w", err)
	}
	return out, nil
}

// HoldPending returns a claimed fire to the queue until delay has passed and
// counts the hold, and records reason on the delivery, in one transaction.
func (r *postgresRepository) HoldPending(ctx context.Context, pendingID, deliveryID string, delay time.Duration, reason string) error {
	return storage.WithTransaction(ctx, r.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE trigger_pending_deliveries
			SET    claimed_at = NULL, dispatch_after = now() + make_interval(secs => $2), hold_count = hold_count + 1
			WHERE  id = $1 AND claimed_at IS NOT NULL
		`, pendingID, delay.Seconds())
		if err != nil {
			return fmt.Errorf("hold pending delivery: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrPendingNotClaimed
		}
		if _, err := tx.Exec(ctx, `UPDATE trigger_deliveries SET reason = $2 WHERE id = $1`, deliveryID, reason); err != nil {
			return fmt.Errorf("record hold reason: %w", err)
		}
		return nil
	})
}

// ClearDeliveryReason removes the reason HoldPending recorded on a delivery.
func (r *postgresRepository) ClearDeliveryReason(ctx context.Context, deliveryID string) error {
	if _, err := r.pool.Exec(ctx, `UPDATE trigger_deliveries SET reason = NULL WHERE id = $1`, deliveryID); err != nil {
		return fmt.Errorf("clear delivery reason: %w", err)
	}
	return nil
}
