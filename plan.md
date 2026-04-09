# Implementation Plan — Rules Resolution Service

## 0. Orientation

### Evaluation priority order (drives every decision below)
1. **Resolution correctness** (30%) — the algorithm must pass all 12 test scenarios
2. **Schema design** (25%) — selector matching, effective dating, audit trail
3. **Go & API quality** (20%) — idiomatic Go, clean error handling, structure, uses of interfaces
4. **Explain & conflict detection** (15%) — trace must be useful, detection must be precise
5. **Code quality & docs** (10%) — APPROACH.md, readability

### Time budget allocation (~9 hours)
| Phase | Area | Est. time |
|---|---|---|
| 1 | Schema + migrations + seed | 1.5h |
| 2 | Domain models + resolution engine | 2h |
| 3 | Repository layer | 1h |
| 4 | REST API handlers | 1.5h |
| 5 | Conflict detection | 1h |
| 6 | Audit trail | 0.5h |
| 7 | Test scenario validation | 1h |
| 8 | README + APPROACH.md | 0.5h |

---

## 1. Project Structure

```
rules-resolution-service/
├── cmd/
│   └── server/
│       └── main.go              # wires everything together, starts HTTP server
├── internal/
│   ├── config/
│   │   └── config.go            # env-based config (DB_URL, PORT, etc.)
│   ├── domain/
│   │   ├── models.go            # Override, Step, Default, CaseContext, ResolvedConfig, etc.
│   │   └── resolver.go          # pure resolution function — no DB, fully testable
│   ├── repository/
│   │   ├── repository.go        # interfaces
│   │   ├── override.go          # override CRUD + history queries
│   │   ├── default.go           # defaults queries
│   │   └── step.go              # steps queries
│   ├── service/
│   │   ├── resolve.go           # orchestrates: fetch candidates → call resolver → return
│   │   └── override.go          # override management: validate, create, update, conflicts
│   └── api/
│       ├── router.go            # registers all routes
│       ├── handler/
│       │   ├── resolve.go       # POST /api/resolve, POST /api/resolve/explain
│       │   └── override.go      # override CRUD + status + history + conflicts
│       ├── middleware/
│       │   └── logger.go        # request logging
│       └── response.go          # shared JSON response helpers
├── migrations/
│   ├── 001_create_steps.sql
│   ├── 002_create_defaults.sql
│   ├── 003_create_overrides.sql
│   └── 004_create_override_history.sql
├── scripts/
│   └── seed/
│       └── main.go              # reads JSON files, inserts seed data
├── seed_data/  # (existing)
├── docker-compose.yml
├── Dockerfile
├── go.mod
├── README.md
├── APPROACH.md
└── plan.md
```

### Key architectural principle
- `domain/resolver.go` is a **pure function** — takes `[]Override`, `map[StepTrait]DefaultValue`, `CaseContext` and returns `ResolvedConfig`. Zero DB dependency. This is the most important code in the project and it must be independently testable.
- The service layer fetches data from DB, passes it to the pure resolver, and shapes the response.
- Handlers only parse/validate HTTP input and serialize output.

### Libraries
- **`github.com/go-chi/chi/v5`** — lightweight router, idiomatic middleware
- **`github.com/jackc/pgx/v5`** — native PostgreSQL driver (better than `database/sql` for Postgres-specific features)
- **`github.com/golang-migrate/migrate/v4`** — migration runner
- Standard library only for everything else (`encoding/json`, `net/http`, `time`, `log/slog`)

---

## 2. Database Schema

### Design decisions

**The selector problem:** An override pins 0–4 dimensions. The resolution query must find all overrides where every *pinned* dimension matches the case context, and unpinned dimensions are wildcards.

**Decision: nullable columns, not JSON.** Store each dimension as a nullable `TEXT` column (`state`, `client`, `investor`, `case_type`). `NULL` = unpinned (wildcard). This makes the matching predicate a simple SQL expression and lets Postgres use indexes efficiently.

The alternative (JSON selector column) would require `jsonb` operators and prevent standard indexing on individual dimensions.

**The value problem:** Trait values are heterogeneous — integer (`slaHours`), boolean (`feeAuthRequired`), string (`templateId`), string array (`requiredDocuments`).

**Decision: `JSONB` value column.** A single `value JSONB NOT NULL` column handles all types cleanly. At the Go layer, deserialize into `interface{}` / `any` and preserve type via the `traitKey`.

### `steps` table
```sql
CREATE TABLE steps (
    key         TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT,
    position    INT  NOT NULL
);
```

### `defaults` table
```sql
CREATE TABLE defaults (
    step_key    TEXT NOT NULL REFERENCES steps(key),
    trait_key   TEXT NOT NULL,
    value       JSONB NOT NULL,
    PRIMARY KEY (step_key, trait_key)
);
```

