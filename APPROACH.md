# Rules Resolution Service — Design Approach

## 1. Schema Design

### Selector as Nullable Columns (not JSON)

Each override rule pins 0–4 dimensions (`state`, `client`, `investor`, `case_type`). These are stored as nullable `TEXT` columns where `NULL` means "wildcard — matches any value."

**Why not a JSON selector column?**  
The resolution query needs `(state IS NULL OR state = $x)` predicates. With nullable columns, Postgres can use standard B-tree indexes on each dimension. A JSON selector column would require `jsonb @>` operators which are less selective and cannot be combined with other index conditions as efficiently.

### Covering Index for Resolution

```sql
CREATE INDEX idx_overrides_resolution
    ON overrides (step_key, trait_key, specificity DESC, effective_date DESC,
                  state, client, investor, case_type)
    WHERE status = 'active';
```

This partial covering index (filtered to `status = 'active'`) enables index-only scans for the resolution query. The selector columns at the tail allow Postgres to filter rows without touching the heap. Excluding draft/archived rows keeps the index small.

### JSONB for Trait Values

The 6 trait types are incompatible at the column level (`slaHours: int`, `feeAuthRequired: bool`, `requiredDocuments: []string`). A `value JSONB NOT NULL` column handles all types cleanly. The application always knows the expected Go type from `traitKey`, so type assertion and normalization (`NormalizeTraitValue`) are deterministic.

### Stored Specificity

`specificity INT` is computed from the selector (count of non-null dimensions) and stored as a column. It is always enforced by the application on every write — the caller's value is ignored and recomputed. Storing it avoids a `CASE WHEN ... THEN 1` expression in every `ORDER BY` and conflict-detection self-join.

### No FK from `override_history` to `overrides`

History is an append-only audit ledger. If an override is archived, its history must remain intact. A FK constraint would prevent deletion (or make archival complicated). The `override_id` column is an intentional soft reference.

### Separate `defaults` Table

Default trait values are immutable reference data loaded from `defaults.json`. Keeping them in a separate table prevents them from appearing in CRUD lists, conflict detection, or audit trail queries. The resolution engine falls back to this table only when no override matches.

---

## 2. Resolution Algorithm

### Single SQL Query + O(k) Grouping

The resolution engine fetches all matching overrides for the entire case in one round-trip:

```sql
SELECT ... FROM overrides
WHERE status = 'active'
  AND effective_date <= $asOfDate
  AND (expires_date IS NULL OR expires_date > $asOfDate)
  AND (state IS NULL OR state = $state)
  AND (client IS NULL OR client = $client)
  AND (investor IS NULL OR investor = $investor)
  AND (case_type IS NULL OR case_type = $caseType)
ORDER BY step_key, trait_key, specificity DESC, effective_date DESC, created_at DESC
```

Go groups the pre-sorted rows by `(stepKey, traitKey)` in O(k) where k ≈ 10–30 for typical data. Within each group, `group[0]` is always the winner — the DB `ORDER BY` guarantee means no comparison logic is needed in Go.

When two overrides tie on both specificity and `effectiveDate`, the winner is `created_at DESC` (deterministic but arbitrary). Genuine conflicts are surfaced via `GET /api/overrides/conflicts`, not embedded in the resolve response.

### `explain` Endpoint

The `Explain` function reuses the same SQL query and the same `groupByStepTrait` grouping. Instead of just picking `group[0]`, it annotates every candidate with a human-readable `outcome` label:
- `"SELECTED — highest specificity"`
- `"SHADOWED — lower specificity (2 < 3)"`
- `"SHADOWED — older effectiveDate (2025-01-01 < 2025-09-01)"`
- `"SELECTED — tiebreak by createdAt"`

The response is a flat array of per-trait trace objects (`[]TraitTrace`) matching the spec's "response per trait" format exactly — no wrapper envelope.

### Performance

**One query for the entire 6×6 grid.** A naive implementation would issue one query per step/trait cell (36 queries per resolve call). This implementation issues exactly one query that fetches all matching overrides across all cells in a single round-trip. The result set is typically 10–30 rows for a real case context.

**Covering index eliminates heap access.** The partial index:
```sql
CREATE INDEX idx_overrides_resolution
    ON overrides (step_key, trait_key, specificity DESC, effective_date DESC,
                  state, client, investor, case_type)
    WHERE status = 'active';
```
includes every column the resolution query needs. Postgres can satisfy the query entirely from the index without touching the table heap. The `WHERE status = 'active'` predicate keeps the index lean by excluding draft and archived rows.

