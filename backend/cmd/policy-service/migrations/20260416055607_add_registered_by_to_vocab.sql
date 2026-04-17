-- +goose Up
-- +goose StatementBegin

-- Add registered_by to track which service owns each vocabulary scope.
-- NULL means the scope was created manually via API.
-- Non-NULL (e.g. "inference-gateway") means the scope is service-owned and
-- will be reconciled (pruned) on re-registration when models are deleted.

ALTER TABLE policy_scope_vocabulary
    ADD COLUMN IF NOT EXISTS registered_by TEXT;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE policy_scope_vocabulary
    DROP COLUMN IF EXISTS registered_by;

-- +goose StatementEnd
