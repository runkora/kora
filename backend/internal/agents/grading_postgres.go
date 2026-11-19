package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/runkora/kora/backend/internal/agents/grading"
	"github.com/runkora/kora/backend/internal/platform/storage"
)

// The grading pipeline's storage (docs/adr/2026-11-19-graders.md, Decisions 3, 8 and 9).

// Grade job kinds. A grade is a run's first grade. A regrade calls the model again from the
// stored snapshot. A recompute rescores stored answers under new thresholds.
const (
	gradeJobGrade     = "grade"
	gradeJobRegrade   = "regrade"
	gradeJobRecompute = "recompute"
)

// Grade statuses the pipeline adds to those grading.Score returns.
const (
	gradeStatusTooLarge = "too_large"
	gradeStatusError    = "error"
)

// gradeJob is one claimed grade_jobs row. SourceVersion is the version whose grade a regrade
// or recompute reads, and is empty for a grade.
type gradeJob struct {
	ID            string
	TenantID      string
	GraderID      string
	Version       string
	RunID         string
	Kind          string
	SourceVersion string
	// Attempts counts this claim.
	Attempts  int
	CreatedAt time.Time
}

// gradeRecord is one grades row with its answers.
type gradeRecord struct {
	TenantID     string
	GraderID     string
	Version      string
	RunID        string
	AgentTypeID  string
	AgentConfig  *string
	RunOutcome   string
	RunStartedAt time.Time
	StateHash    *string
	StateCap     *int
	InputTokens  *int64
	ModelServed  *string
	UsageID      *string
	Status       string
	AppliesRaw   json.RawMessage
	AppliesP     *float64
	Verdict      *string
	CostUSD      *float64
	// SourceVersion is the version whose grade a regrade or recompute read. Empty on a first grade.
	SourceVersion string
	Answers       []gradeAnswer
}

// gradeAnswer is one grade_answers row. P is nil when the question was skipped.
type gradeAnswer struct {
	Question string
	Raw      json.RawMessage
	P        *float64
	Verdict  string
}

// regradeWindow is how many of a Grader's latest graded runs a new version regrades.
const regradeWindow = grading.PassRateWindow