**DB sort eliminates Go-side sorting.** The `ORDER BY step_key, trait_key, specificity DESC, effective_date DESC, created_at DESC` in the query means the rows arrive pre-sorted into winner-first order per group. The `groupByStepTrait` pass is a single linear scan (`append` preserves arrival order), and `pickWinner` is just `group[0]` — O(1) per cell. Total Go-side work after the query is O(k).

**Stored `specificity` column.** Computing specificity inline (`CASE WHEN state IS NOT NULL THEN 1 ELSE 0 END + ...`) on every row in `ORDER BY` and the conflict self-join would be expensive. Materializing it as a stored column (always recomputed on write) makes both operations a simple integer comparison.

### `asOfDate` Defaulting

The service layer sets `asOfDate = time.Now().UTC()` if the caller omits it. Draft/archived overrides are excluded by the `status = 'active'` predicate. Future overrides are excluded by `effective_date <= asOfDate`. Expired overrides are excluded by `expires_date > asOfDate`.

---

## 3. Boilerplate Adaptation (chi → Echo, pgx → GORM)

The original implementation used `chi` + `pgx` + `golang-migrate`. This service migrates to the Echo + GORM boilerplate:

| Aspect | Old | New |
|--------|-----|-----|
| HTTP router | chi | Echo v4 |
| DB driver | pgx v5 (raw) | GORM + `db.Raw()` |
| Migrations | golang-migrate | Custom DDL file runner |
| Config | YAML + custom loader | Viper + Cobra |
| Logging | slog | logrus |

**GORM raw queries**: All complex queries (`FindMatchingOverrides`, `FindConflicts`, `List` with window function) use `db.Raw(sql, args...).Scan(&rows)`. CRUD mutations use `db.Exec(sql, args...)`. This gives full control over SQL without any ORM magic interfering with the resolution algorithm.

**Actor middleware**: Mutation routes (`POST`, `PUT`, `PATCH`) are wrapped in a `RequireActor()` Echo middleware that enforces the `X-Actor` header and stores the value at context key `"actor"`. Handlers retrieve it with `c.Get("actor").(string)`.

---

## 4. Conflict Detection (API)

`GET /api/overrides/conflicts` runs a SQL self-join:

```sql
FROM overrides a JOIN overrides b
    ON a.id < b.id                    -- avoid duplicate pairs
    AND a.step_key = b.step_key
    AND a.trait_key = b.trait_key
    AND a.specificity = b.specificity
WHERE a.status != 'archived' AND b.status != 'archived'
  AND a.effective_date < COALESCE(b.expires_date, 'infinity'::date)
  AND b.effective_date < COALESCE(a.expires_date, 'infinity'::date)
  AND NOT (a.state IS NOT NULL AND b.state IS NOT NULL AND a.state != b.state)
  AND NOT (a.client IS NOT NULL AND b.client IS NOT NULL AND a.client != b.client)
  AND NOT (a.investor IS NOT NULL AND b.investor IS NOT NULL AND a.investor != b.investor)
  AND NOT (a.case_type IS NOT NULL AND b.case_type IS NOT NULL AND a.case_type != b.case_type)
```

Two overrides conflict when:
1. Same `step_key`, `trait_key`, and `specificity`
2. Date ranges overlap
3. Selectors are compatible (no dimension where both are pinned to *different* values)

Compatible selectors means there exists at least one case context that both overrides would match — making them ambiguous at resolution time.

---

## 5. Override Status State Machine

```
draft ──► active ──► archived
  └──────────────────────────►
```

- `draft → active`: override enters production resolution
- `draft → archived`: discard without activating
- `active → archived`: retire a live rule
- `archived → *`: terminal state, no transitions allowed

Each transition is validated in `OverrideService.UpdateStatus` and recorded in `override_history` with `action: "status_changed"`.

---

## 6. Seed Script

`scripts/seed/main.go` is a standalone binary (separate from the service) that loads the three JSON files from `sr_backend_assignment_data/` into PostgreSQL in dependency order: steps → defaults → overrides.

**Run once after migrations:**
```bash
DATABASE_URL=postgres://postgres:postgres@localhost:5432/user?sslmode=disable \
DATA_DIR=../../sr_backend_assignment_data \
go run ./scripts/seed/main.go
```

**Design decisions:**
- `ON CONFLICT DO NOTHING` on all three tables makes the script idempotent — safe to re-run without duplicating data.
- `specificity` is computed from the selector during seeding (not read from the JSON file) to guarantee the stored column is always correct from the first row.
- Each seeded override is also written to `override_history` with `action: "created"` so the audit trail is complete from initial load.
- `createdBy` / `updatedBy` default to `"seed"` if the JSON record has no `createdBy` field.