Defaults are reference data loaded from `defaults.json`. They are not managed via the API — no CRUD needed, no audit trail. The resolution engine falls back to this table when no override matches.

### `overrides` table — the core table
```sql
CREATE TABLE overrides (
    id              TEXT PRIMARY KEY,   -- 'ovr-001', 'ovr-002', etc. User-supplied or generated.
    step_key        TEXT NOT NULL REFERENCES steps(key),
    trait_key       TEXT NOT NULL,

    -- Selector dimensions. NULL = unpinned (wildcard).
    state           TEXT,
    client          TEXT,
    investor        TEXT,
    case_type       TEXT,

    -- Computed from selector. Always = COUNT of non-null dimensions above.
    -- Stored for query efficiency. Must be enforced by application layer on every write.
    specificity     INT NOT NULL CHECK (specificity BETWEEN 0 AND 4),

    value           JSONB NOT NULL,
    effective_date  DATE NOT NULL,
    expires_date    DATE,               -- NULL = no expiry
    status          TEXT NOT NULL DEFAULT 'draft'
                        CHECK (status IN ('draft', 'active', 'archived')),

    description     TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by      TEXT NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by      TEXT,

    CONSTRAINT valid_date_range CHECK (expires_date IS NULL OR expires_date > effective_date)
);
```

**Indexes:**

```sql
-- Primary resolution query: covering index — selector columns included at the end
-- so Postgres can apply the (col IS NULL OR col = $x) filter without touching the heap.
-- Partial index on status='active' keeps the index lean (excludes draft/archived rows).
CREATE INDEX idx_overrides_resolution
    ON overrides (step_key, trait_key, specificity DESC, effective_date DESC,
                  state, client, investor, case_type)
    WHERE status = 'active';

-- Conflict detection: self-join on same step+trait+specificity
CREATE INDEX idx_overrides_conflict
    ON overrides (step_key, trait_key, specificity)
    WHERE status != 'archived';

-- Selector dimension indexes for filtering
CREATE INDEX idx_overrides_state    ON overrides (state)    WHERE state    IS NOT NULL;
CREATE INDEX idx_overrides_client   ON overrides (client)   WHERE client   IS NOT NULL;
CREATE INDEX idx_overrides_investor ON overrides (investor) WHERE investor IS NOT NULL;
CREATE INDEX idx_overrides_casetype ON overrides (case_type) WHERE case_type IS NOT NULL;
```

### `override_history` table
```sql
CREATE TABLE override_history (
    id              BIGSERIAL PRIMARY KEY,
    override_id     TEXT NOT NULL,          -- not FK — history must survive archival
    action          TEXT NOT NULL           -- 'created', 'updated', 'status_changed'
                        CHECK (action IN ('created', 'updated', 'status_changed')),
    changed_by      TEXT NOT NULL,
    changed_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    snapshot_before JSONB,                  -- NULL for 'created'
    snapshot_after  JSONB NOT NULL          -- full override record as JSON
);

CREATE INDEX idx_override_history_override_id ON override_history (override_id, changed_at DESC);
```

No FK to `overrides` intentionally — history is an immutable ledger and must not be cascade-deleted.

---

## 3. Domain Models (Go)