// enqueueGradeJobs queues a grade job per Grader of each run's agent. Run-end paths call it in
// their transaction, so a job exists exactly when its run's end commits. A job or grade already
// held for the run and version keeps the insert from adding another.
func (r *PostgresRepository) enqueueGradeJobs(ctx context.Context, runIDs ...string) error {
	if len(runIDs) == 0 {
		return nil
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO grade_jobs (id, tenant_id, grader_id, version, run_id, kind, run_after)
		SELECT gen_random_uuid()::text, g.tenant_id, g.id, g.current_version, r.id, 'grade', now()
		FROM runs r JOIN graders g ON g.agent_type_id = r.agent_type_id AND g.tenant_id = r.tenant_id
		WHERE r.id = ANY($1)
		  AND NOT EXISTS (SELECT 1 FROM grades x WHERE x.grader_id = g.id AND x.version = g.current_version AND x.run_id = r.id)
		-- A version change waits for this transaction, so it sees and moves the jobs queued here.
		FOR SHARE OF g
		ON CONFLICT (grader_id, version, run_id) DO NOTHING`, runIDs)
	if err != nil {
		return fmt.Errorf("enqueue grade jobs: %w", err)
	}
	return nil
}

// enqueueRegrade queues the new version's jobs for the Grader's latest graded runs on the old
// version, and moves queued first grades to the new version. It runs in PutGrader's transaction.
func (r *PostgresRepository) enqueueRegrade(ctx context.Context, graderID, from, to string, fromSpec, toSpec []byte) error {
	kind, err := regradeKind(fromSpec, toSpec)
	if err != nil {
		return err
	}
	if _, err := r.pool.Exec(ctx, `
		UPDATE grade_jobs j SET version = $3
		WHERE j.grader_id = $1 AND j.version = $2 AND j.kind = 'grade'
		  AND NOT EXISTS (SELECT 1 FROM grade_jobs x WHERE x.grader_id = $1 AND x.version = $3 AND x.run_id = j.run_id)`,
		graderID, from, to); err != nil {
		return fmt.Errorf("move queued grades: %w", err)
	}
	if err := r.retargetRegrades(ctx, graderID, from, to, toSpec); err != nil {
		return err
	}
	if _, err := r.pool.Exec(ctx, `
		INSERT INTO grade_jobs (id, tenant_id, grader_id, version, run_id, kind, source_version, run_after)
		SELECT gen_random_uuid()::text, s.tenant_id, s.grader_id, $3, s.run_id, $4, $2, now()
		FROM (SELECT tenant_id, grader_id, run_id FROM grades
		      WHERE grader_id = $1 AND version = $2 AND status = 'graded'
		      ORDER BY run_started_at DESC LIMIT $5) s
		WHERE NOT EXISTS (SELECT 1 FROM grades x WHERE x.grader_id = $1 AND x.version = $3 AND x.run_id = s.run_id)
		ON CONFLICT (grader_id, version, run_id) DO NOTHING`,
		graderID, from, to, kind, regradeWindow); err != nil {
		return fmt.Errorf("enqueue regrade: %w", err)
	}
	return nil
}

// retargetRegrades moves the old version's queued regrades and recomputes to the new version.
// Each keeps its source grade, and its kind is decided again against the new version. A
// superseded version's regrade would call the model for grades nobody reads.
func (r *PostgresRepository) retargetRegrades(ctx context.Context, graderID, from, to string, toSpec []byte) error {
	rows, err := r.pool.Query(ctx, `SELECT DISTINCT source_version FROM grade_jobs WHERE grader_id = $1 AND version = $2 AND kind <> 'grade'`, graderID, from)
	if err != nil {
		return fmt.Errorf("list queued regrades: %w", err)
	}
	sources, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return fmt.Errorf("list queued regrades: %w", err)
	}
	for _, src := range sources {
		srcSpec, err := r.graderVersionSpec(ctx, src)
		if err != nil {
			return err
		}
		kind, err := regradeKind(srcSpec, toSpec)
		if err != nil {
			return err
		}
		if _, err := r.pool.Exec(ctx, `
			UPDATE grade_jobs j SET version = $3, kind = $5
			WHERE j.grader_id = $1 AND j.version = $2 AND j.source_version = $4
			  AND NOT EXISTS (SELECT 1 FROM grade_jobs x WHERE x.grader_id = $1 AND x.version = $3 AND x.run_id = j.run_id)
			  AND NOT EXISTS (SELECT 1 FROM grades x WHERE x.grader_id = $1 AND x.version = $3 AND x.run_id = j.run_id)`,
			graderID, from, to, src, kind); err != nil {
			return fmt.Errorf("move queued regrades: %w", err)
		}
	}
	if _, err := r.pool.Exec(ctx, `DELETE FROM grade_jobs WHERE grader_id = $1 AND version = $2 AND kind <> 'grade'`, graderID, from); err != nil {
		return fmt.Errorf("drop superseded regrades: %w", err)
	}
	return nil
}

// regradeKind is recompute when the two versions differ only in thresholds, and regrade otherwise.
func regradeKind(fromSpec, toSpec []byte) (string, error) {
	from, err := parseGraderVersion(fromSpec)
	if err != nil {
		return "", err
	}
	to, err := parseGraderVersion(toSpec)
	if err != nil {
		return "", err
	}
	a, err := thresholdless(from)
	if err != nil {
		return "", err
	}
	b, err := thresholdless(to)
	if err != nil {
		return "", err
	}
	if string(a) == string(b) {
		return gradeJobRecompute, nil
	}
	return gradeJobRegrade, nil
}

// claimGradeJobs claims up to limit due jobs with SKIP LOCKED. A claim counts an attempt and
// holds the job for lease, so a job whose worker died is claimed again after it.
func (r *PostgresRepository) claimGradeJobs(ctx context.Context, limit int, now time.Time, lease time.Duration) ([]gradeJob, error) {
	rows, err := r.pool.Query(ctx, `
		UPDATE grade_jobs SET attempts = attempts + 1, run_after = $3
		WHERE id IN (SELECT id FROM grade_jobs WHERE run_after <= $2 ORDER BY run_after LIMIT $1 FOR UPDATE SKIP LOCKED)
		RETURNING id, tenant_id, grader_id, version, run_id, kind, COALESCE(source_version, ''), attempts, created_at`,
		limit, now, now.Add(lease))
	if err != nil {
		return nil, fmt.Errorf("claim grade jobs: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (gradeJob, error) {
		var j gradeJob
		scanErr := row.Scan(&j.ID, &j.TenantID, &j.GraderID, &j.Version, &j.RunID, &j.Kind, &j.SourceVersion, &j.Attempts, &j.CreatedAt)
		return j, scanErr
	})
}

// retryGradeJob returns a claimed job to the queue at at, with the error that failed it.
func (r *PostgresRepository) retryGradeJob(ctx context.Context, id string, at time.Time, lastError string) error {
	if _, err := r.pool.Exec(ctx, `UPDATE grade_jobs SET run_after = $2, last_error = $3 WHERE id = $1`, id, at, lastError); err != nil {
		return fmt.Errorf("retry grade job: %w", err)
	}
	return nil
}

// deferGradeJob returns a claimed job to the queue at at without counting the claim as an attempt.
func (r *PostgresRepository) deferGradeJob(ctx context.Context, id string, at time.Time) error {
	if _, err := r.pool.Exec(ctx, `UPDATE grade_jobs SET run_after = $2, attempts = attempts - 1 WHERE id = $1`, id, at); err != nil {
		return fmt.Errorf("defer grade job: %w", err)
	}
	return nil
}

// dropGradeJob deletes a job that can never be graded.
func (r *PostgresRepository) dropGradeJob(ctx context.Context, id string) error {
	if _, err := r.pool.Exec(ctx, `DELETE FROM grade_jobs WHERE id = $1`, id); err != nil {
		return fmt.Errorf("drop grade job: %w", err)
	}
	return nil
}

// graderVersionSpec returns a stored version's spec.
func (r *PostgresRepository) graderVersionSpec(ctx context.Context, version string) ([]byte, error) {
	var spec []byte
	err := r.pool.QueryRow(ctx, `SELECT spec FROM grader_versions WHERE version = $1`, version).Scan(&spec)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get grader version: %w", err)
	}
	return spec, nil
}

// runEndRecorded reports whether the run's terminal run_state event is written. Transition
// writes it after the run-end transaction commits, so a job can be claimed before it.
func (r *PostgresRepository) runEndRecorded(ctx context.Context, runID string) (bool, error) {
	var ok bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM run_events WHERE run_id = $1 AND event_type = 'run_state'
		               AND payload->>'state' = ANY($2))`, runID, stateStrings(terminalRunStates())).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("read run end: %w", err)
	}
	return ok, nil
}

