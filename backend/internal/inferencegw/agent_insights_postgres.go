package inferencegw

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Every query here runs with pgx.QueryExecModeExec, so Postgres plans it with
// the actual window. A prepared statement is planned once without it, and its
// generic plan reads most of the table through an index (about 2 seconds for a
// 90d window at 150 agents, against about 0.3 for the plan chosen with the real
// values).

// The predicate is two SQL strings, not one with a bound admin flag. Tenant is
// $1. A non-admin sees rows without a delegator and their own delegated rows. An
// admin sees every row.
const (
	whereAdmin  = "tenant_id = $1 AND agent_slug IS NOT NULL"
	whereOthers = "tenant_id = $1 AND agent_slug IS NOT NULL AND (delegator_id IS NULL OR delegator_id = $2)"
)

// where returns the predicate and its arguments after the tenant, which is $1.
func (c usageCaller) where() (string, []any) {
	if c.Admin {
		return whereAdmin, []any{c.TenantID}
	}
	return whereOthers, []any{c.TenantID, c.Subject}
}

// scope adds the window and the optional single agent to a predicate. The
// placeholders are numbered after the caller's arguments.
func (q usageQuery) scope(from, to time.Time) (string, []any) {
	clause, args := q.Caller.where()
	args = append(args, from, to)
	clause += fmt.Sprintf(" AND created_at >= $%d AND created_at < $%d", len(args)-1, len(args))
	if q.Slug != "" {
		args = append(args, q.Slug)
		clause += fmt.Sprintf(" AND agent_slug = $%d", len(args))
	}
	return clause, args
}

func withExecMode(args []any) []any { return append([]any{pgx.QueryExecModeExec}, args...) }

// AgentUsage returns tokens and cost per agent over the window, or for one
// agent. Agents with no usage in the window are absent.
func (r *PostgresInferenceUsageRepository) AgentUsage(ctx context.Context, q usageQuery) ([]AgentTypeUsage, error) {
	clause, args := q.scope(q.Window.Start, q.Window.End)
	rows, err := r.pool.Query(ctx, `
		SELECT agent_slug,
		       COALESCE(SUM(prompt_tokens), 0),
		       COALESCE(SUM(completion_tokens), 0),
		       COALESCE(SUM(cache_read_input_tokens), 0),
		       COALESCE(SUM(cache_creation_input_tokens), 0),
		       COALESCE(SUM(total_tokens), 0),
		       COALESCE(SUM(estimated_cost_usd), 0)
		FROM inference_usage
		WHERE `+clause+`
		GROUP BY agent_slug
		ORDER BY agent_slug`, withExecMode(args)...)
	if err != nil {
		return nil, fmt.Errorf("agent usage: %w", err)
	}
	defer rows.Close()

	var out []AgentTypeUsage
	for rows.Next() {
		var u AgentTypeUsage
		if err := rows.Scan(&u.AgentSlug, &u.PromptTokens, &u.CompletionTokens, &u.CacheReadTokens,
			&u.CacheCreationTokens, &u.TotalTokens, &u.EstimatedCostUSD); err != nil {
			return nil, fmt.Errorf("scan agent usage: %w", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// AgentSpendByModel returns the window's spend per model, largest first.
func (r *PostgresInferenceUsageRepository) AgentSpendByModel(ctx context.Context, q usageQuery) ([]ModelSpend, error) {
	clause, args := q.scope(q.Window.Start, q.Window.End)
	rows, err := r.pool.Query(ctx, `
		SELECT model_id, COALESCE(SUM(estimated_cost_usd), 0) AS cost
		FROM inference_usage
		WHERE `+clause+`
		GROUP BY model_id
		HAVING COALESCE(SUM(estimated_cost_usd), 0) > 0
		ORDER BY cost DESC, model_id`, withExecMode(args)...)
	if err != nil {
		return nil, fmt.Errorf("agent spend by model: %w", err)
	}
	defer rows.Close()

	var out []ModelSpend
	for rows.Next() {
		var m ModelSpend
		if err := rows.Scan(&m.ModelID, &m.CostUSD); err != nil {
			return nil, fmt.Errorf("scan agent spend by model: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// AgentSpendBuckets returns the previous window's total and the spend in each
// bucket of the window. Each is one FILTER sum over the same rows, so the query
// has no GROUP BY and Postgres runs it as a parallel aggregate.
//
// A plain GROUP BY on the bucket was measured against this at 150 agents and 3M
// rows over 90 days. With the bucket from floor(date_part('epoch', ...)) it took
// 1.0 s against 0.29 s here, and with date_bin 0.55 s. Neither was within 1.5x of
// the FILTER sums at every window, so the FILTER form stays.
func (r *PostgresInferenceUsageRepository) AgentSpendBuckets(ctx context.Context, q usageQuery) (float64, []float64, error) {
	w := q.Window
	n := w.Buckets()
	clause, args := q.scope(w.PreviousStart(), w.End)

	// The previous total is everything before the window start, then one sum per bucket.
	args = append(args, w.Start)
	cols := []string{fmt.Sprintf("COALESCE(SUM(estimated_cost_usd) FILTER (WHERE created_at < $%d), 0)", len(args))}
	for i := range n {
		args = append(args, w.Start.Add(time.Duration(i)*w.Bucket), w.Start.Add(time.Duration(i+1)*w.Bucket))
		cols = append(cols, fmt.Sprintf("COALESCE(SUM(estimated_cost_usd) FILTER (WHERE created_at >= $%d AND created_at < $%d), 0)", len(args)-1, len(args)))
	}

	vals := make([]float64, n+1)
	dest := make([]any, n+1)
	for i := range vals {
		dest[i] = &vals[i]
	}
	row := r.pool.QueryRow(ctx, "SELECT "+strings.Join(cols, ", ")+" FROM inference_usage WHERE "+clause, withExecMode(args)...)
	if err := row.Scan(dest...); err != nil {
		return 0, nil, fmt.Errorf("agent spend buckets: %w", err)
	}
	return vals[0], vals[1:], nil
}

// AgentBudgetSpend returns each listed agent's spend since weekStart and since
// monthStart, and whether it has delegated rows in that span. It applies no
// delegator predicate: a delegated agent is dropped by the caller, and every
// other agent has no delegated rows, so the sums are the same for every caller.
func (r *PostgresInferenceUsageRepository) AgentBudgetSpend(ctx context.Context, tenantID string, slugs []string, weekStart, monthStart time.Time) ([]AgentBudgetSpend, error) {
	if len(slugs) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, strings.TrimSpace(`
		SELECT agent_slug,
		       COALESCE(SUM(estimated_cost_usd) FILTER (WHERE created_at >= $3), 0),
		       COALESCE(SUM(estimated_cost_usd) FILTER (WHERE created_at >= $4), 0),
		       COALESCE(bool_or(delegator_id IS NOT NULL), false)
		FROM inference_usage
		WHERE tenant_id = $1
		  AND agent_slug = ANY($2)
		  AND created_at >= LEAST($3::timestamptz, $4::timestamptz)
		GROUP BY agent_slug`), pgx.QueryExecModeExec, tenantID, slugs, weekStart, monthStart)
	if err != nil {
		return nil, fmt.Errorf("agent budget spend: %w", err)
	}
	defer rows.Close()

	var out []AgentBudgetSpend
	for rows.Next() {
		var b AgentBudgetSpend
		if err := rows.Scan(&b.AgentSlug, &b.Week, &b.Month, &b.Delegated); err != nil {
			return nil, fmt.Errorf("scan agent budget spend: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