```go
// domain/models.go

type CaseContext struct {
    State    string    `json:"state"`
    Client   string    `json:"client"`
    Investor string    `json:"investor"`
    CaseType string    `json:"caseType"`
    AsOfDate time.Time `json:"asOfDate,omitempty"` // defaults to NOW() if zero
}

type Override struct {
    ID            string
    StepKey       string
    TraitKey      string
    Selector      Selector
    Specificity   int
    Value         any            // int64, bool, string, []string — depends on TraitKey
    EffectiveDate time.Time
    ExpiresDate   *time.Time
    Status        string
    Description   string
    CreatedAt     time.Time
    CreatedBy     string
    UpdatedAt     time.Time
    UpdatedBy     string
}

type Selector struct {
    State    *string `json:"state,omitempty"`
    Client   *string `json:"client,omitempty"`
    Investor *string `json:"investor,omitempty"`
    CaseType *string `json:"caseType,omitempty"`
}

// Specificity computes the selector's specificity — count of non-nil fields.
// This is the canonical function. Called on every write; stored value must equal this.
func (s Selector) Specificity() int {
    count := 0
    if s.State    != nil { count++ }
    if s.Client   != nil { count++ }
    if s.Investor != nil { count++ }
    if s.CaseType != nil { count++ }
    return count
}

// Matches returns true if this selector matches the given case context.
// A nil dimension is a wildcard — it matches any value.
func (s Selector) Matches(ctx CaseContext) bool {
    if s.State    != nil && *s.State    != ctx.State    { return false }
    if s.Client   != nil && *s.Client   != ctx.Client   { return false }
    if s.Investor != nil && *s.Investor != ctx.Investor { return false }
    if s.CaseType != nil && *s.CaseType != ctx.CaseType { return false }
    return true
}

type StepTrait struct {
    StepKey  string
    TraitKey string
}

// Resolution output types
type ResolvedConfig struct {
    Context    CaseContext              `json:"context"`
    ResolvedAt time.Time               `json:"resolvedAt"`
    Steps      map[string]ResolvedStep `json:"steps"`
}

type ResolvedStep map[string]ResolvedTrait  // traitKey → result

type ResolvedTrait struct {
    Value       any     `json:"value"`
    Source      string  `json:"source"`       // "default" or "override"
    OverrideID  string  `json:"overrideId,omitempty"`
    Conflict    bool    `json:"conflict,omitempty"`
    ConflictWith string `json:"conflictsWith,omitempty"`
}

// Explain output types
type ExplainResult struct {
    Context    CaseContext       `json:"context"`
    ResolvedAt time.Time        `json:"resolvedAt"`
    Traces     []TraitTrace     `json:"traces"`
}

type TraitTrace struct {
    Step          string          `json:"step"`
    Trait         string          `json:"trait"`
    ResolvedValue any             `json:"resolvedValue"`
    ResolvedFrom  *TraceSource    `json:"resolvedFrom"` // nil = default
    Candidates    []TraceCandidate `json:"candidates"`
}

type TraceSource struct {
    OverrideID    string    `json:"overrideId"`
    Selector      Selector  `json:"selector"`
    Specificity   int       `json:"specificity"`
    EffectiveDate time.Time `json:"effectiveDate"`
}

type TraceCandidate struct {
    OverrideID    string    `json:"overrideId"`
    Selector      Selector  `json:"selector"`
    Specificity   int       `json:"specificity"`
    EffectiveDate time.Time `json:"effectiveDate"`
    Value         any       `json:"value"`
    Outcome       string    `json:"outcome"` // "SELECTED — ...", "SHADOWED — ..."
}
```

---

## 4. Resolution Engine (Pure Function)

This is the most important code. Lives in `domain/resolver.go`. No imports from `repository` or `service`.

### The core algorithm

```go
// domain/resolver.go

// AllSteps and AllTraits define the canonical 6×6 grid.
var AllSteps  = []string{"title-search","file-complaint","serve-borrower","obtain-judgment","schedule-sale","conduct-sale"}
var AllTraits = []string{"slaHours","requiredDocuments","feeAmount","feeAuthRequired","assignedRole","templateId"}

// Resolve runs the resolution algorithm.
// candidates: all overrides from DB that matched the case context for this asOfDate
//             (DB already filtered by: status=active, effectiveDate<=asOfDate, expires logic, selector match)
//             sorted by (specificity DESC, effective_date DESC, created_at DESC) — DB ORDER BY handles this
// defaults:   map of StepTrait → default value (loaded once at startup or per-request)
func Resolve(
    ctx CaseContext,
    candidates []Override,
    defaults map[StepTrait]any,
) ResolvedConfig {

    // Group candidates by (stepKey, traitKey).
    // Because of ORDER BY in the DB query, within each group the first element is the winner.
    grouped := make(map[StepTrait][]Override)
    for _, o := range candidates {
        k := StepTrait{o.StepKey, o.TraitKey}
        grouped[k] = append(grouped[k], o)
    }

    result := ResolvedConfig{
        Context:    ctx,
        ResolvedAt: time.Now().UTC(),
        Steps:      make(map[string]ResolvedStep),
    }

    for _, step := range AllSteps {
        resolvedStep := make(ResolvedStep)
        for _, trait := range AllTraits {
            k := StepTrait{step, trait}
            group := grouped[k]
            resolvedStep[trait] = pickWinner(group, defaults[k])
        }
        result.Steps[step] = resolvedStep
    }

    return result
}

func pickWinner(group []Override, defaultVal any) ResolvedTrait {
    if len(group) == 0 {
        return ResolvedTrait{Value: defaultVal, Source: "default"}
    }
    winner := group[0]
    rt := ResolvedTrait{
        Value:      winner.Value,
        Source:     "override",
        OverrideID: winner.ID,
    }
    // Detect conflict: second candidate has same specificity AND same effectiveDate
    if len(group) > 1 {
        runner := group[1]
        if runner.Specificity == winner.Specificity &&
            runner.EffectiveDate.Equal(winner.EffectiveDate) {
            rt.Conflict     = true
            rt.ConflictWith = runner.ID
        }
    }
    return rt
}
```

### The single DB query for resolution

The key insight: **one SQL query fetches ALL matching overrides for the entire resolution in one round-trip.** The DB does the selector filtering and ordering; Go does the grouping.

