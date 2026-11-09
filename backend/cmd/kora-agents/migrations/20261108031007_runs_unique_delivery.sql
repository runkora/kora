-- +goose Up
-- +goose StatementBegin
-- One run per trigger delivery: a repeated StartRun for the same (tenant, trigger, delivery)
-- resolves to the first run. Runs with no delivery id are outside the index.
CREATE UNIQUE INDEX idx_runs_trigger_delivery ON runs (tenant_id, trigger_id, delivery_id) WHERE delivery_id IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX idx_runs_trigger_delivery;
-- +goose StatementEnd
