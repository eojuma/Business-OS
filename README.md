# Business OS

An operating system for hardware stores in Africa. Replaces notebooks, spreadsheets, and WhatsApp with a natural-language interface for inventory, sales, customer credit, and financial management.

---

## What works right now

Milestones 1–3 are done and tested live end to end — not just compiling, actually running against a real Postgres database and a real browser. Signup, products, inventory, sales, customer credit, daily reports, suppliers, purchases, finance, and analytics all work through the actual frontend.

| Module | Backend | Frontend | Notes |
|---|---|---|---|
| Auth | ✅ | ✅ | Register, login, JWT issuing, business validation |
| Business | ✅ | ✅ | Create (public), get/update (scoped) |
| Products | ✅ | ✅ | Full CRUD, price stored as int64 cents |
| Inventory | ✅ | ✅ | Stock levels + movement ledger, atomic via DB transaction |
| Sales | ✅ | ✅ | Multi-item sales, decrements stock, customer credit, one transaction |
| Customers | ✅ | ✅ | Profiles, credit limit enforcement, balance queries |
| Reports | ✅ | ✅ | Daily sales aggregated via Postgres `SUM`/`GROUP BY` |
| Dashboard | — | ✅ | Today's sales, low-stock count, calls real endpoints |
| Suppliers | ✅ | ✅ | Profiles, outstanding balances, payments, CRUD |
| Purchases | ✅ | ✅ | Draft POs, goods receipt, stock & supplier debt updates |
| Finance | ✅ | ✅ | Expenses, profit, cash flow, date-filtered summaries |
| Notifications | ✅ | — | Low-stock alerts with severity & product context |
| Analytics | ✅ | ✅ | Overview, top-profit & slow-moving products |
| Assistant | ✅ | ✅ | Natural-language sale preview + explicit confirmation |

---

## Stack

- **Backend:** Go, Gin, GORM, PostgreSQL, Redis
- **Frontend:** Next.js 14 (App Router), React, Tailwind CSS
- **Auth:** JWT
- **Architecture:** Modular monolith
- **CI/CD:** GitHub Actions (build, test, lint, race detector, coverage)
- **Deployment:** Docker Compose (Postgres, Redis, backend, frontend all containerized)

---

## Project structure

```
Business-OS/
├── backend/
│   ├── cmd/api/main.go          # entrypoint, runs migrations on startup
│   ├── Dockerfile               # multi-stage Go build
│   └── internal/
│       ├── config/              # env config, loaded once
│       ├── router/              # wires all modules together
│       ├── shared/
│       │   ├── database/        # postgres + redis connections
│       │   ├── middleware/      # JWT auth, CORS, request logging
│       │   ├── response/        # consistent JSON envelopes
│       │   └── utils/           # money.go — cents ↔ decimal conversion
│       └── modules/
│           ├── auth/            # full implementation — the template
│           ├── business/
│           ├── products/
│           ├── inventory/
│           ├── sales/           # calls into inventory + products + customers
│           ├── customers/
│           ├── suppliers/
│           ├── purchases/
│           ├── finance/
│           ├── reports/         # read-only, aggregates via SQL
│           ├── analytics/
│           ├── notifications/
│           ├── assistant/
│           └── ...              # remaining modules
├── frontend/
│   ├── app/
│   │   ├── login/
│   │   ├── signup/              # combined business + owner signup
│   │   └── dashboard/           # guarded by layout.tsx
│   │       ├── layout.tsx       # redirects to /login if no token
│   │       ├── page.tsx         # dashboard home
│   │       ├── products/
│   │       ├── inventory/
│   │       ├── sales/
│   │       ├── customers/
│   │       ├── suppliers/
│   │       ├── purchases/
│   │       ├── finance/
│   │       ├── analytics/
│   │       ├── assistant/
│   │       ├── reports/
│   │       └── settings/
│   ├── lib/api.ts               # single axios client, JWT auto-attached
│   └── Dockerfile
├── docker-compose.yml
├── .env.example
└── Makefile
```

---

## The module pattern

Every module that owns data follows the same five files (see `auth/`, `business/`, or `products/` as the reference):