```sql
-- resolution query (parameterized)
SELECT
    id, step_key, trait_key,
    state, client, investor, case_type,
    specificity, value,
    effective_date, expires_date,
    created_at
FROM overrides
WHERE status = 'active'
  AND effective_date <= $1          -- asOfDate
  AND (expires_date IS NULL OR expires_date > $1)
  AND (state     IS NULL OR state     = $2)   -- case context state
  AND (client    IS NULL OR client    = $3)   -- case context client
  AND (investor  IS NULL OR investor  = $4)   -- case context investor
  AND (case_type IS NULL OR case_type = $5)   -- case context caseType
ORDER BY
    step_key,
    trait_key,
    specificity   DESC,
    effective_date DESC,
    created_at    DESC;
```

Complexity: O(k) where k is the number of overrides matching the context — typically 10-30 in this dataset. The ORDER BY means Go just needs to read the first override per (stepKey, traitKey) group.

### Explain — same query, different output

`Explain` calls the same DB query, but instead of calling `pickWinner`, it annotates every candidate:

```go
func Explain(ctx CaseContext, candidates []Override, defaults map[StepTrait]any) ExplainResult {
    grouped := groupByStepTrait(candidates)
    var traces []TraitTrace

    for _, step := range AllSteps {
        for _, trait := range AllTraits {
            k := StepTrait{step, trait}
            group := grouped[k]
            traces = append(traces, buildTrace(step, trait, group, defaults[k]))
        }
    }
    return ExplainResult{Context: ctx, ResolvedAt: time.Now().UTC(), Traces: traces}
}

func buildTrace(step, trait string, group []Override, defaultVal any) TraitTrace {
    trace := TraitTrace{Step: step, Trait: trait}
    if len(group) == 0 {
        trace.ResolvedValue = defaultVal
        return trace
    }

    winner := group[0]
    trace.ResolvedValue = winner.Value
    trace.ResolvedFrom = &TraceSource{
        OverrideID:    winner.ID,
        Selector:      winner.Selector,
        Specificity:   winner.Specificity,
        EffectiveDate: winner.EffectiveDate,
    }

    for i, c := range group {
        var outcome string
        if i == 0 {
            outcome = "SELECTED — highest specificity"
            if len(group) > 1 && group[1].Specificity == winner.Specificity {
                if group[1].EffectiveDate.Equal(winner.EffectiveDate) {
                    outcome = "SELECTED — tiebreak by createdAt (conflict flagged)"
                } else {
                    outcome = "SELECTED — tiebreak by effectiveDate"
                }
            }
        } else {
            if c.Specificity < winner.Specificity {
                outcome = fmt.Sprintf("SHADOWED — lower specificity (%d < %d)", c.Specificity, winner.Specificity)
            } else if c.EffectiveDate.Before(winner.EffectiveDate) {
                outcome = fmt.Sprintf("SHADOWED — older effectiveDate (%s < %s)", c.EffectiveDate.Format("2006-01-02"), winner.EffectiveDate.Format("2006-01-02"))
            } else {
                outcome = "SHADOWED — later createdAt lost tiebreak"
            }
        }
        trace.Candidates = append(trace.Candidates, TraceCandidate{
            OverrideID: c.ID, Selector: c.Selector, Specificity: c.Specificity,
            EffectiveDate: c.EffectiveDate, Value: c.Value, Outcome: outcome,
        })
    }
    return trace
}
```

---

## 5. Repository Layer

### Interface (repository/repository.go)

```go
type OverrideRepository interface {
    // Resolution
    FindMatchingOverrides(ctx context.Context, caseCtx domain.CaseContext) ([]domain.Override, error)

    // CRUD
    GetByID(ctx context.Context, id string) (*domain.Override, error)
    List(ctx context.Context, filter OverrideFilter) ([]domain.Override, error)
    Create(ctx context.Context, o domain.Override) error
    Update(ctx context.Context, o domain.Override) error
    UpdateStatus(ctx context.Context, id, status, updatedBy string) error

    // Audit
    GetHistory(ctx context.Context, overrideID string) ([]domain.OverrideHistoryEntry, error)
    RecordHistory(ctx context.Context, entry domain.OverrideHistoryEntry) error

    // Conflict detection
    FindConflictCandidates(ctx context.Context) ([]domain.Override, error) // returns all non-archived
}

type DefaultRepository interface {
    GetAll(ctx context.Context) (map[domain.StepTrait]any, error)
}

type StepRepository interface {
    GetAll(ctx context.Context) ([]domain.Step, error)
}
```

### OverrideFilter

```go
type OverrideFilter struct {
    StepKey   *string
    TraitKey  *string
    State     *string
    Client    *string
    Investor  *string
    CaseType  *string
    Status    *string
}
```

---

## 6. Conflict Detection Algorithm

