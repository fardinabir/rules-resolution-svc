# Senior Backend Engineer — Take-Home Assignment

## Rules Resolution Service

---

## Context

Pearson Specter Litt is an AI-native SaaS platform for foreclosure law firms. These firms manage hundreds to thousands of active legal cases simultaneously, each progressing through jurisdiction-specific milestones — title search, filing, service, judgment, sale — with procedures, deadlines, required documents, and fees that vary along multiple dimensions.

Foreclosure is not one process. What happens depends on the **state** (Florida has different rules than New York), the **servicer** (Chase has different requirements than Wells Fargo), the **investor** who owns the loan (FHA loans have different rules than conventional), and whether the case is **judicial** or **non-judicial**. A single canonical workflow must produce different behavior depending on where it sits in this multi-dimensional configuration space.

The **Configuration Spine** is the architectural centerpiece that makes this possible. Instead of maintaining hundreds of process variants, we maintain one canonical template per case type and use **override records** to specify what changes along each dimension. The Rules Resolution Service resolves these overrides at runtime — given a case's context, it determines the exact deadlines, documents, fees, and templates that apply.

Your assignment is to build this Rules Resolution Service.

---

## What to Build

A Go service with a REST API that resolves configuration for foreclosure cases by applying specificity-ranked override records against a set of defaults.

The core problem: given a case context like `{state: "FL", client: "Chase", investor: "FHA", caseType: "FC-Judicial"}`, determine the resolved configuration for each step in the workflow — deadlines, required documents, fees, assigned roles, and document templates.

---

## Technical Stack

- **Required:** Go 1.21+, PostgreSQL
- **Your choice:** Any Go libraries. We evaluate architecture, not dependency choices.
- **No frontend needed.** This is a pure backend service with a REST API.

---

## Domain Background

### The Override Model

Configuration is resolved through a **specificity cascade** — similar in concept to CSS specificity but applied to business rules.

There are **four configuration dimensions:**

| Dimension | Examples | Description |
|---|---|---|
| `state` | FL, IL, TX, NY, OH | Jurisdiction — determines legal procedures, timelines, required filings |
| `client` | Chase, WellsFargo, Nationstar | The loan servicer — each has different requirements, fee schedules, templates |
| `investor` | FannieMae, FreddieMac, FHA, VA | The loan owner — government-backed loans have additional requirements |
| `caseType` | FC-Judicial, FC-NonJudicial | Whether the case goes through court (judicial) or a trustee (non-judicial) |

Every override record **pins zero or more of these dimensions.** An override that pins `{state: "FL"}` applies to all Florida cases regardless of client, investor, or case type. An override that pins `{state: "FL", client: "Chase", investor: "FHA"}` only applies to FHA loans serviced by Chase in Florida.

### Specificity Resolution

When multiple overrides match a case context, the **most specific one wins.** Specificity is the count of pinned dimensions:

| Override Selector | Specificity | Beats |
|---|---|---|
| `{}` (default) | 0 | nothing |
| `{state: "FL"}` | 1 | defaults |
| `{state: "FL", client: "Chase"}` | 2 | state-only overrides |
| `{state: "FL", client: "Chase", investor: "FHA"}` | 3 | state+client overrides |
| `{state: "FL", client: "Chase", investor: "FHA", caseType: "FC-Judicial"}` | 4 | everything except another specificity-4 |

If two overrides have **equal specificity**, the one with the more recent `effectiveDate` wins. If still tied, this is a **conflict** that your system should detect and report.

### Steps and Traits

The workflow has **6 canonical steps.** Each step has **6 configurable traits** that can be overridden:

| Trait | Type | Description |
|---|---|---|
| `slaHours` | integer | Deadline in hours from step activation |
| `requiredDocuments` | string array | Documents that must be collected/filed for this step |
| `feeAmount` | integer | Fee in cents earned for completing this step |
| `feeAuthRequired` | boolean | Whether the fee requires pre-authorization from the servicer |
| `assignedRole` | string | Who performs this step (`"processor"` or `"attorney"`) |
| `templateId` | string | Document template to use for generated documents in this step |

