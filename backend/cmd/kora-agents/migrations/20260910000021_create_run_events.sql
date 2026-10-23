-- +goose Up
-- +goose StatementBegin
CREATE TABLE run_events (
    sequence_no BIGSERIAL NOT NULL,
    run_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    payload JSONB DEFAULT '{}'::jsonb NOT NULL,
    created_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    CONSTRAINT run_events_pkey PRIMARY KEY (sequence_no),
    CONSTRAINT run_events_run_id_fkey FOREIGN KEY (run_id) REFERENCES runs(id) ON DELETE CASCADE
);

CREATE INDEX idx_run_events_run_id ON run_events (run_id, sequence_no);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS run_events;
-- +goose StatementEnd
