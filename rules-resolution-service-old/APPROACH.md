# APPROACH.md

## Schema Design

**Selector as nullable columns, not JSON.** The four selector dimensions (`state`, `client`, `investor`, `case_type`) are stored as nullable `TEXT` columns. `NULL` means unpinned (wildcard). The alternative — a `JSONB` selector column — would require `@>` containment operators that are harder to compose and prevent standard B-tree indexing. Nullable columns let the resolution query use a simple `(state IS NULL OR state = $x)` predicate that the planner handles natively.

**Single covering index for resolution.** The primary index is:
```sql
CREATE INDEX idx_overrides_resolution
    ON overrides (step_key, trait_key, specificity DESC, effective_date DESC, state, client, investor, case_type)
    WHERE status = 'active';
```
Including the selector columns enables index-only scans: after filtering by `step_key` and `trait_key`, the planner can apply the selector conditions from the index leaf pages without touching the heap. The partial condition `WHERE status = 'active'` keeps the index small by excluding draft and archived rows.

**JSONB for trait values.** The six trait types (integer, boolean, string, string array) are incompatible at the column level. A single `value JSONB` column handles all types. The application always knows the expected type from `traitKey`, so deserialization is deterministic.

**Specificity stored, not computed.** Specificity (count of non-null selector dimensions) is stored as a column and enforced by the application on every write. Recomputing it via a `CASE WHEN` expression on every query scan adds unnecessary CPU and prevents the index on `specificity` from being useful.

**No FK from `override_history` to `overrides`.** History is an append-only audit ledger. An archived override's full history must remain readable. A foreign key would either prevent archival or cascade-delete history — both wrong.

**Separate `defaults` table.** Defaults are immutable reference data loaded from a seed file. Mixing them into the `overrides` table as specificity-0 records would require filtering them out of CRUD APIs, conflict detection, and the audit trail. Separation keeps both tables simpler and intent clearer.

---

## Resolution Algorithm

One SQL query fetches all matching overrides for the full resolve call. The query filters by `status = 'active'`, effective date window (`effectiveDate <= asOfDate AND expiresDate > asOfDate`), and the `IS NULL OR col = $x` selector predicates, then orders by `(step_key, trait_key, specificity DESC, effective_date DESC, created_at DESC)`.

Go groups the results by `(stepKey, traitKey)` in O(k) where k is the number of matching overrides (typically 10–30 for a given context). Within each group, `group[0]` is always the winner — the DB ordering guarantees it. The resolver iterates the 6×6 step/trait grid and calls `pickWinner` for each cell, returning defaults for empty groups.

**Complexity:** O(k) grouping pass + O(36) grid iteration = O(k + 36) ≈ O(k) per resolve call. No N+1 queries. The explain endpoint reuses the same query and candidates.

---

## Edge Cases

**Equal specificity, same `effectiveDate` (conflict).** The DB `ORDER BY` falls back to `created_at DESC`, so the most recently created override wins deterministically. The `pickWinner` function detects this situation by checking if `group[1]` has the same specificity and effectiveDate as `group[0]`, and sets `conflict: true` + `conflictsWith` on the resolved trait. Resolution never fails — the system is resilient and transparent about conflicts.

**`asOfDate` not provided.** Defaults to `time.Now().UTC()` in the service layer before the DB query is executed.

**Draft overrides.** Excluded by `status = 'active'` in the resolution query. They are stored but never participate in resolution until explicitly activated via `PATCH /{id}/status`.

**Archived overrides.** Also excluded from resolution. Conflict detection excludes archived overrides (`WHERE status != 'archived'`) so historical records don't generate false positives.

**Conflict detection.** Two overrides conflict when they target the same `(step_key, trait_key, specificity)`, their date ranges overlap, and their selectors are compatible (no dimension where both pin a different value). Compatible selectors means there exists at least one case context that both overrides would match — making neither able to shadow the other. The detection runs in Go against the full non-archived override set, making the logic independently testable without complex SQL self-joins.

---

## Trade-offs and What I'd Change

**Defaults caching.** The defaults table never changes after seeding, but the current implementation queries it on every resolve call. In production I'd load defaults once at startup into a `sync.Map` or a plain `map` protected by a `sync.RWMutex`. Cache invalidation is only needed if defaults become mutable via an API endpoint.

**ID generation.** Override IDs are currently `time.Now().UnixNano()` suffixed, which is not collision-safe under concurrent creation. I'd use a UUID (e.g., `github.com/google/uuid`) or a short random string (nanoid) in production.

**Bulk resolution.** The current `/resolve` endpoint handles one context at a time. A `POST /api/resolve/bulk` endpoint could batch multiple contexts into a single DB round-trip using an `IN` clause or a temporary table join — avoiding O(n) queries for bulk operations like dashboard views across many active cases.

**Validation rules.** The service validates `stepKey` and `traitKey` on override creation, but does not validate dimension values (e.g., `state` must be a valid US state code, `client` must be a known servicer). Adding a validation layer with a configurable allowlist would prevent bad data from entering the system.

---

## Nice-to-Have: Containerized Deployment

Implemented. The repo includes a `Dockerfile` (multi-stage, Alpine-based, ~15MB image) and a `docker-compose.yml` that starts PostgreSQL and the service together with health checks. The service auto-runs migrations on startup so `docker compose up` is the only command needed to get a fully functional environment.