Each step/trait combination resolves independently. A case might match one override for `file-complaint.slaHours` and a different override for `file-complaint.requiredDocuments`.

---

## Data Files

All seed data is in `sr_backend_assignment_data/`.

| File | Description |
|---|---|
| `steps.json` | The 6 canonical workflow steps with their position order |
| `defaults.json` | Default values for every step/trait combination (specificity 0) |
| `overrides.json` | 49 override records with varying selectors and specificities (1-4) |
| `test_scenarios.json` | 12 test scenarios with input case contexts and expected resolved values |

Load these into PostgreSQL as seed data. The override records and defaults are your initial configuration — your system should be able to add, modify, and deactivate overrides via the API.

**Note on seed data format:** The seed files use a simplified format. Your database schema will have additional fields (`specificity` computed from the selector, `createdAt`/`updatedAt` timestamps, `updatedBy`). Generate sensible defaults for these during seeding.

---

## Requirements

### Must Have

These are the core requirements. All must be implemented.

**1. Resolution API**

`POST /api/resolve` — Given a case context, resolve the full configuration.

**Request:**
```json
{
  "state": "FL",
  "client": "Chase",
  "investor": "FHA",
  "caseType": "FC-Judicial"
}
```

**Response:**
```json
{
  "context": {
    "state": "FL",
    "client": "Chase",
    "investor": "FHA",
    "caseType": "FC-Judicial"
  },
  "resolvedAt": "2026-03-16T14:30:00Z",
  "steps": {
    "title-search": {
      "slaHours": { "value": 720, "source": "default" },
      "requiredDocuments": { "value": ["title_commitment", "tax_certificate"], "source": "default" },
      "feeAmount": { "value": 35000, "source": "default" },
      "feeAuthRequired": { "value": true, "source": "override", "overrideId": "ovr-030" },
      "assignedRole": { "value": "processor", "source": "default" },
      "templateId": { "value": "title-review-standard-v1", "source": "default" }
    },
    "file-complaint": {
      "slaHours": { "value": 168, "source": "override", "overrideId": "ovr-034" },
      "...": "..."
    }
  }
}
```

Each resolved trait must indicate whether it came from a default or an override, and if an override, which one.

**2. Explain Endpoint**

`POST /api/resolve/explain` — Same input as `/resolve`, but returns a detailed resolution trace showing which overrides were considered, which matched, and why the winner was chosen.

**Response (per trait):**
```json
{
  "step": "file-complaint",
  "trait": "slaHours",
  "resolvedValue": 168,
  "resolvedFrom": {
    "overrideId": "ovr-034",
    "selector": { "state": "FL", "client": "Chase", "investor": "FHA" },
    "specificity": 3,
    "effectiveDate": "2025-09-01"
  },
  "candidates": [
    {
      "overrideId": "ovr-034",
      "selector": { "state": "FL", "client": "Chase", "investor": "FHA" },
      "specificity": 3,
      "effectiveDate": "2025-09-01",
      "value": 168,
      "outcome": "SELECTED — highest specificity"
    },
    {
      "overrideId": "ovr-020",
      "selector": { "state": "FL", "client": "Chase" },
      "specificity": 2,
      "effectiveDate": "2025-06-01",
      "value": 240,
      "outcome": "SHADOWED — lower specificity (2 < 3)"
    },
    {
      "overrideId": "ovr-001",
      "selector": { "state": "FL" },
      "specificity": 1,
      "effectiveDate": "2025-01-01",
      "value": 360,
      "outcome": "SHADOWED — lower specificity (1 < 3)"
    }
  ]
}
```

This is the debugging tool. When someone asks "why does this case have a 7-day deadline instead of 30?", this endpoint answers the question.

