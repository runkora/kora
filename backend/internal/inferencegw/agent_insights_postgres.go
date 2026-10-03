package inferencegw

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/clear-route/orbit/internal/platform/usagewindow"
)

// The agent rollups read inference_usage through idx_inference_usage_agent_slug
// (tenant_id, agent_slug, created_at). Each is one query: tokens, models and
// buckets are separate aggregates over the same rows, run side by side by the
// service, and the previous window rides inside the bucket query as bucket -1.

// visibilityClause returns the SQL condition limiting rows to a scope, and its
// arguments, numbering placeholders from first. The delegator test is what
// keeps one person's delegated agent out of another person's totals.
//
// Every query here runs with pgx.QueryExecModeExec, so Postgres plans it with the
// actual slugs and window. A prepared statement is planned once without them,
// and its generic plan picks an index scan that reads most of the table through
// the index (about 2 seconds for a 90d window at 150 agents against about 0.3
// for the plan chosen with the real values).
func (s AgentUsageScope) visibilityClause(first int) (string, []any) {
	all := s.AllRows
	if all == nil {
		all = []string{}
	}
	if len(s.OwnRows) == 0 {
		return fmt.Sprintf("agent_slug = ANY($%d)", first), []any{all}
	}
	clause := fmt.Sprintf("(agent_slug = ANY($%d) OR (agent_slug = ANY($%d) AND delegator_id = $%d))", first, first+1, first+2)
	return clause, []any{all, s.OwnRows, s.Subject}
}

// AgentUsage returns tokens and cost per visible agent over [w.Start, w.End).
// Agents with no usage in the window are absent.
func (r *PostgresInferenceUsageRepository) AgentUsage(ctx context.Context, scope AgentUsageScope, w usagewindow.Window) ([]AgentTypeUsage, error) {
	clause, args := scope.visibilityClause(4)
	q := `
		SELECT
		    agent_slug,
		    COALESCE(SUM(prompt_tokens), 0),
		    COALESCE(SUM(completion_tokens), 0),
		    COALESCE(SUM(cache_read_input_tokens), 0),
		    COALESCE(SUM(cache_creation_input_tokens), 0),
		    COALESCE(SUM(total_tokens), 0),
		    COALESCE(SUM(estimated_cost_usd), 0)
		FROM inference_usage
		WHERE tenant_id = $1
		  AND created_at >= $2 AND created_at < $3
		  AND ` + clause + `
		GROUP BY agent_slug
		ORDER BY agent_slug`

	rows, err := r.pool.Query(ctx, q, append([]any{pgx.QueryExecModeExec, scope.TenantID, w.Start, w.End}, args...)...)
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
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agent usage: %w", err)
	}
	return out, nil
}

// AgentSpendByModel returns the window's spend per model, largest first.
func (r *PostgresInferenceUsageRepository) AgentSpendByModel(ctx context.Context, scope AgentUsageScope, w usagewindow.Window) ([]ModelSpend, error) {
	clause, args := scope.visibilityClause(4)
	q := `
		SELECT model_id, COALESCE(SUM(estimated_cost_usd), 0) AS cost
		FROM inference_usage
		WHERE tenant_id = $1
		  AND created_at >= $2 AND created_at < $3
		  AND ` + clause + `
		GROUP BY model_id
		HAVING COALESCE(SUM(estimated_cost_usd), 0) > 0
		ORDER BY cost DESC, model_id`

	rows, err := r.pool.Query(ctx, q, append([]any{pgx.QueryExecModeExec, scope.TenantID, w.Start, w.End}, args...)...)
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
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agent spend by model: %w", err)
	}
	return out, nil
}

// AgentSpendBuckets returns the spend in each chart bucket of the window and
// the total for the window before it. Each bucket is one FILTER sum over the
// same rows, so the query has no GROUP BY and Postgres runs it as a parallel
// aggregate. Bucket i covers [bounds[i], bounds[i+1]), where bounds[0] is the
// window start and the last bound is the window end; the previous window is
// everything before the start.
func (r *PostgresInferenceUsageRepository) AgentSpendBuckets(ctx context.Context, scope AgentUsageScope, w usagewindow.Window) (SpendBuckets, error) {
	n := w.Buckets()
	bounds := make([]time.Time, n+1)
	bounds[0], bounds[n] = w.Start, w.End
	for i := 1; i < n; i++ {
		bounds[i] = w.Origin.Add(time.Duration(i) * w.Bucket)
	}

	// $1 tenant, $2 previous start, $3 end, $4..$(4+n) the bounds, then the visibility arguments.
	cols := []string{"COALESCE(SUM(estimated_cost_usd) FILTER (WHERE created_at < $4), 0)"}
	args := []any{scope.TenantID, w.PreviousStart(), w.End}
	for i, b := range bounds {
		args = append(args, b)
		if i < n {
			cols = append(cols, fmt.Sprintf("COALESCE(SUM(estimated_cost_usd) FILTER (WHERE created_at >= $%d AND created_at < $%d), 0)", 4+i, 5+i))
		}
	}
	clause, visArgs := scope.visibilityClause(4 + len(bounds))
	q := "SELECT " + strings.Join(cols, ", ") + `
		FROM inference_usage
		WHERE tenant_id = $1
		  AND created_at >= $2 AND created_at < $3
		  AND ` + clause

	totals := make([]float64, n+1)
	dest := make([]any, n+1)
	for i := range totals {
		dest[i] = &totals[i]
	}
	if err := r.pool.QueryRow(ctx, q, append(append([]any{pgx.QueryExecModeExec}, args...), visArgs...)...).Scan(dest...); err != nil {
		return SpendBuckets{}, fmt.Errorf("agent spend buckets: %w", err)
	}
	return SpendBuckets{PreviousUSD: totals[0], Buckets: totals[1:]}, nil
}

// AgentBudgetSpend returns each listed agent's spend since weekStart and since
// monthStart. Agents with no spend since the earlier of the two are absent.
func (r *PostgresInferenceUsageRepository) AgentBudgetSpend(ctx context.Context, tenantID string, slugs []string, weekStart, monthStart time.Time) ([]AgentBudgetSpend, error) {
	const q = `
		SELECT
		    agent_slug,
		    COALESCE(SUM(estimated_cost_usd) FILTER (WHERE created_at >= $3), 0),
		    COALESCE(SUM(estimated_cost_usd) FILTER (WHERE created_at >= $4), 0)
		FROM inference_usage
		WHERE tenant_id = $1
		  AND agent_slug = ANY($2)
		  AND created_at >= LEAST($3::timestamptz, $4::timestamptz)
		GROUP BY agent_slug`

	rows, err := r.pool.Query(ctx, q, pgx.QueryExecModeExec, tenantID, slugs, weekStart, monthStart)
	if err != nil {
		return nil, fmt.Errorf("agent budget spend: %w", err)
	}
	defer rows.Close()

	var out []AgentBudgetSpend
	for rows.Next() {
		var b AgentBudgetSpend
		if err := rows.Scan(&b.AgentSlug, &b.WeekToDateUSD, &b.MonthToDateUSD); err != nil {
			return nil, fmt.Errorf("scan agent budget spend: %w", err)
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agent budget spend: %w", err)
	}
	return out, nil
}
