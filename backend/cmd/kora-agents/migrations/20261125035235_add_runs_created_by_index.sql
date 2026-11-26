-- +goose Up
-- +goose StatementBegin
-- The run list's created_by=me filter reads a person's runs newest first
-- (docs/adr/2026-11-25-runs-list-live-stream.md, Decision 5).
CREATE INDEX idx_runs_tenant_created_by ON runs (tenant_id, created_by, created_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_runs_tenant_created_by;
-- +goose StatementEnd