**3. Override CRUD API**

- `GET /api/overrides` — List all overrides. Support filtering by `stepKey`, `traitKey`, `state`, `client`, `investor`, `caseType`, `status`.
- `GET /api/overrides/{id}` — Get a single override.
- `POST /api/overrides` — Create a new override.
- `PUT /api/overrides/{id}` — Update an override.
- `PATCH /api/overrides/{id}/status` — Activate, deactivate, or archive an override.

Override structure:
```json
{
  "id": "ovr-057",
  "stepKey": "file-complaint",
  "traitKey": "slaHours",
  "selector": {
    "state": "FL",
    "client": "Chase"
  },
  "value": 240,
  "specificity": 2,
  "effectiveDate": "2025-06-01",
  "expiresDate": null,
  "status": "active",
  "description": "Chase Florida filing deadline — tighter than state default",
  "createdAt": "2025-05-15T10:00:00Z",
  "createdBy": "admin@pearsonspecter.com",
  "updatedAt": "2025-05-15T10:00:00Z"
}
```

**Specificity must be computed** from the selector, not provided by the caller. An override with `{state: "FL", client: "Chase"}` always has specificity 2.

**4. PostgreSQL Schema Design**

This is a key evaluation area. Design a schema that:
- Stores steps, defaults, and overrides efficiently.
- Allows efficient querying of overrides that match a given case context.
- Handles the multi-dimensional selector (some dimensions null/unpinned, some pinned).
- Supports effective date ranges (an override applies from `effectiveDate` until `expiresDate`, or indefinitely if `expiresDate` is null).
- Tracks status (`draft`, `active`, `archived`).
- Includes audit fields (`createdAt`, `createdBy`, `updatedAt`, `updatedBy`).

Include your schema as migration files or a `schema.sql` in the repo.

**5. Conflict Detection**

`GET /api/overrides/conflicts` — Identify overrides that conflict with each other.

Two overrides conflict when they target the same step/trait, have the same specificity, overlapping effective date ranges, and selectors where every non-null dimension matches (meaning they would both match the same case context and neither shadows the other).

Return conflicts as pairs with an explanation of why they conflict:

```json
{
  "conflicts": [
    {
      "overrideA": "ovr-058",
      "overrideB": "ovr-059",
      "stepKey": "file-complaint",
      "traitKey": "slaHours",
      "reason": "Same step/trait, same specificity (2), overlapping effective dates, identical selectors {state: FL, caseType: FC-Judicial}"
    }
  ]
}
```

Note: The seed data is conflict-free. The conflict detection endpoint should return an empty list for the initial data, and detect conflicts when new overrides are created via the CRUD API.

**6. Effective Dating**

Overrides have an `effectiveDate` and an optional `expiresDate`. The resolution API must accept an optional `asOfDate` parameter:

```json
{
  "state": "FL",
  "client": "Chase",
  "investor": "FHA",
  "caseType": "FC-Judicial",
  "asOfDate": "2025-06-15"
}
```

Only overrides that are `active` (not `draft` or `archived`) and where `effectiveDate <= asOfDate` and (`expiresDate` is null OR `expiresDate > asOfDate`) should be considered. If `asOfDate` is not provided, use the current date.

`draft` overrides are stored but never participate in resolution — they represent proposed changes that haven't been approved. `archived` overrides are historical records that are no longer in effect.

**7. Audit Trail**

All changes to overrides (create, update, status change) must be recorded with:
- Who made the change
- When
- What changed (before/after for updates)

`GET /api/overrides/{id}/history` — Return the change history for an override.

---

### Nice to Have

Pick **any** of these that interest you. These are not required, but they let you demonstrate depth in areas you're strongest in. We'd rather see one done well than three done superficially.

- **Bulk Resolution:** `POST /api/resolve/bulk` — Resolve configuration for multiple case contexts in one call. Demonstrate efficient querying (batch the override lookups rather than N+1 per context).

