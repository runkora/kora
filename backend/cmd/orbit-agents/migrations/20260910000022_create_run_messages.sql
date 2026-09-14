-- +goose Up
-- +goose StatementBegin
CREATE TABLE run_messages (
    id TEXT NOT NULL,
    run_id TEXT NOT NULL,
    sequence_no BIGSERIAL NOT NULL,
    content TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    CONSTRAINT run_messages_pkey PRIMARY KEY (id),
    CONSTRAINT run_messages_run_id_fkey FOREIGN KEY (run_id) REFERENCES runs(id) ON DELETE CASCADE
);

CREATE INDEX idx_run_messages_run_id ON run_messages (run_id, sequence_no);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS run_messages;
-- +goose StatementEnd
