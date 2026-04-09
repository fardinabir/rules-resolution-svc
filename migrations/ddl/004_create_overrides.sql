CREATE TABLE IF NOT EXISTS overrides (
    id              TEXT PRIMARY KEY,
    step_key        TEXT NOT NULL REFERENCES steps(key),
    trait_key       TEXT NOT NULL,

    state           TEXT,
    client          TEXT,
    investor        TEXT,
    case_type       TEXT,

    specificity     INT NOT NULL CHECK (specificity BETWEEN 0 AND 4),

    value           JSONB NOT NULL,
    effective_date  DATE NOT NULL,
    expires_date    DATE,

    status          TEXT NOT NULL DEFAULT 'draft'
                        CHECK (status IN ('draft', 'active', 'archived')),

    description     TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by      TEXT NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by      TEXT,

    CONSTRAINT valid_date_range CHECK (expires_date IS NULL OR expires_date > effective_date)
);

CREATE INDEX IF NOT EXISTS idx_overrides_resolution
    ON overrides (step_key, trait_key, specificity DESC, effective_date DESC,
                  state, client, investor, case_type)
    WHERE status = 'active';

CREATE INDEX IF NOT EXISTS idx_overrides_conflict
    ON overrides (step_key, trait_key, specificity)
    WHERE status != 'archived';

CREATE INDEX IF NOT EXISTS idx_overrides_status ON overrides (status);

CREATE INDEX IF NOT EXISTS idx_overrides_state    ON overrides (state)     WHERE state     IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_overrides_client   ON overrides (client)    WHERE client    IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_overrides_investor ON overrides (investor)  WHERE investor  IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_overrides_casetype ON overrides (case_type) WHERE case_type IS NOT NULL;