Two overrides A and B conflict when ALL of:
1. Same `step_key` and `trait_key`
2. Same `specificity`
3. Date ranges overlap: `A.effective_date < COALESCE(B.expires_date, infinity)` AND `B.effective_date < COALESCE(A.expires_date, infinity)`
4. Selectors are **compatible** — no dimension where both are pinned to *different* values

The "compatible selectors" condition means: there EXISTS at least one case context that both overrides would match. Since they have equal specificity (same number of pinned dimensions), conflict occurs unless some pinned dimension disagrees.

**SQL for conflict detection:**

```sql
SELECT
    a.id AS id_a, b.id AS id_b,
    a.step_key, a.trait_key, a.specificity,
    a.state AS a_state, a.client AS a_client, a.investor AS a_investor, a.case_type AS a_case_type,
    b.state AS b_state, b.client AS b_client, b.investor AS b_investor, b.case_type AS b_case_type,
    a.effective_date AS a_eff, b.effective_date AS b_eff,
    a.expires_date AS a_exp, b.expires_date AS b_exp
FROM overrides a
JOIN overrides b
    ON a.id < b.id                        -- avoid duplicate pairs and self-joins
    AND a.step_key    = b.step_key
    AND a.trait_key   = b.trait_key
    AND a.specificity = b.specificity
WHERE a.status != 'archived'
  AND b.status != 'archived'
  -- Date ranges overlap
  AND a.effective_date < COALESCE(b.expires_date, 'infinity'::date)
  AND b.effective_date < COALESCE(a.expires_date, 'infinity'::date)
  -- No dimension where both are pinned to different values
  AND NOT (a.state     IS NOT NULL AND b.state     IS NOT NULL AND a.state     != b.state)
  AND NOT (a.client    IS NOT NULL AND b.client    IS NOT NULL AND a.client    != b.client)
  AND NOT (a.investor  IS NOT NULL AND b.investor  IS NOT NULL AND a.investor  != b.investor)
  AND NOT (a.case_type IS NOT NULL AND b.case_type IS NOT NULL AND a.case_type != b.case_type);
```

This runs entirely in the DB. For 49 overrides the self-join produces at most (49×48)/2 = 1176 pairs, trivially fast. Even at 10,000 overrides, the indexes on `(step_key, trait_key, specificity)` will keep this manageable.

**In Go**, iterate the rows and for each pair build the human-readable `reason` string:
```
"Same step/trait (file-complaint/slaHours), same specificity (2), overlapping effective dates, compatible selectors {state: FL} and {state: FL}"
```

---

## 7. Override Service Layer

### Create override (validation + specificity enforcement)

```go
func (s *OverrideService) Create(ctx context.Context, req CreateOverrideRequest, actor string) (*domain.Override, error) {
    // 1. Validate step_key exists
    // 2. Validate trait_key is one of the 6 known traits
    // 3. Validate dimension values (if state != nil, must be valid state code, etc.)
    // 4. Compute and SET specificity from selector — never trust caller's specificity
    req.Override.Specificity = req.Override.Selector.Specificity()
    // 5. Validate value type matches trait — call domain.NormalizeTraitValue(traitKey, raw)
    // 6. Assign server-generated ID: "ovr-" + ulid.Make().String() (caller-supplied ID is ignored)
    // 7. Set CreatedBy = actor, UpdatedBy = actor
    // 8. Insert to DB
    // 9. Record history (action: "created", snapshot_before: nil, snapshot_after: full record, changed_by: actor)
    return &override, nil
}
```

### Update override

```go
func (s *OverrideService) Update(ctx context.Context, id string, req UpdateOverrideRequest, actor string) (*domain.Override, error) {
    before, err := s.repo.GetByID(ctx, id)
    // ... validate, recompute specificity if selector changed
    // ... set UpdatedBy = actor
    // ... persist
    // ... record history (action: "updated", snapshot_before: before, snapshot_after: after, changed_by: actor)
}
```

### Status change

```go
func (s *OverrideService) UpdateStatus(ctx context.Context, id, newStatus, actor string) error {
    // Valid transitions:
    //   draft → active
    //   draft → archived
    //   active → archived
    //   archived → (nothing — archived is terminal)
    // ... set UpdatedBy = actor
    // ... record history (action: "status_changed", changed_by: actor)
}
```

---

## 8. REST API Routes

```
POST   /api/resolve                    → handler.Resolve
POST   /api/resolve/explain            → handler.Explain

GET    /api/overrides                  → handler.ListOverrides        (query params: stepKey, traitKey, state, client, investor, caseType, status, page, pageSize)
GET    /api/overrides/conflicts        → handler.GetConflicts         (NOTE: must be registered BEFORE /{id})
GET    /api/overrides/{id}             → handler.GetOverride
POST   /api/overrides                  → handler.CreateOverride
PUT    /api/overrides/{id}             → handler.UpdateOverride
PATCH  /api/overrides/{id}/status      → handler.UpdateOverrideStatus
GET    /api/overrides/{id}/history     → handler.GetOverrideHistory

GET    /health                         → health check
```

