-- +goose Up
-- +goose StatementBegin
CREATE TABLE virtual_models (
    id TEXT NOT NULL,
    family TEXT NOT NULL,
    name TEXT NOT NULL,
    display_name TEXT NOT NULL,
    description TEXT DEFAULT '' NOT NULL,
    strategy TEXT NOT NULL,
    enabled BOOLEAN DEFAULT true NOT NULL,
    created_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    CONSTRAINT virtual_models_pkey PRIMARY KEY (id),
    CONSTRAINT virtual_models_family_name_key UNIQUE (family, name),
    CONSTRAINT virtual_models_strategy_check CHECK (strategy IN ('priority', 'weighted', 'lowest_cost'))
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS virtual_models;
-- +goose StatementEnd
