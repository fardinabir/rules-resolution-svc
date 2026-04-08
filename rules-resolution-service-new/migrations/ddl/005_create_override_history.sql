CREATE TABLE IF NOT EXISTS override_history (
    id              BIGSERIAL PRIMARY KEY,
    override_id     TEXT NOT NULL,
    action          TEXT NOT NULL CHECK (action IN ('created', 'updated', 'status_changed')),
    changed_by      TEXT NOT NULL,
    changed_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    snapshot_before JSONB,
    snapshot_after  JSONB NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_override_history_override_id
    ON override_history (override_id, changed_at DESC);