**Important:** `/api/overrides/conflicts` must be registered before `/{id}` in the router or `chi` will match "conflicts" as an ID parameter. With chi, use `r.Get("/conflicts", ...)` before `r.Get("/{id}", ...)`.

### List response envelope

All list endpoints return a consistent envelope:

```json
{
  "data": [...],
  "total": 49,
  "page": 1,
  "pageSize": 50
}
```

Pagination query params: `page` (default `1`), `pageSize` (default `50`, max `200`).
Repository `List()` returns `([]Override, int, error)` — total count via `COUNT(*) OVER()` window function (single query, no separate count round-trip).

### Actor header (mutation endpoints)

`POST`, `PUT`, `PATCH` endpoints require the `X-Actor` header (e.g. `X-Actor: user@firm.com`).
- Missing or empty → `400 { "error": "X-Actor header is required", "code": "MISSING_ACTOR" }`
- Extracted by a middleware and stored in `context.Context` under a typed key
- Passed through service → repository as `actor string` parameter
- Populates `createdBy`, `updatedBy`, and `changedBy` in audit records

### Error response format (consistent across all endpoints)

```json
{
  "error": "override not found",
  "code": "NOT_FOUND"
}
```

HTTP status codes:
- `200` — success
- `201` — created
- `400` — validation error (include field-level detail)
- `404` — not found
- `409` — conflict (when creating an override that immediately conflicts)
- `500` — internal server error

---

## 9. Seeding

`scripts/seed/main.go` — standalone binary, run once:
1. Reads `steps.json` → insert into `steps`
2. Reads `defaults.json` → insert into `defaults` (flatten nested structure)
3. Reads `overrides.json` → for each override:
   - Compute `specificity` from selector
   - Set `created_at = updated_at = NOW()`
   - Set `updated_by = "seed"`
   - Insert into `overrides`
   - Insert into `override_history` (action: "created")

Use `ON CONFLICT DO NOTHING` for idempotent re-runs.

---

## 10. Implementation Phases

### Phase 1 — Foundation (1.5h)
- [ ] `go mod init` + add dependencies
- [ ] `config/config.go` — read env vars (`DATABASE_URL`, `PORT`, `LOG_LEVEL`)
- [ ] Write all 4 migration files
- [ ] Write `scripts/seed/main.go`
- [ ] `docker-compose.yml` with Postgres + service
- [ ] Verify DB comes up and seed loads cleanly

### Phase 2 — Domain + Resolution Engine (2h)
- [ ] `domain/models.go` — all types as designed in §3
- [ ] `domain/traits.go` — `TraitTypes` map + `NormalizeTraitValue()` (see §13)
- [ ] `domain/resolver.go` — `Resolve()`, `Explain()`, `pickWinner()`, `buildTrace()`
- [ ] `internal/domain/resolver_test.go` — table-driven unit tests (see §15 Phase C1)

### Phase 3 — Repository Layer (1h)
- [ ] `repository/repository.go` — interfaces
- [ ] `repository/override.go` — implement all methods using pgx
- [ ] `repository/default.go` — `GetAll()` returns `map[StepTrait]any`
- [ ] `repository/step.go` — `GetAll()`
- [ ] Manual test: run resolution query against seeded DB, verify rows come back

### Phase 4 — Service + Handlers (1.5h)
- [ ] `service/resolve.go` — `Resolve(ctx, caseCtx)` and `Explain(ctx, caseCtx)`
  - Fetch defaults (can cache in memory on startup — they never change)
  - Call `repo.FindMatchingOverrides(ctx, caseCtx)`
  - Call `domain.Resolve()` or `domain.Explain()`
- [ ] `service/override.go` — CRUD with validation, specificity enforcement, history recording; all mutating methods accept `actor string` (see §14)
- [ ] `api/middleware/actor.go` — extract `X-Actor` header into context; reject if missing on mutation routes
- [ ] `api/router.go` — wire chi router; apply actor middleware to POST/PUT/PATCH routes
- [ ] `api/handler/resolve.go` — parse request, call service, serialize response
- [ ] `api/handler/override.go` — all 7 override endpoints; list handler reads `page`/`pageSize` params and returns envelope (see §8)
- [ ] `cmd/server/main.go` — DI wiring, migration on startup, serve

### Phase 5 — Conflict Detection (1h)
- [ ] `repository/override.go` — `FindConflictCandidates()` with the self-join SQL (§6)
- [ ] `service/override.go` — `GetConflicts()` builds `[]ConflictPair` with reason strings
- [ ] `api/handler/override.go` — `GET /api/overrides/conflicts`
- [ ] Verify seed data returns zero conflicts

