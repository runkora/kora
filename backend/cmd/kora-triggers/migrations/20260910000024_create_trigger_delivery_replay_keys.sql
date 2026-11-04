-- +goose Up
-- +goose StatementBegin
CREATE TABLE trigger_delivery_replay_keys (
    source_id TEXT NOT NULL,
    body_hash BYTEA NOT NULL,
    provider_delivery_id TEXT NOT NULL,
    seen_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    CONSTRAINT trigger_delivery_replay_keys_pkey PRIMARY KEY (source_id, body_hash)
);

CREATE INDEX idx_trigger_replay_keys_seen ON trigger_delivery_replay_keys (seen_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS trigger_delivery_replay_keys;
-- +goose StatementEnd
