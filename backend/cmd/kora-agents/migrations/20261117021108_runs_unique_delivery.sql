-- +goose Up
-- +goose StatementBegin
-- One run per trigger delivery: a repeated StartRun for the same (tenant, trigger, delivery)
-- resolves to the first run. Runs with no delivery id stay distinct (NULLs never collide).
-- Before uniqueness is added, a database that already holds duplicate runs for one delivery
-- keeps the oldest run (by created_at, then id) and loses only the delivery id on the others.
-- No run is deleted.
UPDATE runs r SET delivery_id = NULL
FROM runs keep
WHERE r.delivery_id IS NOT NULL
  AND keep.tenant_id = r.tenant_id
  AND keep.trigger_id = r.trigger_id
  AND keep.delivery_id = r.delivery_id
  AND (keep.created_at, keep.id) < (r.created_at, r.id);
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE runs ADD CONSTRAINT runs_trigger_delivery_key UNIQUE (tenant_id, trigger_id, delivery_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE runs DROP CONSTRAINT runs_trigger_delivery_key;
-- +goose StatementEnd