- **Configuration Diff:** `POST /api/resolve/diff` — Given two case contexts, show what differs between their resolved configurations and which overrides cause each difference. Useful for answering "why does this case behave differently than that one?"

- **Override Simulation:** `POST /api/overrides/simulate` — Submit a proposed override (without saving it) and see what cases it would affect, which existing overrides it would shadow, and whether it creates new conflicts.

- **Validation Rules:** When creating or updating an override, validate that the dimension values are legitimate (e.g., `state` must be a valid US state abbreviation, `client` must be a known servicer). Return clear validation errors.

- **Caching Strategy:** Resolution involves querying overrides on every request. Implement a caching layer with appropriate invalidation when overrides change.

- **Containerized Deployment:** Provide a `Dockerfile` for your service and a `docker-compose.yml` that starts your service and PostgreSQL together.

---

## Constraints

- **Time budget:** We expect this to take **8-10 hours** of focused work. Do not over-polish. We would rather see clean domain modeling with a correct resolution algorithm than a feature-complete CRUD API with naive specificity handling.
- **Test scenarios are your acceptance tests.** The `test_scenarios.json` file contains 12 scenarios with expected resolved values. Your resolution endpoint should produce results that match these expectations. Use them to verify your implementation.
- **PostgreSQL is required.** A `docker-compose.yml` for PostgreSQL is included for convenience.

---

## Deliverables

1. **A GitHub repository** (public or private with access granted) containing the working service with a `README.md` that includes:
   - Instructions to run the service (including database setup and seed data loading)
   - How to resolve a case configuration and interpret the result
   - Brief description of the architecture

2. **`APPROACH.md`** (maximum 2 pages) covering:
   - Your schema design decisions — how you modeled the multi-dimensional selector, why you chose that approach, and what trade-offs you considered.
   - Your resolution algorithm — how overrides are queried, filtered, and ranked. Complexity analysis if applicable.
   - How you handle edge cases: equal specificity ties, expired overrides, draft overrides, conflicting overrides.
   - What you would change, refactor, or add if you had more time.
   - Which "Nice to Have" items you chose and why.

3. **Database schema** — as migration files or `schema.sql`, included in the repo.

---

## Evaluation Criteria

| Area | Weight | What We Evaluate |
|---|---|---|
| **Domain Modeling & Resolution** | 30% | Does the resolution algorithm correctly implement specificity-based cascading? Does it handle edge cases (ties, effective dates, draft status)? Is the algorithm clean and understandable? |
| **PostgreSQL Schema Design** | 25% | Does the schema model the multi-dimensional override space efficiently? Are queries for matching overrides performant? Is the effective dating model sound? Audit trail clean? |
| **Go & API Design** | 20% | Idiomatic Go. Clean error handling. Well-structured API. Proper use of interfaces and type safety. RESTful conventions followed. |
| **Explain & Conflict Detection** | 15% | Is the explain trace useful for debugging? Does conflict detection catch real conflicts without false positives? Would an operator trust these tools? |
| **Code Quality & Documentation** | 10% | Readable, well-organized code. Clear APPROACH.md that demonstrates deep thinking about the domain. Schema decisions are justified. |

---

## Why This Assignment

This is a simplified version of the actual **Configuration Spine** at the heart of the Pearson Specter Litt platform. The production system has 8 configuration dimensions (not 4), 40-80 steps per workflow template (not 6), and dozens of traits per step. It also handles workflow versioning, runtime plan materialization, and cross-step dependency resolution.

The senior backend engineer who owns this layer must understand multi-dimensional configuration spaces, build resolution algorithms that operators can debug, and design PostgreSQL schemas that make complex queries efficient. Every bug in this layer affects every case in the system — correctness is not optional.

We want to see how you model this kind of problem — not just "look up some config," but build a system that handles the combinatorial reality of jurisdiction-specific legal workflows.

Good luck. We look forward to reviewing your work.