**Known trade-offs:**
- Inserts are row-by-row (one round-trip per record). For 49 overrides this is negligible; a production bulk-load should use batched `INSERT ... VALUES (...),(...)` or `COPY`.
- The seed duplicates `Selector.Specificity()` from the domain package rather than importing it, to keep the binary dependency-light. A shared utility would be cleaner.

---

## 7. Nice-to-Have Features Implemented

Three of the optional enhancements from the assignment brief were implemented. Below is what was built and the rationale for prioritising each.

### Caching Strategy (Redis Repository Decorator)

**What:** A Redis caching layer sits in front of both hot-path repository reads: `FindMatchingOverrides` and `DefaultRepository.GetAll`. The implementation follows the decorator pattern — `cachedOverrideRepo` and `cachedDefaultRepo` wrap their respective GORM implementations and are transparent to the service layer.

**Cache keys and TTLs:**
- `resolve:overrides:{state}:{client}:{investor}:{caseType}:{YYYY-MM-DD}` → 5 min TTL
- `resolve:defaults:all` → 1 hour TTL (defaults are immutable reference data)

**Invalidation:** Any override mutation (create / update / status change) calls `FlushByPattern("resolve:overrides:*")` and `FlushByPattern("resolve:bulk:*")` via a Redis `SCAN + DEL` cursor loop. The defaults key is never evicted by override writes — only by TTL expiry.

**Why prioritised:** Resolution is a read-heavy operation called on every case. Without caching, every `POST /api/resolve` makes two DB round-trips. With a warm cache, both are eliminated to a single Redis `GET`. This is the highest-leverage optimisation for a production service.

### Bulk Resolution (`POST /api/resolve/bulk`)

**What:** A batch endpoint that accepts up to 50 case contexts and resolves all of them in **one DB round-trip**. The query uses a `VALUES` CTE to inject N contexts inline, joined against the `overrides` table with the same matching predicates as the single-context query, tagged with a `ctx_idx` column:

```sql
WITH contexts(ctx_idx, state, client, investor, case_type, as_of_date) AS (
    VALUES (0, $1::text, $2::text, $3::text, $4::text, $5::timestamptz),
           (1, $6::text, ...)
)
SELECT c.ctx_idx, o.*
FROM contexts c JOIN overrides o ON (matching predicates)
ORDER BY c.ctx_idx, o.step_key, o.trait_key,
         o.specificity DESC, o.effective_date DESC, o.created_at DESC
```

`ORDER BY ctx_idx` first preserves the per-context sort guarantee, so `group[0]` remains the correct `pickWinner` input. Defaults are fetched once and reused across all N resolutions.

**Bulk cache:** The entire request is hashed (SHA-256 over all context tuples) into a single cache key (`resolve:bulk:{hash}`). One Redis `GET` on the hot path — no per-context cache juggling.

**Why prioritised:** The assignment explicitly called out "no N+1 per context" as a requirement for batch resolution. The VALUES CTE approach was preferred over UNNEST because GORM's `Raw()` wrapping passes scalar positional parameters through the pgx codec reliably, whereas array binding through `pgtype` is driver-version-sensitive and harder to reason about.

### Containerized Deployment (Docker Compose + Makefile)

**What:** The full local stack runs with a single command. `docker-compose.yml` defines three services: `postgres:15-alpine`, `redis:7-alpine`, and `users-app` (the service binary). Both infrastructure services expose health checks; `users-app` waits for both to be healthy before starting. The Makefile exposes ergonomic targets:

| Target | Effect |
|--------|--------|
| `make up` | Start all services (detached) |
| `make down` | Stop all services |
| `make infra` | Start only Postgres + Redis (no app) |
| `make rebuild` | Force-rebuild the app image and restart |
| `make logs` | Tail all container logs |
| `make purge` | Tear down containers + volumes (full reset) |

**Why prioritised:** A reviewer should be able to run `make up && make seed` and hit the API within two minutes, without installing Go or Postgres locally. Containerising the app alongside its dependencies also validates that the Redis and Postgres hostnames, ports, and credentials in `config.docker.yaml` are correct — catching configuration drift before it reaches production.

---

## 8. Trade-offs

| Decision | Trade-off |
|----------|-----------|
| Defaults queried per resolve (DB) | Negligible cost at low volume; the Redis cache layer eliminates the round-trip on cache hits |
| `ovr-<uuid8>` ID format | Short and readable; full UUID would be safer against collisions |
| No dimension value validation | State codes, client names, investor names are not validated against a registry |
| Specificity enforced in app layer | Simpler than a DB trigger; requires discipline on every write path |
| Bulk cache TTL hardcoded to 5 min | Bulk cache shares the same implicit TTL as per-context overrides; a separate config key could offer more control |