### Phase 6 — Audit Trail (0.5h)
- [ ] `repository/override.go` — `RecordHistory()`, `GetHistory()`
- [ ] Ensure Create, Update, UpdateStatus all call `RecordHistory`
- [ ] `GET /api/overrides/{id}/history` returns entries sorted by `changed_at DESC`

### Phase 7 — Test Scenario Validation (1h)
- [ ] `internal/acceptance/scenarios_test.go` — acceptance tests against real DB (see §15 Phase C2)
  - Build tag: `//go:build integration`
  - Reads `test_scenarios.json`, iterates all 12 scenarios
  - POST `/api/resolve` per scenario, assert each `expectedResolution` field matches
  - On failure: print diff showing step/trait/expected/got
- [ ] `internal/api/handler/override_test.go` — handler validation tests (see §15 Phase C3)
- [ ] Fix any resolution failures — these are the acceptance tests

### Phase 8 — Docs (0.5h)
- [ ] `README.md` — setup instructions, run commands, curl examples
- [ ] `APPROACH.md` — schema decisions, algorithm walkthrough, edge case handling, trade-offs

---

## 11. Edge Cases and How to Handle Them

| Edge Case | Handling |
|---|---|
| `asOfDate` not provided | Default to `time.Now().UTC()` in the service layer before calling the DB |
| `draft` override | Never returned by `FindMatchingOverrides` — DB filters `status = 'active'` |
| `archived` override | Same as above — excluded from resolution |
| Expired override (`expiresDate` in past) | DB filter: `expires_date IS NULL OR expires_date > asOfDate` |
| Future `effectiveDate` | DB filter: `effective_date <= asOfDate` |
| Spec tie, same effectiveDate (conflict) | `pickWinner` uses `createdAt DESC` as final tiebreaker; sets `conflict: true` flag |
| No matching overrides for a trait | Falls back to default value |
| `specificity` provided by caller in POST body | Ignored — always recomputed from selector |
| `status` transition to invalid state (e.g. archived → active) | 400 with message listing valid transitions |
| Override with `expiresDate < effectiveDate` | Rejected by DB constraint AND validated in service layer (return 400) |

---

## 12. Schema Design Rationale (for APPROACH.md)

**Why nullable columns over JSON selector?**
The resolution query's `(state IS NULL OR state = $x)` predicate is a standard indexed equality check. Postgres can use a partial index on `status = 'active'` + the dimension columns. A JSON selector would require `jsonb @>` operator which is less selective and harder to compose with other predicates.

**Why JSONB for value?**
The 6 trait types are incompatible at the column level. Alternatives: (a) 6 typed columns with most null — wastes space and is awkward; (b) `TEXT` with application-level parsing — loses type safety; (c) `JSONB` — clean, Postgres handles serialization, pgx deserializes into `any`. The application always knows the trait type from `traitKey` so type assertion is deterministic.

**Why store `specificity` as a column?**
It's derivable from the selector but queried in ORDER BY and the conflict detection self-join. Materializing it avoids computing `(CASE WHEN state IS NOT NULL THEN 1 ELSE 0 END + ...)` on every row. Enforced by application layer on every write.

**Why no FK from override_history to overrides?**
History is an append-only audit ledger. If an override is archived (or hypothetically deleted), its history must remain intact. Foreign key constraints would prevent or complicate this.

**Why separate `defaults` table instead of specificity-0 overrides?**
Defaults are immutable reference data loaded from seed files. Mixing them into the `overrides` table would require filtering them out of the CRUD API, conflict detection, and audit trail. The separation makes both tables simpler and the intent clearer.

---

## 13. Value Type System

JSONB deserialization in pgx returns JSON numbers as `float64`. Without normalization, `slaHours: 720` arrives in Go as `float64(720)`, causing incorrect type assertions and wrong JSON output.

### Type registry (`domain/traits.go`)

```go
// TraitTypes maps each trait key to its canonical Go type name.
var TraitTypes = map[string]string{
    "slaHours":          "int",
    "requiredDocuments": "[]string",
    "feeAmount":         "int",
    "feeAuthRequired":   "bool",
    "assignedRole":      "string",
    "templateId":        "string",
}

// NormalizeTraitValue validates a raw value (e.g., from JSON decode or JSONB read)
// against the expected type for the given traitKey.
// Returns the normalized value or a descriptive error.
//
// Normalizations performed:
//   float64 → int64   for "int" traits  (JSON numbers decode as float64)
//   []interface{} → []string  for "[]string" traits
func NormalizeTraitValue(traitKey string, raw any) (any, error)
```

**Call sites:**
- `service/override.go` Create and Update — validate on every write before hitting the DB
- `repository/override.go` scan rows — normalize on every read from JSONB column

