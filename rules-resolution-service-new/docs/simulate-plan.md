# Override Simulation Endpoint — `POST /api/overrides/simulate`

## Context

Submit a proposed override (without saving it) and get back three answers:
1. **What cases it would affect** — selector scope description (Option A: describe match space, no case enumeration needed)
2. **Which existing overrides it would shadow** — active/draft overrides for the same step/trait that this one beats
3. **Whether it creates new conflicts** — active/draft overrides it would tie with (same specificity + overlapping dates + compatible selectors)

Zero side-effects. No DB writes. No ID assigned. No history entry.

---

## Response Shape

```json
{
  "proposed": {
    "stepKey": "file-complaint",
    "traitKey": "slaHours",
    "selector": { "state": "FL", "client": "Chase" },
    "specificity": 2,
    "value": 72,
    "effectiveDate": "2026-01-01",
    "expiresDate": null
  },
  "affectedScope": {
    "specificity": 2,
    "description": "Matches all cases where state=FL AND client=Chase (investor, caseType are wildcards)",
    "pinnedDimensions": { "state": "FL", "client": "Chase" },
    "wildcardDimensions": ["investor", "caseType"]
  },
  "shadowedOverrides": [
    {
      "overrideId": "ovr-001",
      "selector": { "state": "FL" },
      "specificity": 1,
      "currentValue": 360,
      "effectiveDate": "2025-01-01",
      "reason": "SHADOWED — lower specificity (1 < 2)"
    }
  ],
  "newConflicts": [
    {
      "overrideId": "ovr-007",
      "selector": { "state": "FL", "client": "Chase" },
      "specificity": 2,
      "currentValue": 48,
      "effectiveDate": "2026-01-01",
      "reason": "CONFLICT — same specificity (2), compatible selectors, overlapping date range"
    }
  ]
}
```

---

## Algorithm

### 1. `affectedScope` — pure selector description, no DB

```
pinnedDimensions   = { dim: value }  for each non-nil selector field
wildcardDimensions = [dim]           for each nil selector field
specificity        = count(pinnedDimensions)
description        = human-readable sentence built from pinned + wildcard lists
```

### 2. `shadowedOverrides` — proposed wins over existing

For each existing active/draft override with the same `stepKey + traitKey`:

```
shadowed IF:
  selectorsCompatible(proposed, existing)          -- no dimension where both pinned to different values
  AND datesOverlap(proposed, existing)             -- date ranges intersect
  AND (
    existing.Specificity < proposed.Specificity    → reason: "SHADOWED — lower specificity (X < Y)"
    OR (
      existing.Specificity == proposed.Specificity
      AND existing.EffectiveDate.Before(proposed.EffectiveDate)
                                                   → reason: "SHADOWED — older effectiveDate (X < Y)"
    )
  )
```

### 3. `newConflicts` — proposed ties with existing (ambiguous resolution)

```
conflict IF:
  selectorsCompatible(proposed, existing)
  AND datesOverlap(proposed, existing)
  AND existing.Specificity == proposed.Specificity
  AND NOT existing.EffectiveDate.Before(proposed.EffectiveDate)
  AND NOT existing.EffectiveDate.Equal(proposed.EffectiveDate)  -- equal date handled separately
```

Actually simpler: an existing override is a **conflict** (not shadow) when:
- same specificity
- compatible selectors  
- overlapping dates
- it would NOT be shadowed by the proposed (i.e. effective dates are equal or existing is newer)

**`shadowedOverrides` and `newConflicts` are mutually exclusive sets.**

### Helper functions (reuse existing logic)

`selectorsCompatible(a, b Selector) bool` — returns `false` only when both a and b pin the same dimension to **different** values. Mirrors the SQL in `FindConflicts`.

`datesOverlap(aEff, aExp, bEff, bExp)` — `aEff < COALESCE(bExp, ∞) AND bEff < COALESCE(aExp, ∞)`.

---

## Files

### Create: `internal/domain/simulate.go`

New domain types + pure `Simulate()` function. No imports outside the `domain` package.

