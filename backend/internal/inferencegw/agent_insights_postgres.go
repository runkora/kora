package inferencegw

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/clear-route/orbit/internal/platform/usagewindow"
)

// agentTraffic is the one predicate behind every agent rollup: the tenant's
// rows that came from an agent. $1 is the tenant.
const agentTraffic = "tenant_id = $1 AND agent_slug IS NOT NULL"

// Rollups run in exec mode so Postgres plans them with the actual window: the
// generic plan of a prepared statement reads most of the table through an index.
func execMode(args ...any) []any { return append([]any{pgx.QueryExecModeExec}, args...) }

// AgentUsage returns usage per agent and model over the window, or for one agent when slug is set.
func (r *PostgresInferenceUsageRepository) AgentUsage(ctx context.Context, tenantID string, w usagewindow.Window, slug string) ([]AgentModelUsage, error) {
	where, args := agentTraffic+" AND created_at >= $2 AND created_at < $3", []any{tenantID, w.Start, w.End}
	if slug != "" {
		where += " AND agent_slug = $4"
		args = append(args, slug)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT agent_slug, model_id,
		       COALESCE(SUM(prompt_tokens), 0),
		       COALESCE(SUM(completion_tokens), 0),
		       COALESCE(SUM(cache_read_input_tokens), 0),
		       COALESCE(SUM(cache_creation_input_tokens), 0),
		       COALESCE(SUM(total_tokens), 0),
		       COALESCE(SUM(estimated_cost_usd), 0)
		FROM inference_usage
		WHERE `+where+`
		GROUP BY agent_slug, model_id
		ORDER BY agent_slug, model_id`, execMode(args...)...)
	if err != nil {
		return nil, fmt.Errorf("agent usage: %w", err)
	}
	defer rows.Close()

	var out []AgentModelUsage
	for rows.Next() {
		var u AgentModelUsage
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
func (r *PostgresInferenceUsageRepository) AgentSpendBuckets(ctx context.Context, tenantID string, w usagewindow.Window) (float64, []float64, error) {
	n := int(w.End.Sub(w.Start) / w.Bucket)
	previousStart := w.Start.Add(-w.End.Sub(w.Start))
	args := []any{tenantID, previousStart, w.End, w.Start}
	cols := []string{"COALESCE(SUM(estimated_cost_usd) FILTER (WHERE created_at < $4), 0)"}
	for i := range n {
		args = append(args, w.Start.Add(time.Duration(i)*w.Bucket), w.Start.Add(time.Duration(i+1)*w.Bucket))
		cols = append(cols, fmt.Sprintf("COALESCE(SUM(estimated_cost_usd) FILTER (WHERE created_at >= $%d AND created_at < $%d), 0)", len(args)-1, len(args)))
	}

	vals := make([]float64, n+1)
	dest := make([]any, n+1)
	for i := range vals {
		dest[i] = &vals[i]
	}
	row := r.pool.QueryRow(ctx, "SELECT "+strings.Join(cols, ", ")+
		" FROM inference_usage WHERE "+agentTraffic+" AND created_at >= $2 AND created_at < $3", execMode(args...)...)
	if err := row.Scan(dest...); err != nil {
		return 0, nil, fmt.Errorf("agent spend buckets: %w", err)
	}
	return vals[0], vals[1:], nil
}

// AgentBudgetSpend returns each listed agent's spend since weekStart and since monthStart.
func (r *PostgresInferenceUsageRepository) AgentBudgetSpend(ctx context.Context, tenantID string, slugs []string, weekStart, monthStart time.Time) ([]AgentBudgetSpend, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT agent_slug,
		       COALESCE(SUM(estimated_cost_usd) FILTER (WHERE created_at >= $3), 0),
		       COALESCE(SUM(estimated_cost_usd) FILTER (WHERE created_at >= $4), 0)
		FROM inference_usage
		WHERE `+agentTraffic+`
		  AND agent_slug = ANY($2)
		  AND created_at >= LEAST($3::timestamptz, $4::timestamptz)
		GROUP BY agent_slug`, execMode(tenantID, slugs, weekStart, monthStart)...)
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
