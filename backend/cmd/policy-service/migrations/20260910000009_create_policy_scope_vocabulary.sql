-- +goose Up
-- +goose StatementBegin
CREATE TABLE policy_scope_vocabulary (
    scope TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    path_segments JSONB DEFAULT '[]'::jsonb NOT NULL,
    action TEXT NOT NULL,
    description TEXT DEFAULT '' NOT NULL,
    created_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    registered_by TEXT,
    requires TEXT[] DEFAULT '{}' NOT NULL,
    CONSTRAINT policy_scope_vocabulary_pkey PRIMARY KEY (scope)
);

CREATE INDEX idx_policy_scope_vocabulary_resource_type ON policy_scope_vocabulary (resource_type);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS policy_scope_vocabulary;
-- +goose StatementEnd