```
domain types:
  SimulationResult   { Proposed, AffectedScope, ShadowedOverrides, NewConflicts }
  ProposedSummary    { StepKey, TraitKey, Selector, Specificity, Value, EffectiveDate, ExpiresDate }
  AffectedScope      { Specificity, Description, PinnedDimensions map[string]string, WildcardDimensions []string }
  ShadowedOverride   { OverrideID, Selector, Specificity, CurrentValue, EffectiveDate, Reason }
  SimConflict        { OverrideID, Selector, Specificity, CurrentValue, EffectiveDate, Reason }

func Simulate(proposed Override, existing []Override) SimulationResult
func selectorsCompatible(a, b Selector) bool
func datesOverlap(aEff time.Time, aExp *time.Time, bEff time.Time, bExp *time.Time) bool
func affectedScope(sel Selector) AffectedScope
```

### Create: `internal/domain/simulate_test.go`

Pure unit tests — no DB. Cover:
- Spec 2 proposed shadows spec 1 existing ✓
- Spec 2 proposed conflicts with another spec 2 (same date) ✓  
- Spec 2 proposed shadows spec 2 with older effectiveDate ✓
- Incompatible selectors → neither shadow nor conflict ✓
- Non-overlapping dates → neither shadow nor conflict ✓
- Wildcard proposed (spec 0) cannot shadow anything; can conflict ✓
- `affectedScope` generates correct description for various selector combos ✓

### Modify: `internal/service/override.go`

Add to `OverrideService` interface:
```go
Simulate(ctx context.Context, req SimulateRequest) (*domain.SimulationResult, error)
```

Add `SimulateRequest` DTO (same shape as `CreateOverrideRequest` minus `status`/`description`).

Implementation:
1. `validateStepTrait(req.StepKey, req.TraitKey)` — reuse existing helper (line ~120)
2. `parseDates(req.EffectiveDate, req.ExpiresDate)` — reuse existing helper (line ~180)
3. `json.Unmarshal` + `domain.NormalizeTraitValue` — reuse existing pattern
4. `s.repo.List(ctx, OverrideFilter{StepKey: &req.StepKey, TraitKey: &req.TraitKey, PageSize: 200})` — fetch candidates
5. Filter out archived in Go: `if o.Status != "archived"`
6. Build `domain.Override{ID: "", Status: ""}` from request
7. Return `domain.Simulate(proposed, nonArchived)`

### Modify: `internal/controller/override.go`

Add `Simulate(c echo.Context) error` to `OverrideHandler` interface.

Handler: bind `SimulateRequest`, call `h.svc.Simulate`, return 200 JSON.
Error from service → 400 (validation) or 500 (DB).

### Modify: `internal/controller/routes.go`

```go
api.GET("/overrides/conflicts", h.GetConflicts)
api.POST("/overrides/simulate", h.Simulate)  // ← add here, BEFORE /:id
api.GET("/overrides/:id", h.GetByID)
```

No actor middleware — simulate is read-only.

---

## Verification

```bash
# 1. Build
go build ./... && echo "OK"

# 2. Domain unit tests
go test ./internal/domain/... -v -run "TestSimulate"

# 3. Smoke: proposed spec-2 override for FL+Chase
curl -s -X POST http://localhost:8082/api/overrides/simulate \
  -H 'Content-Type: application/json' \
  -d '{
    "stepKey": "file-complaint",
    "traitKey": "slaHours",
    "selector": { "state": "FL", "client": "Chase" },
    "value": 72,
    "effectiveDate": "2026-01-01"
  }' | jq '{scope: .affectedScope.description, shadows: (.shadowedOverrides|length), conflicts: (.newConflicts|length)}'

# 4. Wildcard proposed (spec 0) — cannot shadow anything
curl -s -X POST http://localhost:8082/api/overrides/simulate \
  -H 'Content-Type: application/json' \
  -d '{"stepKey":"file-complaint","traitKey":"slaHours","selector":{},"value":999,"effectiveDate":"2025-01-01"}' \
  | jq '.shadowedOverrides | length'
# Expected: 0 (spec-0 beats nothing)

# 5. No actor header required (read-only route)
curl -s -o /dev/null -w "%{http_code}" -X POST http://localhost:8082/api/overrides/simulate \
  -H 'Content-Type: application/json' \
  -d '{"stepKey":"file-complaint","traitKey":"slaHours","selector":{},"value":1,"effectiveDate":"2026-01-01"}'
# Expected: 200
```
