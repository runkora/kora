-- +goose Up
-- +goose StatementBegin
CREATE TABLE virtual_model_routes (
    id TEXT NOT NULL,
    virtual_model_id TEXT NOT NULL,
    provider TEXT NOT NULL,
    model_id TEXT NOT NULL,
    priority INTEGER DEFAULT 1 NOT NULL,
    weight INTEGER DEFAULT 0 NOT NULL,
    enabled BOOLEAN DEFAULT true NOT NULL,
    CONSTRAINT virtual_model_routes_pkey PRIMARY KEY (id),
    CONSTRAINT virtual_model_routes_virtual_model_id_provider_model_id_key UNIQUE (virtual_model_id, provider, model_id),
    CONSTRAINT virtual_model_routes_provider_check CHECK (provider IN ('bedrock', 'vertexai', 'azure') OR provider ~ '^bedrock:[a-z][a-z0-9_]*$'),
    CONSTRAINT virtual_model_routes_virtual_model_id_fkey FOREIGN KEY (virtual_model_id) REFERENCES virtual_models(id) ON DELETE CASCADE
);

CREATE INDEX idx_virtual_model_routes_vm_priority ON virtual_model_routes (virtual_model_id, priority);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS virtual_model_routes;
-- +goose StatementEnd
