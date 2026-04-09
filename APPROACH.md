# Rules Resolution Service — Approach

---

## 1. Schema Design

### Multi-dimensional Selector: Nullable Columns

Each override pins 0–4 dimensions (`state`, `client`, `investor`, `case_type`). These are stored as four nullable `TEXT` columns where `NULL` means "wildcard — matches any value in that dimension."

The resolution query becomes a standard indexed predicate:
```sql
(state IS NULL OR state = $state)
AND (client IS NULL OR client = $client)
```
A JSON selector column would require `jsonb @>` — less selective, harder to index-compose.

### Key Schema Decisions

**Why nullable columns over a JSON selector?**  
The `(col IS NULL OR col = $x)` predicate is a standard B-tree equality check. PostgreSQL can use a partial index combining the status filter with the dimension columns. A JSON column would require `jsonb @>` which is less selective, cannot be efficiently combined with other predicates, and forces a GIN index instead of B-tree.

**Why JSONB for trait values?**  
The 6 trait types are incompatible at the column level (`slaHours: int`, `feeAuthRequired: bool`, `requiredDocuments: []string`). Alternatives: (a) 6 typed columns with most null — wastes space, awkward joins; (b) `TEXT` with app-level parsing — loses type safety. JSONB is clean, PostgreSQL handles serialization, and the application always knows the expected type from `traitKey`, making `NormalizeTraitValue` deterministic.

**Why store `specificity` as a column?**  
Specificity is derivable from the selector (count of non-null dimensions) but queried in `ORDER BY` on every resolution call and in the conflict detection self-join. Computing it inline (`CASE WHEN state IS NOT NULL THEN 1 ELSE 0 END + ...`) on every row is expensive. Materializing it as a stored column — recomputed on every write, never trusted from the caller — makes both operations a simple integer comparison.

**Why no FK from `override_history` to `overrides`?**  
History is an append-only audit ledger. If an override is archived, its history must remain intact and queryable. A foreign key would prevent deletion or complicate archival with cascades. The `override_id` column is an intentional soft reference.

**Why a separate `defaults` table?**  
Defaults are immutable reference data seeded from `defaults.json`. Mixing them into the `overrides` table would require filtering them out of CRUD listings, conflict detection, and audit trail queries. Keeping them separate makes both tables simpler and the caching of default values straightforward — the resolver falls back to defaults only when no override matches.

### Indexes

```sql
-- Resolution: partial covering index, index-only scans possible
CREATE INDEX idx_overrides_resolution
    ON overrides (step_key, trait_key, specificity DESC, effective_date DESC,
                  state, client, investor, case_type)
    WHERE status = 'active';

-- Conflict detection self-join
CREATE INDEX idx_overrides_conflict
    ON overrides (step_key, trait_key, specificity)
    WHERE status = 'active';

-- Per-dimension partial indexes for list filtering
CREATE INDEX idx_overrides_state    ON overrides (state)     WHERE state IS NOT NULL;
CREATE INDEX idx_overrides_client   ON overrides (client)    WHERE client IS NOT NULL;
-- (same for investor, case_type)
```

---

## 2. Resolution Algorithm

### Single Query, O(k) Grouping

One SQL round-trip fetches all matching overrides for the entire 6×6 grid:

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

Go groups the pre-sorted rows by `(stepKey, traitKey)` in a single linear scan. Within each group, `group[0]` is the winner — the DB `ORDER BY` guarantee eliminates all comparison logic in Go.

**Complexity:**
- DB query: `O(log n)` index seek + `O(k)` row scan, where `k` ≈ 10–30 matching overrides for a typical context and `n` = total active overrides.
- Go grouping: `O(k)` — one pass, append-order preserved.
- Winner selection: `O(1)` per cell (`group[0]`).
- Total per resolve call: effectively `O(k)` after the index seek. A naive per-cell implementation would be 36 queries — `O(36 × log n)`.

### Edge Cases Handled

| Case | Handling |
|---|---|
| Equal specificity, different `effectiveDate` | Newer `effectiveDate` wins — `ORDER BY effective_date DESC` |
| Equal specificity + equal `effectiveDate` | `created_at DESC` as deterministic tiebreak — surfaced in explain as `"SELECTED — tiebreak by createdAt"` |
| Draft overrides | Excluded by `status = 'active'` — never reach the resolver |
| Expired overrides | Excluded by `expires_date > $asOfDate` |
| No override matches | Falls back to defaults table value |
| `asOfDate` omitted | Defaults to `time.Now().UTC()` in the service layer |

### Explain Endpoint

`POST /api/resolve/explain` reuses the same SQL query and grouping. Instead of returning just `group[0]`, it annotates every candidate with a human-readable outcome label:

- `"SELECTED — highest specificity"`
- `"SHADOWED — lower specificity (2 < 3)"`
- `"SHADOWED — older effectiveDate (2025-01-01 < 2025-09-01)"`
- `"SELECTED — tiebreak by createdAt (conflict flagged)"`

The response is a flat array of 36 `TraitTrace` objects — one per step/trait cell — each containing the winning value, which override it came from, and every evaluated candidate.

---

## 3. Conflict Detection

`GET /api/overrides/conflicts` runs a SQL self-join:

```sql
FROM overrides a JOIN overrides b
    ON a.id < b.id
    AND a.step_key = b.step_key AND a.trait_key = b.trait_key
    AND a.specificity = b.specificity
WHERE a.status = 'active' AND b.status = 'active'
  AND a.effective_date < COALESCE(b.expires_date, 'infinity'::date)
  AND b.effective_date < COALESCE(a.expires_date, 'infinity'::date)
  AND NOT (a.state IS NOT NULL AND b.state IS NOT NULL AND a.state != b.state)
  -- (same for client, investor, case_type)
```

Two overrides conflict when: same step/trait, same specificity, overlapping date ranges, and compatible selectors (no dimension where both are pinned to different values — meaning there exists a case context both would match simultaneously). Only `active` overrides are checked — draft overrides never participate in resolution and cannot create real conflicts.

---

## 4. Nice-to-Have Features Implemented

### Caching Strategy — Decorator Pattern

A Redis caching layer is added in front of the two hot-path repository reads without the service layer knowing. `cachedOverrideRepo` and `cachedDefaultRepo` implement the same `OverrideRepository` and `DefaultRepository` interfaces as the GORM-backed implementations. The service receives the cached version through constructor injection — zero coupling.

```
Service → OverrideRepository (interface)
               ↑
         cachedOverrideRepo  →  Redis
               ↑ (on miss)
         pgOverrideRepo  →  PostgreSQL
```

Cache keys:
- `resolve:overrides:{state}:{client}:{investor}:{caseType}:{YYYY-MM-DD}` — 5 min TTL
- `resolve:defaults:all` — 1 hr TTL (immutable reference data)
- `resolve:bulk:{sha256-of-all-contexts}` — single key for entire bulk request

Any override mutation (create / update / status change) flushes `resolve:overrides:*` and `resolve:bulk:*` via a Redis SCAN+DEL cursor loop. This eliminates the primary DB cost on the hot resolve path. If Redis is unavailable, the service falls back to `NoopCache{}` transparently — no startup failure, no code path change.

### Bulk Resolution — Single DB Round-Trip

`POST /api/resolve/bulk` resolves up to 50 case contexts in **one database query** using a `VALUES` CTE:

```sql
WITH contexts(ctx_idx, state, client, investor, case_type, as_of_date) AS (
    VALUES (0, $1::text, $2::text, $3::text, $4::text, $5::timestamptz),
           (1, $6::text, ...)
)
SELECT c.ctx_idx, o.*
FROM contexts c
JOIN overrides o ON (matching predicates)
ORDER BY c.ctx_idx, o.step_key, o.trait_key,
         o.specificity DESC, o.effective_date DESC, o.created_at DESC
```

The `ctx_idx` column partitions results back to their originating context. Defaults are fetched once and reused across all N resolutions. A naive implementation would issue N × 2 queries (overrides + defaults per context). This approach keeps it constant at 2 queries regardless of batch size.

### Containerized Deployment

Full local stack with `docker-compose.yml` (Postgres 15, Redis 7, app). Both infra services use health checks; the app container waits for both before starting. `make start && make seed` is the complete setup sequence for a reviewer.

---

## 5. Bonus Engineering Points

- **Idiomatic Go layering** — strict Controller → Service → Repository with interfaces at every boundary; DI at the composition root.
- **Decorator pattern for caching** — Redis layer is transparent to the service; swappable via the `Cache` interface with `NoopCache` as the zero-config fallback.
- **Repository pattern** — all persistence behind `OverrideRepository` / `DefaultRepository` interfaces; raw SQL via `GORM Raw()` for full query control without ORM interference.
- **Test coverage** — 12 domain unit tests (pure, no DB), 12 acceptance tests driven by `test_scenarios.json`, handler-level integration tests for every endpoint including conflict detection and audit history.
- **CI pipeline** — GitHub Actions runs `make test-ci` (tests + coverage) and a separate reviewdog job for linting on every PR and push to main.
- **Makefile** — self-documenting command reference for every developer workflow: serve, migrate, seed, test, docker, lint, swagger.
- **Swagger UI** — interactive API documentation hosted on a separate server (port 1315). Generated via `swag init` from inline `// @` annotations on all handler methods; spec and HTML rebuilt with `make swagger`. Covers all 11 endpoints with request/response schemas, query parameters, and example payloads.

---

## 6. What I Would Add With More Time

| Item | Why |
|---|---|
| **Migration versioning** | DDL migrations rely on `CREATE TABLE IF NOT EXISTS` idempotency. A proper migration tracker (e.g., `goose` or `golang-migrate`) with a `schema_migrations` table prevents partial re-runs and supports rollbacks safely. |
| **Request timeouts** | No per-request deadline is enforced. A slow DB query or Redis latency spike can block an Echo worker indefinitely. A request-scoped timeout middleware (e.g., 5s for resolve, 30s for bulk) is a basic production guard. |
| **Dimension value validation** | `state`, `client`, `investor`, `caseType` are free-form strings. A registry of valid values with clear 400 errors would prevent bad data from entering the override table silently. This is the highest-value missing validation. |
| **Configuration Diff** (`POST /api/resolve/diff`) | Given two case contexts, show which traits differ and which override causes each difference. High operator value for comparing cases across jurisdictions or clients. |
| **Override Simulation** (`POST /api/overrides/simulate`) | Submit a proposed override without saving — see which existing overrides it shadows, which cases it would affect, and whether it creates new conflicts. Reduces the risk of activating a rule with unintended reach. |