| File | Responsibility |
|---|---|
| `model.go` | GORM structs — the tables this module owns |
| `repository.go` | DB access only, behind an interface |
| `service.go` | Business logic, depends on `Repository` |
| `handler.go` | HTTP request/response, depends on `Service` |
| `routes.go` | Wires repo → service → handler → gin routes |

Modules never import each other directly. When one module needs another (e.g. `sales` needs `inventory` stock and `customers` credit limits), the pattern is:

1. The dependent module (`sales`) declares a **narrow interface** describing only what it needs (`InventoryMover`, `CustomerCharger`) — never the other module's full `Repository` or `Service`.
2. `routes.go` constructs the real implementation and wires it in via an **adapter** that also translates error types across the module boundary (e.g. `inventory.ErrInsufficientStock` → `sales.ErrInsufficientStock`), so neither module needs to import the other's error types directly.

See `sales/repository.go` and `sales/routes.go` for the reference implementation.

**Not every module needs all five files.** `reports` has no `model.go` — it queries other modules' tables directly rather than owning any data of its own. `assistant` calls into other modules' services rather than a database at all.

---

## CI/CD Pipeline

GitHub Actions workflow (`.github/workflows/ci.yml`) runs on every push and PR:

| Job | Purpose |
|-----|---------|
| `backend-build` | `go build ./...`, `go mod verify`, `go mod tidy -diff` |
| `backend-vet` | `go vet ./...` |
| `backend-fmt` | `gofmt -l .` |
| `backend-lint` | `golangci-lint` (25+ linters) |
| `backend-test` | `go test -race -coverprofile` (race detector + coverage) |
| `frontend-lint` | `npm run lint` (ESLint) |
| `frontend-typecheck` | `tsc --noEmit` |
| `frontend-build` | `npm run build` (Next.js production build) |
| `docker-build` | Verifies Docker images build on `main` pushes |
| `ci-summary` | Aggregates all job statuses, fails fast if any check fails |

Coverage uploaded to Codecov (non-blocking). Docker images verified on `main` pushes.

---

## Getting started

Environment:

```bash
cp .env.example .env
```

Start everything (Postgres, Redis, backend, frontend):

```bash
make up
docker compose ps
```

Or, for faster local iteration on the backend (no Docker rebuild per change):

```bash
docker compose up -d postgres redis
cd backend && go run ./cmd/api
```

Frontend dev server (hot reload):

```bash
cd frontend && npm install && npm run dev
```

Health check: `curl http://localhost:8080/health` should return `{"status":"ok"}`

**Note:** don't run the Docker frontend container and `npm run dev` at the same time — they'll fight over port 3000, and the CORS middleware only allows the origin set in `FRONTEND_URL` (`.env`), so a port mismatch will silently break login with a CORS error in the browser console.

---

## Try the working flow

The full loop — sign up, add a product, restock it, sell it, see it in reports — works end to end through the actual UI at `http://localhost:3000/signup`. Or via curl:

```bash
# 1. Create a business (public, no auth)
curl -X POST http://localhost:8080/api/v1/business \
  -H "Content-Type: application/json" \
  -d '{"name": "My Hardware Store", "phone": "0700000000"}'

# 2. Register a user against it (copy the id from step 1)
curl -X POST http://localhost:8080/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"business_id": "<id>", "name": "Owner", "email": "owner@example.com", "password": "password123"}'

# 3. Create a product (copy the token from step 2; price is in cents)
curl -X POST http://localhost:8080/api/v1/products \
  -H "Content-Type: application/json" -H "Authorization: Bearer <token>" \
  -d '{"name": "Cement 50kg", "unit": "bag", "price": 45050}'

# 4. Restock it (copy the product id from step 3)
curl -X POST http://localhost:8080/api/v1/inventory/movements \
  -H "Content-Type: application/json" -H "Authorization: Bearer <token>" \
  -d '{"product_id": "<product_id>", "type": "restock", "quantity": 50}'

# 5. Sell some
curl -X POST http://localhost:8080/api/v1/sales \
  -H "Content-Type: application/json" -H "Authorization: Bearer <token>" \
  -d '{"items": [{"product_id": "<product_id>", "quantity": 3}]}'

# 6. See it in the daily report
curl -H "Authorization: Bearer <token>" \
  http://localhost:8080/api/v1/reports/daily-sales
```

