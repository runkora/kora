-- +goose Up
-- agent_slug is the agent segment of an AGENT principal (spiffe://{td}/tenant/{ns}/agent/{slug}),
-- stored at write time so per-agent aggregates filter on an indexed column instead of parsing identity.
-- +goose StatementBegin
ALTER TABLE inference_usage ADD COLUMN IF NOT EXISTS agent_slug TEXT;
-- +goose StatementEnd

-- +goose StatementBegin
UPDATE inference_usage
SET agent_slug = split_part(principal_id, '/', 7)
WHERE agent_slug IS NULL
  AND principal_type = 'AGENT'
  AND starts_with(principal_id, 'spiffe://')
  AND split_part(principal_id, '/', 4) = 'tenant'
  AND split_part(principal_id, '/', 6) = 'agent'
  AND split_part(principal_id, '/', 7) <> '';
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_inference_usage_agent_slug
    ON inference_usage (tenant_id, agent_slug, created_at DESC)
    WHERE agent_slug IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_inference_usage_agent_slug;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE inference_usage DROP COLUMN IF EXISTS agent_slug;
-- +goose StatementEnd
