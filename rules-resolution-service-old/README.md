# Rules Resolution Service

A Go service that resolves configuration for foreclosure cases by applying specificity-ranked override records against a set of defaults.

---

## Prerequisites

- Go 1.21+
- Docker + Docker Compose

---

## Quick Start (Docker Compose)

```bash
# Start PostgreSQL + service
docker compose up

# In another terminal — seed the database
DATABASE_URL=postgres://rrs:rrs@localhost:5432/rrs?sslmode=disable \
  DATA_DIR=../sr_backend_assignment_data \
  go run ./scripts/seed/main.go
```

The service auto-runs migrations on startup. The API is available at `http://localhost:8080` and Swagger UI at `http://localhost:8081/swagger/index.html`.

---

## Local Development

```bash
# 1. Start only PostgreSQL
docker compose up -d postgres

# 2. Run the service (API on :8080, Swagger UI on :8081)
DATABASE_URL=postgres://rrs:rrs@localhost:5432/rrs?sslmode=disable \
  PORT=8080 \
  SWAGGER_PORT=8081 \
  go run ./cmd/server

# 3. Seed data (in another terminal)
DATABASE_URL=postgres://rrs:rrs@localhost:5432/rrs?sslmode=disable \
  DATA_DIR=../sr_backend_assignment_data \
  go run ./scripts/seed/main.go
```

Swagger UI: http://localhost:8081/swagger/index.html

---

## Running Tests

```bash
go test ./internal/domain/... -v
```

The domain package tests validate the resolution algorithm against fixture data from `sr_backend_assignment_data/`.

---

## API Reference

### Resolve a case configuration

```bash
curl -s -X POST http://localhost:8080/api/resolve \
  -H 'Content-Type: application/json' \
  -d '{
    "state": "FL",
    "client": "Chase",
    "investor": "FHA",
    "caseType": "FC-Judicial"
  }' | jq .
```

Optional `asOfDate` for time-travel resolution:
```json
{
  "state": "FL",
  "client": "Chase",
  "investor": "FHA",
  "caseType": "FC-Judicial",
  "asOfDate": "2025-06-15"
}
```

**Reading the response:** Each step/trait shows `source: "default"` or `source: "override"`. When source is `override`, `overrideId` identifies which rule applied. A `conflict: true` flag means two rules tied and the system chose one deterministically — use the conflicts endpoint to investigate.

### Explain resolution (full trace)

```bash
curl -s -X POST http://localhost:8080/api/resolve/explain \
  -H 'Content-Type: application/json' \
  -d '{"state":"FL","client":"Chase","investor":"FHA","caseType":"FC-Judicial"}' | jq .
```

Returns every candidate override considered per trait, with `outcome` explaining why each was selected or shadowed.

### Override CRUD

```bash
# List all active overrides for a specific step
curl "http://localhost:8080/api/overrides?stepKey=file-complaint&status=active"

# Get a single override
curl http://localhost:8080/api/overrides/ovr-034

# Create a new override
curl -X POST http://localhost:8080/api/overrides \
  -H 'Content-Type: application/json' \
  -d '{
    "stepKey": "file-complaint",
    "traitKey": "slaHours",
    "selector": {"state": "IL", "client": "Chase"},
    "value": 192,
    "effectiveDate": "2026-01-01",
    "status": "draft",
    "description": "Chase Illinois tighter deadline",
    "createdBy": "admin@example.com"
  }'

# Activate a draft override
curl -X PATCH http://localhost:8080/api/overrides/{id}/status \
  -H 'Content-Type: application/json' \
  -d '{"status":"active","updatedBy":"admin@example.com"}'

# Check for conflicts
curl http://localhost:8080/api/overrides/conflicts

# View change history
curl http://localhost:8080/api/overrides/ovr-034/history
```

---

## Architecture

The service is structured in three layers: **domain** (pure resolution logic, zero DB dependency), **repository** (PostgreSQL queries via pgx), and **service + API** (orchestration and HTTP). The core resolution algorithm is a pure function in `internal/domain/resolver.go` that takes pre-fetched override candidates and defaults and returns a resolved config — this makes it fast, deterministic, and independently testable.

Override selectors are stored as four nullable columns (`state`, `client`, `investor`, `case_type`) rather than a JSON blob, enabling efficient `IS NULL OR col = $x` predicate matching in a single covering index. A single SQL query per resolve call fetches all matching overrides ordered by specificity, avoiding N+1 queries across the 6×6 step/trait grid.

---

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `DATABASE_URL` | `postgres://rrs:rrs@localhost:5432/rrs?sslmode=disable` | PostgreSQL connection string |
| `PORT` | `8080` | API HTTP listen port |
| `LOG_LEVEL` | `info` | Logging level (`info` or `debug`) |
| `MIGRATIONS_PATH` | `file://migrations` | Path to migration files |
| `SWAGGER_ENABLE` | `true` | Enable Swagger UI server |
| `SWAGGER_PORT` | `8081` | Swagger UI listen port |
| `DATA_DIR` | `../../sr_backend_assignment_data` | Path to seed data JSON files (seed script only) |
