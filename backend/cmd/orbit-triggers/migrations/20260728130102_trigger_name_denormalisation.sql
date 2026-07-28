-- +goose Up
-- +goose StatementBegin

-- Trigger names captured onto delivery history (US-004 AC4).
--
-- Deliveries deliberately carry no foreign key to triggers, so that history
-- survives the trigger being deleted. The cost, until now, was that the console
-- had nothing but a trigger id to show once the trigger was gone: the row
-- outlived the trigger and became unreadable at exactly the moment someone was
-- most likely to be asking what had happened.
--
-- The name is captured at write time rather than joined at read time. A join
-- returns NULL for a deleted trigger, which is the case that matters; and a
-- later rename would silently rewrite history, showing a delivery as having been
-- handled by a trigger under a name it did not have at the time. This column is
-- the name AS IT WAS, which is what an audit record should hold.
ALTER TABLE trigger_deliveries
    ADD COLUMN trigger_name TEXT;

ALTER TABLE trigger_delivery_runs
    ADD COLUMN trigger_name TEXT;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE trigger_delivery_runs
    DROP COLUMN IF EXISTS trigger_name;

ALTER TABLE trigger_deliveries
    DROP COLUMN IF EXISTS trigger_name;

-- +goose StatementEnd
