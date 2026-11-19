-- +goose Up
-- +goose StatementBegin
-- The grading pipeline's tables (docs/adr/2026-11-19-graders.md, Decision 8).

-- One snapshot per run: its uncapped Grader state. It has no foreign key to runs, so a
-- grade can be regraded after the run's events are gone.
CREATE TABLE grade_states (
    run_id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    state BYTEA NOT NULL,
    state_hash TEXT NOT NULL,
    size_bytes INTEGER NOT NULL,
    created_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    CONSTRAINT grade_states_pkey PRIMARY KEY (run_id)
);

-- The grading queue. A job grades one run for one Grader version. A regrade or recompute reads the
-- run from its grade under source_version. A recompute rescores that grade's answers with no model call.
CREATE TABLE grade_jobs (
    id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    grader_id TEXT NOT NULL,
    version TEXT NOT NULL,
    run_id TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('grade', 'regrade', 'recompute')),
    source_version TEXT,
    attempts INTEGER DEFAULT 0 NOT NULL,
    run_after TIMESTAMPTZ NOT NULL,
    last_error TEXT,
    created_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    CONSTRAINT grade_jobs_pkey PRIMARY KEY (id),
    CONSTRAINT grade_jobs_grader_version_run_key UNIQUE (grader_id, version, run_id),
    CONSTRAINT grade_jobs_grader_id_fkey FOREIGN KEY (grader_id) REFERENCES graders(id) ON DELETE CASCADE,
    CONSTRAINT grade_jobs_source_version CHECK ((kind = 'grade') = (source_version IS NULL))
);

CREATE INDEX idx_grade_jobs_run_after ON grade_jobs (run_after);

-- One grade per run per Grader version. It has no foreign key to graders or runs, so deleting
-- either keeps it. state_hash is NULL only when the run's state could not be built.
CREATE TABLE grades (
    id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    grader_id TEXT NOT NULL,
    version TEXT NOT NULL,
    run_id TEXT NOT NULL,
    agent_type_id TEXT NOT NULL,
    agent_config TEXT,
    run_outcome TEXT NOT NULL,
    run_started_at TIMESTAMPTZ NOT NULL,
    graded_at TIMESTAMPTZ NOT NULL,
    state_hash TEXT,
    -- The tool_result cap in characters. 0 is no bodies. NULL is not capped.
    state_cap INTEGER,
    input_tokens INTEGER,
    model_served TEXT,
    usage_id TEXT,
    status TEXT NOT NULL CHECK (status IN ('graded', 'not_applicable', 'applies_uncertain', 'too_large', 'error')),
    applies_raw JSONB,
    applies_p NUMERIC,
    verdict TEXT CHECK (verdict IN ('pass', 'fail', 'uncertain')),
    -- NULL when the model has no price.
    cost_usd NUMERIC,
    CONSTRAINT grades_pkey PRIMARY KEY (id),
    CONSTRAINT grades_grader_version_run_key UNIQUE (grader_id, version, run_id),
    CONSTRAINT grades_verdict_when_graded CHECK ((status = 'graded') = (verdict IS NOT NULL))
);

-- Serves the minPassRate window and the regrade selection.
CREATE INDEX idx_grades_window ON grades (grader_id, version, run_started_at DESC) WHERE status = 'graded';

-- One answer per question. raw is the answer as returned. p is NULL when skipped.
CREATE TABLE grade_answers (
    grade_id TEXT NOT NULL,
    question TEXT NOT NULL,
    raw JSONB NOT NULL,
    p NUMERIC,
    verdict TEXT NOT NULL CHECK (verdict IN ('pass', 'fail', 'uncertain', 'skipped')),
    CONSTRAINT grade_answers_pkey PRIMARY KEY (grade_id, question),
    CONSTRAINT grade_answers_grade_id_fkey FOREIGN KEY (grade_id) REFERENCES grades(id) ON DELETE CASCADE
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE grade_answers;
DROP TABLE grades;
DROP TABLE grade_jobs;
DROP TABLE grade_states;
-- +goose StatementEnd
