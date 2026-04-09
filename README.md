<h1 align="center">Rules Resolution Service</h1>

---
<p align="center">
  <img src="https://github.com/fardinabir/rules-resolution-svc/actions/workflows/test.yml/badge.svg" alt="Test">
  <img src="https://github.com/fardinabir/rules-resolution-svc/actions/workflows/reviewdog.yml/badge.svg" alt="Lint">
  <img src="https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go" alt="Go Version">
  <img src="https://img.shields.io/badge/Docker-Compose-2496ED?logo=docker&logoColor=white" alt="Docker Compose">
</p>
<p align="center">
  A Go service that resolves foreclosure workflow configuration for a given case context using a specificity-based override cascade — the Configuration Spine for Pearson Specter Litt. Given a case's dimensions (state, client, investor, case type), the service determines the exact deadlines, fees, required documents, assigned roles, and document templates for each step in the workflow. Override rules are ranked by specificity — the most precisely targeted rule always wins — with full audit history, conflict detection, and a debug-friendly explain trace.
</p>



---

## Architecture

```
Controller  →  Service  →  Repository  →  PostgreSQL
                                ↑
                          Cache (Redis)
```

Strict layered design with interfaces at every boundary. Dependency injection wired at the composition root (`internal/server/api.go`). The resolution algorithm is a **pure function** in `internal/domain/resolver.go` — no DB, no cache, no side effects — making it independently testable.

```
.
├── cmd/                    # CLI entrypoints (server, migrate)
├── internal/
│   ├── controller/         # HTTP handlers, validation, route registration
│   ├── service/            # Business logic, orchestration
│   ├── repository/         # DB queries (raw SQL via GORM) + Redis decorators
│   ├── domain/             # Pure domain types and resolution algorithm
│   ├── cache/              # Cache interface, Redis impl, NoopCache fallback
│   ├── model/              # Config types
│   ├── server/             # Echo engine setup, DI wiring, middleware
│   └── db/                 # DB connection, migration runner
├── migrations/ddl/         # PostgreSQL schema migrations
├── scripts/seed/           # Standalone seed binary
├── sr_backend_assignment_data/  # steps.json, defaults.json, overrides.json, test_scenarios.json
├── config.yaml             # Local config
├── config.docker.yaml      # Docker Compose config
└── docker-compose.yml      # Postgres + Redis + app
```

---

## Running the Service

### Docker Compose

Starts Postgres, Redis, and the app. No local Go or Postgres installation required.

```bash
# 1. Start all services
make start

# 2. Verify
curl http://localhost:8082/api/health
```

The app waits for Postgres and Redis health checks before starting. Migrations run automatically on startup inside the container.

| Endpoint | URL |
|---|---|
| API | `http://localhost:8082` |
| Swagger UI | `http://localhost:1315/swagger/index.html` |

### Configuration

Edit `config.yaml` for local, `config.docker.yaml` for Docker:

```yaml
apiServer:
  port: 8082
postgreSQL:
  host: localhost
  port: 5432
  user: postgres
  password: postgres
  dbname: user
redis:
  host: localhost
  port: 6379
defaultActor: admin@rrs.lawsuite.com
```

---


## Testing

### Go tests (unit + integration)

Integration tests run against a real Postgres database (auto-provisioned). The `test_scenarios.json` file is the acceptance test suite — all 12 scenarios must pass.

```bash
# Provision test DB, run all tests with coverage
make test

# CI mode (generates coverage.out)
make test-ci
```

### CURL-based API test (Easier)

A shell script exercises every endpoint end-to-end against a running service:

```bash
# Start the service first (make serve or docker-compose up -d)
bash scripts/test-all-apis.sh
```

The script tests health, single resolve, explain, bulk resolve, override CRUD, conflict detection, audit history, and X-Actor middleware — with pass/fail output for each case.

---


## How to Resolve a Case Configuration

### Single resolve

POST the case context — the four dimensions that describe a case:

```bash
curl -s -X POST http://localhost:8082/api/resolve \
  -H 'Content-Type: application/json' \
  -d '{
    "state":    "FL",
    "client":   "Chase",
    "investor": "FNMA",
    "caseType": "judicial"
  }' | jq .
```

**Response:**

```json
{
  "context": { "state": "FL", "client": "Chase", "investor": "FNMA", "caseType": "judicial" },
  "resolvedAt": "2026-04-09T10:00:00Z",
  "steps": {
    "file-complaint": {
      "slaHours":          { "value": 168,    "source": "override", "overrideId": "ovr-034" },
      "feeAmount":         { "value": 85000,  "source": "default" },
      "feeAuthRequired":   { "value": true,   "source": "override", "overrideId": "ovr-021" },
      "assignedRole":      { "value": "attorney", "source": "default" },
      "requiredDocuments": { "value": ["complaint", "lis_pendens"], "source": "default" },
      "templateId":        { "value": "complaint-fl-v2", "source": "override", "overrideId": "ovr-008" }
    }
  }
}
```