---

## Conventions worth knowing

- **Money is always `int64` cents, never `float64`.** Prevents floating-point rounding drift across sales/inventory/reports. Convert to/from a decimal string only at the UI boundary — see `shared/utils/money.go` (backend) and the `formatMoney` helper in each frontend page.
- **Every protected route pulls `business_id` from the JWT**, never trusts one from the request body — see `middleware.CurrentBusinessID`. This is what prevents one store from reading or editing another store's data.
- **Cross-module writes that must succeed or fail together use a single DB transaction** — see `inventory.RecordMovementTx` and `sales.CreateSale`, which writes a sale, its line items, stock movements, and a customer credit charge all inside one `db.Transaction(...)` call.

---

## Database migrations

Versioned SQL migrations live in `backend/internal/shared/migrations/sql` and are embedded into the API binary. Pending migrations run automatically at startup and are recorded in `schema_migrations`.

Run them explicitly with `make migrate-up`. `make migrate-down` rolls back only the most recently applied migration and is destructive, so use it deliberately.

---

## Production database and backups

Production runs on [Neon](https://neon.tech) Postgres. Set these on the backend host (the direct endpoint, not the `-pooler` one, since the API keeps a long-lived connection pool):

    DB_HOST=ep-xxxx.<region>.aws.neon.tech
    DB_PORT=5432
    DB_USER=<neon-user>
    DB_PASSWORD=<neon-password>
    DB_NAME=neondb
    DB_SSLMODE=require

Migrations apply automatically on startup. A local `.env` (gitignored) can point at Neon, but the Docker Compose database stays available for offline development.

### Scheduled backups

`.github/workflows/db-backup.yml` dumps the database daily (02:00 UTC) with `pg_dump`, gzips it, and uploads it to S3-compatible storage (AWS S3 or Cloudflare R2). Backups older than 30 days are pruned. It can also be run on demand from the Actions tab.

Configure these repository secrets (Settings → Secrets and variables → Actions):

| Secret | Purpose |
|---|---|
| `NEON_DATABASE_URL` | Full direct connection string, e.g. `postgresql://user:pass@ep-xxxx.aws.neon.tech/neondb?sslmode=require` |
| `S3_BUCKET` | Target bucket name |
| `S3_ENDPOINT_URL` | S3-compatible endpoint (R2 only; leave unset for AWS S3) |
| `AWS_ACCESS_KEY_ID` | Storage access key |
| `AWS_SECRET_ACCESS_KEY` | Storage secret key |
| `AWS_DEFAULT_REGION` | Region (e.g. `us-east-2`, or `auto` for R2) |

### Restore a backup

Download a dump from the bucket, then load it into the target database:

    gunzip -c businessos-YYYY-MM-DD.sql.gz | psql "postgresql://user:pass@host/db?sslmode=require"

The dump uses `--clean --if-exists`, so restoring over an existing database drops and recreates objects first.

---

## Build order

Matches the MVP scope in the product vision doc:

1. `business` ✅
2. `products` ✅
3. `inventory` ✅
4. `sales` ✅
5. `customers` ✅
6. Frontend (signup, dashboard, products, inventory, sales, customers) ✅
7. `reports` ✅
8. `assistant` sale entry flow ✅
9. `suppliers` + frontend ✅
10. `purchases` + frontend ✅
11. `finance` + frontend ✅
12. `analytics` + frontend ✅

---

## Known gaps

- CI/CD pipeline is in place with comprehensive checks (build, test, lint, race detector, coverage)
- Test coverage improving: supplier module now has comprehensive unit & integration tests (≥80% coverage)
- CORS origin is configurable via `FRONTEND_URL` but still assumes one single allowed origin — fine for one environment, will need revisiting for staging + production
- The frontend does not yet have a dedicated notifications screen; the authenticated API is available
- The assistant currently handles sale entry only; business Q&A, report generation, forecasting, and anomaly detection remain future work
- Not yet hardened for hosting: default JWT secret, default DB password, `GIN_MODE=debug` — see hosting checklist (tracked separately, not yet in this README)