**Why not store as typed columns?** 6 typed columns would be mostly null and make every query more complex. JSONB + application-level normalization keeps the schema clean and the type contract enforced at the Go layer where `traitKey` is always known.

---

## 14. Actor Extraction

All mutation endpoints need an authenticated caller identity for the audit trail. There is no auth system in scope, so identity is supplied via a request header.

### Header: `X-Actor`

```
X-Actor: user@firm.com
```

**Middleware (`api/middleware/actor.go`):**

```go
type contextKey string
const ActorKey contextKey = "actor"

// RequireActor middleware extracts X-Actor and rejects the request if missing.
// Apply to POST, PUT, PATCH routes only.
func RequireActor(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        actor := strings.TrimSpace(r.Header.Get("X-Actor"))
        if actor == "" {
            writeError(w, http.StatusBadRequest, "X-Actor header is required", "MISSING_ACTOR")
            return
        }
        ctx := context.WithValue(r.Context(), ActorKey, actor)
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}

// ActorFromContext retrieves the actor string; panics if missing (programming error).
func ActorFromContext(ctx context.Context) string {
    return ctx.Value(ActorKey).(string)
}
```

**Flow:** handler calls `ActorFromContext(r.Context())` → passes to service method → service passes to repo as `actor` parameter → stored in `created_by`, `updated_by`, `changed_by`.

---

## 15. Testing Plan

### Phase C1 — Domain unit tests (`internal/domain/resolver_test.go`)

Pure function, no DB, no build tag. Table-driven using `testing.T`.

| # | Test name | What it verifies |
|---|---|---|
| 1 | `TestResolve_SpecificityWins` | Spec-3 beats spec-2 beats spec-1 beats default |
| 2 | `TestResolve_DefaultFallback` | Empty candidates → resolved from default |
| 3 | `TestResolve_EqualSpecNewerDateWins` | Same specificity, newer `effectiveDate` selected |
| 4 | `TestResolve_ConflictFlagged` | Equal spec + equal `effectiveDate` → `conflict: true`, `conflictsWith` set |
| 5 | `TestResolve_FutureEffectiveDateExcluded` | Override with future date not in candidates |
| 6 | `TestResolve_ExpiredOverrideExcluded` | Override past `expiresDate` not in candidates |
| 7 | `TestSelector_WildcardMatchesAny` | Nil dimension matches any context value |
| 8 | `TestSelector_PinnedDimensionMismatch` | Pinned dimension ≠ context → Matches() = false |
| 9 | `TestSelector_Specificity` | Count of non-nil fields is correct |
| 10 | `TestResolve_AllStepsAllTraits` | 6×6 grid fully populated in output |
| 11 | `TestExplain_CandidatesAnnotated` | Each candidate has correct `outcome` string |
| 12 | `TestExplain_DefaultTraceHasNoCandidates` | Default-resolved trait has empty candidates list |
| 13 | `TestNormalizeTraitValue_FloatToInt` | `float64(720)` → `int64(720)` for `slaHours` |
| 14 | `TestNormalizeTraitValue_WrongType` | `string` for `slaHours` → error |
| 15 | `TestNormalizeTraitValue_StringSlice` | `[]interface{}{"a","b"}` → `[]string{"a","b"}` |

### Phase C2 — Acceptance tests (`internal/acceptance/scenarios_test.go`)

Build tag: `//go:build integration`. Requires running Postgres (via `docker-compose.yml`).

```
go test -tags=integration ./internal/acceptance/... -v
```

**Structure:**
1. `TestMain`: connect to DB (from `DATABASE_URL` env), run migrations, seed data once for the suite
2. Start the HTTP server on a random port using `httptest.NewServer`
3. Load `test_scenarios.json`
4. For each scenario (12 total): POST `/api/resolve`, assert each `expectedResolution` key-value matches
5. On mismatch: `t.Errorf("scenario %d step=%s trait=%s: got %v, want %v", ...)`

This is the primary correctness gate — all 12 must pass before the implementation is considered done.

### Phase C3 — Handler validation tests (`internal/api/handler/override_test.go`)

`httptest`-based, service layer replaced with a stub. No build tag (runs in standard `go test`).

| Test | Asserts |
|---|---|
| `TestCreate_MissingActorHeader` | → 400 `MISSING_ACTOR` |
| `TestCreate_InvalidTraitKey` | → 400 validation error |
| `TestCreate_SpecificityIgnored` | Caller-provided `specificity` overwritten by server |
| `TestCreate_IDIgnored` | Caller-provided `id` overwritten by server-generated ULID |
| `TestUpdateStatus_InvalidTransition` | archived → active → 400 |
| `TestGetOverride_NotFound` | → 404 `NOT_FOUND` |
| `TestList_Pagination` | `page=2&pageSize=5` → correct envelope shape |
| `TestCreate_WrongValueType` | `slaHours: "not-a-number"` → 400 |
