CREATE TABLE overrides (
    id              TEXT PRIMARY KEY,
    step_key        TEXT NOT NULL REFERENCES steps(key),
    trait_key       TEXT NOT NULL,

    -- Selector dimensions. NULL = unpinned (wildcard).
    state           TEXT,
    client          TEXT,
    investor        TEXT,
    case_type       TEXT,

    -- Computed from selector: COUNT of non-null dimensions above.
    -- Enforced by application layer on every write.
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

-- Primary resolution query: covering index — selector columns at the end
-- allow index-only scans without touching the heap.
-- Partial index on status='active' keeps it lean.
CREATE INDEX idx_overrides_resolution
    ON overrides (step_key, trait_key, specificity DESC, effective_date DESC,
                  state, client, investor, case_type)
    WHERE status = 'active';

-- Conflict detection: self-join on same step+trait+specificity.
-- Includes draft+active (excludes archived) so conflicts are caught before activation.
CREATE INDEX idx_overrides_conflict
    ON overrides (step_key, trait_key, specificity)
    WHERE status != 'archived';

-- Status filter for CRUD list endpoint
CREATE INDEX idx_overrides_status ON overrides (status);
