-- +goose Up
-- +goose StatementBegin

-- ─── Replay defense for the unauthenticated ingest path ───────────────────────
-- Dedup keys on the provider's delivery id, and for GitHub that id travels in an
-- UNSIGNED header. A captured, correctly-signed body therefore replays forever:
-- rewrite X-GitHub-Delivery each time and every copy is a fresh, verified
-- arrival that starts a run.
--
-- The fingerprint that cannot be rewritten is the body itself. The signature is
-- a deterministic function of (secret, body), so a hash of the body identifies
-- exactly the same repeats a hash of the signature would, without this table
-- needing to know where any given provider puts its signature.
--
-- Bounded, not permanent: rows are pruned by the retention job, so this stays a
-- window rather than a growing ledger of every body ever received.
-- provider_delivery_id is stored beside the fingerprint so a GENUINE retry is
-- not mistaken for a replay. A provider retrying an unacknowledged delivery
-- re-sends the same body under the same id, which the delivery history already
-- handles as a duplicate. The attack is the same body under a DIFFERENT id, and
-- that is exactly what this column distinguishes.
CREATE TABLE trigger_delivery_replay_keys (
    source_id            TEXT        NOT NULL,
    body_hash            BYTEA       NOT NULL,
    provider_delivery_id TEXT        NOT NULL,
    seen_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (source_id, body_hash)
);

-- The prune scan is by age across all sources.
CREATE INDEX idx_trigger_replay_keys_seen ON trigger_delivery_replay_keys (seen_at);

-- ─── Source: secret age, and requests that got through ────────────────────────
-- secret_rotated_at dates the signing secret currently on the source. A replay
-- of a payload that arrived under an OLDER secret is refused: rotating a secret
-- is what an operator does when the old one may be in someone else's hands, and
-- replaying is the one path that re-runs a stored payload without re-verifying
-- it. Existing rows inherit their creation time, which is when their secret was
-- in fact issued.
ALTER TABLE webhook_sources
    ADD COLUMN secret_rotated_at TIMESTAMPTZ;

UPDATE webhook_sources SET secret_rotated_at = created_at WHERE secret_rotated_at IS NULL;

-- accepted_count pairs with rejection_count. A rejection count alone says
-- nothing: five rejections on a source taking a million deliveries is noise, and
-- five on a source taking five is a wrong signing secret. The ratio is the
-- diagnostic, so both halves have to exist.
ALTER TABLE webhook_sources
    ADD COLUMN accepted_count BIGINT NOT NULL DEFAULT 0;

-- ─── Reaper scan ──────────────────────────────────────────────────────────────
-- The stale-claim reaper looks for claimed rows, which are the rare ones. Without
-- a partial index that is a sequential scan of the whole queue on every sweep.
CREATE INDEX idx_pending_claimed
    ON trigger_pending_deliveries (claimed_at)
    WHERE claimed_at IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_pending_claimed;

ALTER TABLE webhook_sources
    DROP COLUMN IF EXISTS accepted_count,
    DROP COLUMN IF EXISTS secret_rotated_at;

DROP INDEX IF EXISTS idx_trigger_replay_keys_seen;
DROP TABLE IF EXISTS trigger_delivery_replay_keys;

-- +goose StatementEnd
