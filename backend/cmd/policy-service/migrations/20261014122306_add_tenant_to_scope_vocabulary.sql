-- +goose Up
-- +goose StatementBegin
ALTER TABLE policy_scope_vocabulary ADD COLUMN tenant_id TEXT DEFAULT '' NOT NULL;
ALTER TABLE policy_scope_vocabulary DROP CONSTRAINT policy_scope_vocabulary_pkey;
ALTER TABLE policy_scope_vocabulary ADD CONSTRAINT policy_scope_vocabulary_pkey PRIMARY KEY (tenant_id, scope);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE policy_scope_vocabulary DROP CONSTRAINT policy_scope_vocabulary_pkey;
ALTER TABLE policy_scope_vocabulary ADD CONSTRAINT policy_scope_vocabulary_pkey PRIMARY KEY (scope);
ALTER TABLE policy_scope_vocabulary DROP COLUMN tenant_id;
-- +goose StatementEnd
