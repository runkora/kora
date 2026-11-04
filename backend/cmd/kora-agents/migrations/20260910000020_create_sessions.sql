-- +goose Up
-- +goose StatementBegin
CREATE TABLE sessions (
    id TEXT NOT NULL,
    run_id TEXT NOT NULL,
    principal_id TEXT NOT NULL,
    connected_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    disconnected_at TIMESTAMPTZ,
    CONSTRAINT sessions_pkey PRIMARY KEY (id),
    CONSTRAINT sessions_run_id_fkey FOREIGN KEY (run_id) REFERENCES runs(id) ON DELETE CASCADE
);

CREATE INDEX idx_sessions_run_id ON sessions (run_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS sessions;
-- +goose StatementEnd