// graderModelScopes is the use scope of each model a tenant's Graders ask at their current
// version. It is the whole set the grading principal's grant may hold.
func (r *PostgresRepository) graderModelScopes(ctx context.Context, tenantID string) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT 'inference:' || (v.spec->'model'->>'family') || ':' || (v.spec->'model'->>'name') || ':use'
		FROM graders g JOIN grader_versions v ON v.version = g.current_version
		WHERE g.tenant_id = $1 ORDER BY 1`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list grader models: %w", err)
	}
	scopes, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("list grader models: %w", err)
	}
	return scopes, nil
}

// terminalRunStates are the states a run ends in.
func terminalRunStates() []RunState {
	return []RunState{RunStateCompleted, RunStateFailed, RunStateBudgetExhausted, RunStateTerminated}
}

// gradeState returns the run's stored snapshot, or nil when none is stored.
func (r *PostgresRepository) gradeState(ctx context.Context, runID string) (*grading.Snapshot, error) {
	var s grading.Snapshot
	err := r.pool.QueryRow(ctx, `SELECT state, state_hash FROM grade_states WHERE run_id = $1`, runID).Scan(&s.JSON, &s.Hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get grade state: %w", err)
	}
	return &s, nil
}

// putGradeState stores the run's snapshot once and returns the stored one. Every Grader of
// the run then reads the same state.
func (r *PostgresRepository) putGradeState(ctx context.Context, tenantID, runID string, s grading.Snapshot) (grading.Snapshot, error) {
	if _, err := r.pool.Exec(ctx, `
		INSERT INTO grade_states (run_id, tenant_id, state, state_hash, size_bytes) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (run_id) DO NOTHING`, runID, tenantID, s.JSON, s.Hash, len(s.JSON)); err != nil {
		return grading.Snapshot{}, fmt.Errorf("put grade state: %w", err)
	}
	stored, err := r.gradeState(ctx, runID)
	if err != nil {
		return grading.Snapshot{}, err
	}
	if stored == nil {
		return grading.Snapshot{}, fmt.Errorf("put grade state: %w", ErrNotFound)
	}
	return *stored, nil
}

const gradeColumns = `tenant_id, grader_id, version, run_id, agent_type_id, agent_config, run_outcome, run_started_at,
	state_hash, state_cap, input_tokens, model_served, usage_id, status, applies_raw, applies_p, verdict, cost_usd, source_version`

// getGrade returns a run's grade on one Grader version with its answers.
func (r *PostgresRepository) getGrade(ctx context.Context, graderID, version, runID string) (*gradeRecord, error) {
	var g gradeRecord
	var id string
	var sourceVersion *string
	err := r.pool.QueryRow(ctx, `SELECT id, `+gradeColumns+` FROM grades WHERE grader_id = $1 AND version = $2 AND run_id = $3`,
		graderID, version, runID).Scan(&id, &g.TenantID, &g.GraderID, &g.Version, &g.RunID, &g.AgentTypeID, &g.AgentConfig,
		&g.RunOutcome, &g.RunStartedAt, &g.StateHash, &g.StateCap, &g.InputTokens, &g.ModelServed, &g.UsageID, &g.Status,
		&g.AppliesRaw, &g.AppliesP, &g.Verdict, &g.CostUSD, &sourceVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get grade: %w", err)
	}
	if sourceVersion != nil {
		g.SourceVersion = *sourceVersion
	}
	rows, err := r.pool.Query(ctx, `SELECT question, raw, p, verdict FROM grade_answers WHERE grade_id = $1 ORDER BY question`, id)
	if err != nil {
		return nil, fmt.Errorf("get grade answers: %w", err)
	}
	g.Answers, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (gradeAnswer, error) {
		var a gradeAnswer
		scanErr := row.Scan(&a.Question, &a.Raw, &a.P, &a.Verdict)
		return a, scanErr
	})
	if err != nil {
		return nil, fmt.Errorf("get grade answers: %w", err)
	}
	return &g, nil
}

// errGradeJobGone reports a job that was moved to another version, deleted with its Grader or
// finished by another worker while this one graded it. Its grade is not written.
var errGradeJobGone = errors.New("grade job is no longer held at its version")

// finishGradeJob deletes the job and writes its grade and answers in one transaction. It writes
// nothing when the job is no longer queued at g's version. A grade already held for the run
// and version is replaced, and its graded_at moves.
func (r *PostgresRepository) finishGradeJob(ctx context.Context, jobID string, g gradeRecord) error {
	return storage.WithTransaction(ctx, r.pool, func(ctx context.Context, _ pgx.Tx) error {
		tag, err := r.pool.Exec(ctx, `DELETE FROM grade_jobs WHERE id = $1 AND version = $2`, jobID, g.Version)
		if err != nil {
			return fmt.Errorf("delete grade job: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return errGradeJobGone
		}
		var id string
		err = r.pool.QueryRow(ctx, `
			INSERT INTO grades (id, graded_at, `+gradeColumns+`)
			VALUES ($1, now(), $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, NULLIF($20, ''))
			ON CONFLICT (grader_id, version, run_id) DO UPDATE SET
				graded_at = now(), agent_type_id = EXCLUDED.agent_type_id, agent_config = EXCLUDED.agent_config,
				run_outcome = EXCLUDED.run_outcome, run_started_at = EXCLUDED.run_started_at, state_hash = EXCLUDED.state_hash,
				state_cap = EXCLUDED.state_cap, input_tokens = EXCLUDED.input_tokens, model_served = EXCLUDED.model_served,
				usage_id = EXCLUDED.usage_id, status = EXCLUDED.status, applies_raw = EXCLUDED.applies_raw,
				applies_p = EXCLUDED.applies_p, verdict = EXCLUDED.verdict, cost_usd = EXCLUDED.cost_usd,
				source_version = EXCLUDED.source_version
			RETURNING id`,
			uuid.NewString(), g.TenantID, g.GraderID, g.Version, g.RunID, g.AgentTypeID, g.AgentConfig, g.RunOutcome,
			g.RunStartedAt, g.StateHash, g.StateCap, g.InputTokens, g.ModelServed, g.UsageID, g.Status, nullJSON(g.AppliesRaw),
			g.AppliesP, g.Verdict, g.CostUSD, g.SourceVersion).Scan(&id)
		if err != nil {
			return fmt.Errorf("write grade: %w", err)
		}
		if _, err := r.pool.Exec(ctx, `DELETE FROM grade_answers WHERE grade_id = $1`, id); err != nil {
			return fmt.Errorf("clear grade answers: %w", err)
		}
		for _, a := range g.Answers {
			if _, err := r.pool.Exec(ctx, `INSERT INTO grade_answers (grade_id, question, raw, p, verdict) VALUES ($1, $2, $3, $4, $5)`,
				id, a.Question, a.Raw, a.P, a.Verdict); err != nil {
				return fmt.Errorf("write grade answer %s: %w", a.Question, err)
			}
		}
		return nil
	})
}

// nullJSON is SQL NULL for an empty raw message.
func nullJSON(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return raw
}
