package inferencegw

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// The visibility predicate, as two SQL strings so an admin query has no delegator
// clause at all. Tenant is $1. A non-admin sees rows without a delegator and
// their own delegated rows. An admin sees every agent row.
const (
	whereAdmin  = "tenant_id = $1 AND agent_slug IS NOT NULL"
	whereOthers = "tenant_id = $1 AND agent_slug IS NOT NULL AND (delegator_id IS NULL OR delegator_id = $2)"
)

// where returns the predicate and its arguments, numbered from $1.
func (c usageCaller) where() (string, []any) {
	if c.Admin {
		return whereAdmin, []any{c.TenantID}
	}
	return whereOthers, []any{c.TenantID, c.Subject}
}

// scope adds the window and the optional single agent to the predicate.
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

// Queries run in exec mode so Postgres plans them with the actual window: the
// generic plan of a prepared statement reads most of the table through an index.
func withExecMode(args []any) []any { return append([]any{pgx.QueryExecModeExec}, args...) }

// AgentUsage returns usage per agent and model over the window, or for one agent.
func (r *PostgresInferenceUsageRepository) AgentUsage(ctx context.Context, q usageQuery) ([]agentUsageRow, error) {
	clause, args := q.scope(q.Window.Start, q.Window.End)
	rows, err := r.pool.Query(ctx, `
		SELECT agent_slug, model_id,
		       COALESCE(SUM(prompt_tokens), 0),
		       COALESCE(SUM(completion_tokens), 0),
		       COALESCE(SUM(cache_read_input_tokens), 0),
		       COALESCE(SUM(cache_creation_input_tokens), 0),
		       COALESCE(SUM(total_tokens), 0),
		       COALESCE(SUM(estimated_cost_usd), 0)
		FROM inference_usage
		WHERE `+clause+`
		GROUP BY agent_slug, model_id
		ORDER BY agent_slug, model_id`, withExecMode(args)...)
	if err != nil {
		return nil, fmt.Errorf("agent usage: %w", err)
	}
	defer rows.Close()

	var out []agentUsageRow
	for rows.Next() {
		var u agentUsageRow
		if err := rows.Scan(&u.AgentSlug, &u.ModelID, &u.PromptTokens, &u.CompletionTokens, &u.CacheReadTokens,
			&u.CacheCreationTokens, &u.TotalTokens, &u.EstimatedCostUSD); err != nil {
			return nil, fmt.Errorf("scan agent usage: %w", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// AgentSpendBuckets returns the previous window's total and the spend in each
// bucket of the window, as FILTER sums over one scan. A plain GROUP BY on the
// bucket was slower at every window size measured.
func (r *PostgresInferenceUsageRepository) AgentSpendBuckets(ctx context.Context, q usageQuery) (float64, []float64, error) {
	w := q.Window
	n := w.Buckets()
	clause, args := q.scope(w.PreviousStart(), w.End)

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
// monthStart, under the caller's predicate.
func (r *PostgresInferenceUsageRepository) AgentBudgetSpend(ctx context.Context, caller usageCaller, slugs []string, weekStart, monthStart time.Time) ([]AgentBudgetSpend, error) {
	clause, args := caller.where()
	n := len(args)
	args = append(args, slugs, weekStart, monthStart)
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`
		SELECT agent_slug,
		       COALESCE(SUM(estimated_cost_usd) FILTER (WHERE created_at >= $%[2]d), 0),
		       COALESCE(SUM(estimated_cost_usd) FILTER (WHERE created_at >= $%[3]d), 0)
		FROM inference_usage
		WHERE `+clause+`
		  AND agent_slug = ANY($%[1]d)
		  AND created_at >= LEAST($%[2]d::timestamptz, $%[3]d::timestamptz)
		GROUP BY agent_slug`, n+1, n+2, n+3), withExecMode(args)...)
	if err != nil {
		return nil, fmt.Errorf("agent budget spend: %w", err)
	}
	defer rows.Close()

	var out []AgentBudgetSpend
	for rows.Next() {
		var b AgentBudgetSpend
		if err := rows.Scan(&b.AgentSlug, &b.Week, &b.Month); err != nil {
			return nil, fmt.Errorf("scan agent budget spend: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
