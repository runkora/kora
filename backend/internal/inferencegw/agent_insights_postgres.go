package inferencegw

import (
	"context"
	"fmt"
	"time"

	"github.com/clear-route/orbit/internal/platform/usagewindow"
)

// The agent rollups read inference_usage through idx_inference_usage_agent_slug
// (tenant_id, agent_slug, created_at). Each is one query: tokens, models and
// buckets are separate aggregates over the same rows, run side by side by the
// service, and the previous window rides inside the bucket query as bucket -1.

// visibilityClause returns the SQL condition limiting rows to a scope, and its
// arguments, numbering placeholders from first. The delegator test is what
// keeps one person's delegated agent out of another person's totals.
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

	rows, err := r.pool.Query(ctx, q, append([]any{scope.TenantID, w.Start, w.End}, args...)...)
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

	rows, err := r.pool.Query(ctx, q, append([]any{scope.TenantID, w.Start, w.End}, args...)...)
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
// the total for the window before it. A row's bucket is the number of bucket
// widths between the window origin and the row; rows before the window start
// fall in bucket -1, the previous window.
func (r *PostgresInferenceUsageRepository) AgentSpendBuckets(ctx context.Context, scope AgentUsageScope, w usagewindow.Window) (SpendBuckets, error) {
	clause, args := scope.visibilityClause(7)
	q := `
		SELECT
		    CASE WHEN created_at < $2 THEN -1
		         ELSE floor(extract(epoch FROM created_at - $6::timestamptz) / $4)::int END AS bucket,
		    COALESCE(SUM(estimated_cost_usd), 0)
		FROM inference_usage
		WHERE tenant_id = $1
		  AND created_at >= $3 AND created_at < $5
		  AND ` + clause + `
		GROUP BY bucket`

	rows, err := r.pool.Query(ctx, q,
		append([]any{scope.TenantID, w.Start, w.PreviousStart(), int64(w.Bucket / time.Second), w.End, w.Origin}, args...)...)
	if err != nil {
		return SpendBuckets{}, fmt.Errorf("agent spend buckets: %w", err)
	}
	defer rows.Close()

	out := SpendBuckets{Buckets: make([]float64, w.Buckets())}
	for rows.Next() {
		var bucket int
		var cost float64
		if err := rows.Scan(&bucket, &cost); err != nil {
			return SpendBuckets{}, fmt.Errorf("scan agent spend bucket: %w", err)
		}
		switch {
		case bucket < 0:
			out.PreviousUSD = cost
		case bucket < len(out.Buckets):
			out.Buckets[bucket] = cost
		}
	}
	if err := rows.Err(); err != nil {
		return SpendBuckets{}, fmt.Errorf("iterate agent spend buckets: %w", err)
	}
	return out, nil
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

	rows, err := r.pool.Query(ctx, q, tenantID, slugs, weekStart, monthStart)
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
