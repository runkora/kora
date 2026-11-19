package agents

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/runkora/kora/backend/internal/agents/grading"
)

// runGrades picks one grade per Grader of the agent: the current version's when it graded the
// run, and otherwise the one graded last. Grades of a deleted Grader are left out.
func (r *PostgresRepository) runGrades(ctx context.Context, tenantID, agentTypeID, runID string) ([]storedGrade, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT ON (g.name) gr.id, g.name, g.description, gr.version, v.spec, gr.status, gr.verdict, gr.applies_p::float8,
		       gr.state_cap, gr.input_tokens, gr.model_served, gr.cost_usd::float8, gr.graded_at
		FROM graders g
		JOIN grades gr ON gr.grader_id = g.id AND gr.run_id = $3 AND gr.tenant_id = g.tenant_id
		JOIN grader_versions v ON v.version = gr.version
		WHERE g.tenant_id = $1 AND g.agent_type_id = $2
		ORDER BY g.name, gr.version = g.current_version DESC, gr.graded_at DESC`, tenantID, agentTypeID, runID)
	if err != nil {
		return nil, fmt.Errorf("query run grades: %w", err)
	}
	var ids []string
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (storedGrade, error) {
		var g storedGrade
		var id string
		scanErr := row.Scan(&id, &g.Grader, &g.Description, &g.Version, &g.Spec, &g.Status, &g.Verdict, &g.AppliesP,
			&g.StateCap, &g.InputTokens, &g.ModelServed, &g.CostUSD, &g.GradedAt)
		ids = append(ids, id)
		return g, scanErr
	})
	if err != nil {
		return nil, fmt.Errorf("scan run grades: %w", err)
	}
	if len(ids) == 0 {
		return out, nil
	}
	arows, err := r.pool.Query(ctx, `SELECT grade_id, question, raw, p::float8, verdict FROM grade_answers WHERE grade_id = ANY($1) ORDER BY question`, ids)
	if err != nil {
		return nil, fmt.Errorf("query run grade answers: %w", err)
	}
	defer arows.Close()
	index := make(map[string]int, len(ids))
	for i, id := range ids {
		index[id] = i
	}
	for arows.Next() {
		var id string
		var a gradeAnswer
		if err := arows.Scan(&id, &a.Question, &a.Raw, &a.P, &a.Verdict); err != nil {
			return nil, fmt.Errorf("scan run grade answer: %w", err)
		}
		out[index[id]].Answers = append(out[index[id]].Answers, a)
	}
	return out, arows.Err()
}

// graderRowColumns reads a Grader with its current version spec and its current version's
// too_large count.
const graderRowColumns = `g.id, g.name, g.description, g.min_pass_rate, g.current_version, v.spec,
	(SELECT count(*) FROM grades t WHERE t.grader_id = g.id AND t.version = g.current_version AND t.status = 'too_large')`

// agentGraders returns the agent's Graders, each with its window.
func (r *PostgresRepository) agentGraders(ctx context.Context, tenantID, agentTypeID string) ([]storedGrader, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+graderRowColumns+` FROM graders g JOIN grader_versions v ON v.version = g.current_version
		WHERE g.tenant_id = $1 AND g.agent_type_id = $2 ORDER BY g.name`, tenantID, agentTypeID)
	if err != nil {
		return nil, fmt.Errorf("query graders: %w", err)
	}
	var ids []string
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (storedGrader, error) {
		g, id, scanErr := scanStoredGrader(row)
		ids = append(ids, id)
		return g, scanErr
	})
	if err != nil {
		return nil, fmt.Errorf("scan graders: %w", err)
	}
	for i := range out {
		if out[i].Window, err = r.graderWindow(ctx, ids[i], out[i].Version, false); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// agentGrader returns the named Grader of the agent with its window and the window's flagged answers.
func (r *PostgresRepository) agentGrader(ctx context.Context, tenantID, agentTypeID, name string) (*storedGrader, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+graderRowColumns+` FROM graders g JOIN grader_versions v ON v.version = g.current_version
		WHERE g.tenant_id = $1 AND g.agent_type_id = $2 AND g.name = $3`, tenantID, agentTypeID, name)
	g, id, err := scanStoredGrader(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get grader: %w", err)
	}
	if g.Window, err = r.graderWindow(ctx, id, g.Version, true); err != nil {
		return nil, err
	}
	return &g, nil
}

func scanStoredGrader(row pgx.Row) (storedGrader, string, error) {
	var g storedGrader
	var id string
	var minPassRate *int16
	if err := row.Scan(&id, &g.Name, &g.Description, &minPassRate, &g.Version, &g.Spec, &g.TooLarge); err != nil {
		return g, "", err
	}
	if minPassRate != nil {
		g.MinPassRate = new(int(*minPassRate))
	}
	return g, id, nil
}

// graderWindow returns the version's latest graded runs, newest first, read through
// idx_grades_window. With answers it adds each run's fail and uncertain answers and its title.
func (r *PostgresRepository) graderWindow(ctx context.Context, graderID, version string, answers bool) ([]windowRun, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT w.id, w.run_id, w.run_started_at, w.verdict, ru.summary,
		       COALESCE((SELECT jsonb_object_agg(a.question, a.verdict) FROM grade_answers a
		                 WHERE $4 AND a.grade_id = w.id AND a.verdict IN ('fail', 'uncertain')), '{}'::jsonb)
		FROM (SELECT id, run_id, run_started_at, verdict FROM grades
		      WHERE grader_id = $1 AND version = $2 AND status = 'graded'
		      ORDER BY run_started_at DESC, run_id DESC LIMIT $3) w
		LEFT JOIN runs ru ON ru.id = w.run_id AND $4
		ORDER BY w.run_started_at DESC, w.run_id DESC`, graderID, version, grading.PassRateWindow, answers)
	if err != nil {
		return nil, fmt.Errorf("query grader window: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (windowRun, error) {
		var w windowRun
		var id, verdict string
		scanErr := row.Scan(&id, &w.RunID, &w.StartedAt, &verdict, &w.Title, &w.Answers)
		w.Verdict = grading.Verdict(verdict)
		return w, scanErr
	})
	if err != nil {
		return nil, fmt.Errorf("scan grader window: %w", err)
	}
	return out, nil
}