**Interpreting the response:**
- `source: "default"` — the value came from the baseline defaults table (specificity 0).
- `source: "override"` — a more specific rule matched; `overrideId` tells you which one.
- Each step/trait cell resolves independently.

### Explain — why did this value win?

```bash
curl -s -X POST http://localhost:8082/api/resolve/explain \
  -H 'Content-Type: application/json' \
  -d '{"state":"FL","client":"Chase","investor":"FNMA","caseType":"judicial"}' | jq .
```

Returns all 36 trace objects showing every candidate override evaluated per cell, its specificity, and why it was selected or shadowed.

### Resolve with a historical date

```bash
curl -s -X POST http://localhost:8082/api/resolve \
  -H 'Content-Type: application/json' \
  -d '{"state":"FL","client":"Chase","investor":"FNMA","caseType":"judicial","asOfDate":"2025-06-15"}'
```

Only overrides effective on or before `2025-06-15` are considered.

### Bulk resolve (up to 50 contexts, one DB round-trip)

```bash
curl -s -X POST http://localhost:8082/api/resolve/bulk \
  -H 'Content-Type: application/json' \
  -d '{
    "contexts": [
      {"state":"FL","client":"Chase","investor":"FNMA","caseType":"judicial"},
      {"state":"NY","client":"WellsFargo","investor":"FHA","caseType":"judicial"}
    ]
  }'
```

---

## Makefile Reference

| Command | Effect |
|---|---|
| `make serve` | Start the API server locally |
| `make migrate` | Run DDL migrations against local DB |
| `make seed` | Seed steps, defaults, and 49 overrides |
| `make setup` | migrate + seed in one step |
| `make test` | Reset test DB and run all tests |
| `make test-ci` | Test with coverage report |
| `make start` | `docker-compose up -d` |
| `make stop` | `docker-compose down` |
| `make clear` | Tear down containers + volumes |
| `make lint` | Run golangci-lint |
| `make fmt` | Tidy, lint-fix, swag fmt |
| `make swagger` | Regenerate Swagger spec and HTML |

---

## API Reference

| Method | Path | Auth | Description |
|---|---|---|---|
| `GET` | `/api/health` | — | Health check |
| `POST` | `/api/resolve` | — | Resolve full config for a case context |
| `POST` | `/api/resolve/explain` | — | Resolution trace — why each value was chosen |
| `POST` | `/api/resolve/bulk` | — | Resolve up to 50 contexts in one call |
| `GET` | `/api/overrides` | — | List overrides (filterable) |
| `GET` | `/api/overrides/conflicts` | — | Detect conflicting override pairs |
| `GET` | `/api/overrides/:id` | — | Get single override |
| `GET` | `/api/overrides/:id/history` | — | Audit history for an override |
| `POST` | `/api/overrides` | X-Actor | Create override |
| `PUT` | `/api/overrides/:id` | X-Actor | Update override |
| `PATCH` | `/api/overrides/:id/status` | X-Actor | Transition status (draft→active→archived) |

Mutation endpoints (`POST`, `PUT`, `PATCH`) require an `X-Actor: <email>` header identifying the operator. If absent, `defaultActor` from config is used.

---

## Swagger UI

The service hosts interactive API documentation on a separate port from the API server.

**Access:** `http://localhost:1315/swagger/index.html`

Available in Swagger UI:
- All 11 endpoints grouped by tag (Resolve, Overrides, Health)
- Request body schemas with field descriptions and example values
- Response schemas for all success and error shapes
- Live execute requests directly from the browser

**Regenerate after code changes:**

```bash
make swagger   # runs swag init → generates docs/ → builds swagger.html
```
---

## Closing Note

This service is a **demonstration implementation** of a specificity-based rules resolution engine for foreclosure case configuration — not a production-ready foreclosure settlement platform.

What it demonstrates:
- A clean **cascading override resolution algorithm** where the most precisely targeted rule always wins, modeled after CSS specificity but applied to business configuration.
- A **template API server** for a foreclosure workflow service, covering the full lifecycle of override management: creation, audit history, conflict detection, and effective dating.
- Idiomatic Go layered architecture, with a pure domain layer that is testable without any infrastructure.

To be used in a real case-solving context, this service would need significant extension — additional configuration dimensions, workflow versioning, runtime plan materialization, cross-step dependency resolution, role-based access control, and integration with the broader case management platform. The current scope intentionally covers the algorithmic core and API surface area, leaving those production concerns as clear extension points.
