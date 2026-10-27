# Business OS

Business OS is a multi-tenant operations platform for hardware retailers. It replaces notebooks, spreadsheets and ad-hoc messaging with a single system for products and stock, sales, customer credit, suppliers and purchases, expenses and profitability, reporting, and a natural-language assistant for day-to-day data entry.

**Live deployment:** <https://business-os-tqu3.onrender.com> — the frontend is served from Render and points at the API through `NEXT_PUBLIC_API_URL`.

The backend is a Go modular monolith (Gin + GORM + PostgreSQL + Redis). The frontend is a Next.js 14 App Router application. Data is scoped per business, money is stored in integer minor units, and every cross-module write runs in one database transaction.

---

## Contents

- [Capabilities](#capabilities)
- [Architecture](#architecture)
- [Repository layout](#repository-layout)
- [Backend module pattern](#backend-module-pattern)
- [Local development](#local-development)
- [Configuration reference](#configuration-reference)
- [Testing and quality gates](#testing-and-quality-gates)
- [API conventions](#api-conventions)
- [Database migrations](#database-migrations)
- [Deployment and operations](#deployment-and-operations)
- [Roadmap and known limitations](#roadmap-and-known-limitations)
- [Contributing](#contributing)
- [License](#license)

---

## Capabilities

| Area | API | UI | Notes |
|---|---|---|---|
| Authentication | Yes | Yes | Email/password and Google Identity Services; JWT issuing |
| Businesses | Yes | Yes | Public creation on signup; reads and updates are business-scoped |
| Products | Yes | Yes | CRUD, price stored as integer minor units |
| Inventory | Yes | Yes | Stock levels and an append-only movement ledger, written transactionally |
| Sales | Yes | Yes | Multi-line sales, stock decrement, customer credit charge in one transaction |
| Customers | Yes | Yes | Profiles, credit limits, balance tracking |
| Suppliers | Yes | Yes | Profiles, payment terms and due dates, outstanding balances, payments, archive/restore |
| Purchases | Yes | Yes | Draft purchase orders, sequential PO numbers, partial receiving, supplier debt updates |
| Finance | Yes | Yes | Expenses, profit, cash flow, date-filtered summaries |
| Reports | Yes | Yes | Daily sales and period aggregates computed in SQL |
| Analytics | Yes | Yes | Overview, top-profit and slow-moving products |
| Notifications | Yes | — | Low-stock alerts with severity and product context; polling API |
| Assistant | Yes | Yes | Natural-language sale preview with explicit confirmation |
| Receipt OCR | Yes | Yes | Photo to extracted line items, reviewed before saving |

---

## Architecture

| Layer | Technology |
|---|---|
| API | Go 1.25, Gin, GORM |
| Data | PostgreSQL 16, Redis 7 |
| Auth | JWT (HS256); Google ID-token verification for social sign-in |
| Web | Next.js 14 (App Router), React 18, TypeScript, Tailwind CSS 3.4 |
| Packaging | Docker and Docker Compose for local; container build verified in CI |
| CI | GitHub Actions: build, vet, formatting, lint, tests, frontend typecheck/build |

Design rules enforced across the codebase:

- **Money is `int64` in minor units.** No floating point is used for monetary values; conversion happens only at the presentation boundary.
- **Tenant scope comes from the token.** Protected handlers read the business identifier from the JWT via `middleware.CurrentBusinessID`; request bodies never supply it.
- **Cross-module writes are transactional.** Stock, sales, receivables and supplier balances are updated inside a single `db.Transaction`.
- **Modules communicate through narrow interfaces.** A module declares only the capability it needs from another module and adapters translate errors at the boundary.
- **Migrations are embedded and serialized.** They run at API startup under a PostgreSQL advisory lock so parallel processes cannot apply them concurrently.

---

## Repository layout

```
Business-OS/
├── backend/
│   ├── cmd/api/                     API entrypoint; migrations and CLI subcommands
│   ├── Dockerfile                   Multi-stage Go build
│   └── internal/
│       ├── config/                  Environment configuration
│       ├── router/                  Route wiring for all modules
│       ├── shared/
│       │   ├── database/            PostgreSQL and Redis clients
│       │   ├── middleware/          JWT authentication, CORS, request logging
│       │   ├── migrations/          Runner plus embedded SQL migrations
│       │   ├── response/            JSON envelope helpers
│       │   └── utils/               Money conversion helpers
│       └── modules/
│           ├── auth/                Reference module implementation
│           ├── business/
│           ├── products/
│           ├── inventory/
│           ├── sales/
│           ├── customers/
│           ├── suppliers/
│           ├── purchases/
│           ├── finance/
│           ├── reports/             Read-only SQL aggregation, no owned tables
│           ├── analytics/
│           ├── notifications/
│           ├── assistant/
│           └── receipts/
├── frontend/
│   ├── app/                         App Router pages
│   │   ├── login/
│   │   ├── signup/
│   │   └── dashboard/               Guarded by dashboard/layout.tsx
│   ├── components/                  Shared UI (`app-shell`, `google-signin`, ...)
│   ├── lib/api.ts                   Axios client with JWT interceptor
│   └── Dockerfile
├── docs/
│   ├── PRODUCT.md                   Product scope and module plan
│   └── OCR.md                       Receipt OCR flow
├── docker-compose.yml               Postgres, Redis, API, web
├── Makefile                         Common local tasks
├── mise.toml                        CI-equivalent local task definitions
└── .github/workflows/               CI and scheduled database backups
```

---

## Backend module pattern

A module that owns data is made of five files:

| File | Responsibility |
|---|---|
| `model.go` | GORM models for the tables the module owns |
| `repository.go` | Database access only, behind an interface |
| `service.go` | Business rules; depends on the repository interface |
| `handler.go` | HTTP binding, validation, error mapping |
| `routes.go` | Wires repository, service and handler into Gin routes |

When a module needs another module's behaviour, it declares a narrow interface for exactly that capability and `routes.go` supplies an adapter. This keeps modules independent: for example, `sales` depends on stock and customer-credit interfaces rather than importing the `inventory` or `customers` packages, and adapters translate error values across the boundary.

Not every module owns data. `reports` reads other modules' tables directly, and `assistant` orchestrates other modules' services.

---

## Local development

### Prerequisites

| Tool | Version | Notes |
|---|---|---|
| Go | 1.25 | `go.mod` pins the toolchain used by CI |
| Node.js | 20 | Used by the frontend and CI |
| Docker Engine + Compose | current | Runs PostgreSQL and Redis (and optionally the app) |
| GNU Make | any | Convenience targets |
| mise | optional | Reproduces the CI toolchain and checks (`mise run check`) |

### 1. Configure the environment

```bash
cp backend/.env.example backend/.env
cp frontend/.env.example frontend/.env
```

Defaults in the templates target Docker Compose. When the API runs directly on the host instead of inside Compose, set `DB_HOST=localhost` and `REDIS_URL=redis://localhost:6379`.

Two settings are common to both modes and worth checking before the first run:

- `JWT_SECRET` — replace the placeholder outside local development.
- `FRONTEND_URL` — comma-separated allowed browser origins; the API's CORS middleware rejects origins that are not listed.

### 2. Run the full stack

```bash
make up          # docker compose up -d
docker compose ps
curl http://localhost:8080/health
```

| Service | URL |
|---|---|
| Web | <http://localhost:3000> |
| API | <http://localhost:8080/api/v1> |
| Health | <http://localhost:8080/health> |

Migrations apply automatically when the API starts.

### 3. Run the backend and frontend on the host

Recommended for iterating on the API without rebuilding an image:

```bash
docker compose up -d postgres redis
make backend-run                 # go run ./cmd/api
```

Frontend development server with hot reload:

```bash
cd frontend
npm ci
npm run dev
```

Do not run the containerised web service and `npm run dev` at the same time: both bind port 3000, and a port mismatch against `FRONTEND_URL` surfaces as a CORS failure at sign-in.

### 4. Database lifecycle

```bash
make migrate-up      # apply pending migrations
make migrate-down    # roll back the most recently applied migration (destructive)
make seed            # load development seed data
```

### 5. Optional: Google sign-in

Google sign-in uses the Google Identity Services ID-token flow; no client secret is stored.

1. Create an OAuth 2.0 **Web application** client in the Google Cloud Console.
2. Add the frontend origins under **Authorized JavaScript origins** (`http://localhost:3000`, and the production origin).
3. Set the same client ID on both sides:

```bash
# backend/.env
GOOGLE_CLIENT_ID=<client-id>.apps.googleusercontent.com

# frontend/.env
NEXT_PUBLIC_GOOGLE_CLIENT_ID=<client-id>.apps.googleusercontent.com
```

Behaviour and security rules:

- A password account is never linked to a Google identity automatically. Google sign-in returns `409` with code `google_link_requires_password`; the user signs in with the password instead.
- If the Google subject behind an email changes (Workspace user recreated, address reassigned), sign-in returns `409` with code `google_account_conflict`. Re-linking is a deliberate operator action: verify the new owner out of band and clear the stale link before the user signs in again.

```sql
UPDATE users SET google_id = NULL WHERE email = 'user@example.com';
```

- An account with a password always uses password sign-in; an unknown address and a Google-only account return the same generic credentials error, so account type is not disclosed.
- With `GOOGLE_CLIENT_ID` empty, `/auth/google` returns `503` and the UI reports that Google sign-in is not configured.

### 6. Reproduce CI locally

```bash
mise run check        # backend + frontend checks, including integration tests
mise run check:unit   # fast checks without a database
mise run db:test:up   # start the test PostgreSQL instance
```

---

## Configuration reference

### Backend

| Variable | Default | Purpose |
|---|---|---|
| `APP_ENV` | `development` | Enables production-only validation when set to `production` |
| `APP_PORT` | `8080` | API listen port |
| `FRONTEND_URL` | `http://localhost:3000` | Comma-separated allowed CORS origins |
| `DB_HOST` | `localhost` | PostgreSQL host (`postgres` inside Compose) |
| `DB_PORT` | `5432` | PostgreSQL port |
| `DB_USER` / `DB_PASSWORD` / `DB_NAME` | `businessos` | PostgreSQL credentials and database |
| `DB_SSLMODE` | `disable` | `require` for managed PostgreSQL |
| `REDIS_URL` | `redis://localhost:6379` | Redis connection string (`redis://redis:6379` inside Compose) |
| `JWT_SECRET` | development placeholder | JWT signing key; must be set in production |
| `JWT_EXPIRY_HOURS` | `24` | Token lifetime |
| `GOOGLE_CLIENT_ID` | empty | Enables Google sign-in when set |
| `AI_PROVIDER` / `AI_MODEL` / `AI_BASE_URL` | Gemini defaults | OpenAI-compatible assistant provider |
| `AI_API_KEY` | empty | Required for `/assistant/*`; those routes return `500` without it |
| `AI_OCR_MODEL` | empty | Vision model for receipt OCR; falls back to `AI_MODEL` |

### Frontend

| Variable | Purpose |
|---|---|
| `NEXT_PUBLIC_API_URL` | API base URL including `/api/v1` (for example `http://localhost:8080/api/v1`) |
| `NEXT_PUBLIC_GOOGLE_CLIENT_ID` | Same client ID as `GOOGLE_CLIENT_ID`; inlined at build time |

---

## Testing and quality gates

Backend:

```bash
cd backend
go test ./...                                   # unit tests
TEST_DATABASE_URL="postgres://businessos:businessos@localhost:5432/businessos_test?sslmode=disable" \
  go test -race -timeout 30m ./...
```

Integration tests are opt-in: they skip when `TEST_DATABASE_URL` is unset. Each test runs in its own PostgreSQL schema and the pool is pinned to one connection so session-scoped `search_path` applies to every query. CI runs the race-enabled suite against a PostgreSQL 16 service and uploads coverage to Codecov.

Frontend:

```bash
cd frontend
npm run lint
npx tsc --noEmit
npm run build
```

CI pipeline (`.github/workflows/ci.yml`):

| Job | Purpose |
|---|---|
| Backend Build / Vet / Format / Lint | Compile, static analysis, `gofmt -l`, golangci-lint |
| Backend Test | `go test -race` against PostgreSQL, coverage upload |
| Frontend Lint / Type Check / Build | ESLint, `tsc --noEmit`, production build |
| Docker Build | Verifies the container image builds |
| CI Summary | Fails the run if any job failed |

---

## API conventions

- Base path: `/api/v1`. Protected routes require `Authorization: Bearer <token>`.
- Success envelope: `{"success": true, "data": ...}`; paginated list endpoints add pagination metadata under `meta`.
- Error envelope: `{"success": false, "error": "<message>"}`. Endpoints that expose a stable client contract add a machine-readable `code` (for example `workspace_required`, `google_account_conflict`); clients branch on the code, not the message text.
- Monetary values in requests and responses are integers in minor units.
- Business scope is derived from the token; identifiers supplied in payloads are validated against that scope.

---

## Database migrations

- SQL migrations live in `backend/internal/shared/migrations/sql` and are embedded in the API binary.
- Applied versions are recorded in `schema_migrations`; pending migrations run automatically at startup.
- Application is serialized with a PostgreSQL advisory lock (`pg_try_advisory_lock` with a retry loop) so parallel processes and parallel test packages cannot apply migrations concurrently or deadlock on conflicting lock order.
- Multi-statement migrations run in a transaction. Migrations marked `-- +migrate NoTransaction` execute statement by statement so `CREATE INDEX CONCURRENTLY` is possible; those migrations must be idempotent.
- Expensive constraints are added `NOT VALID` and validated in a separate migration, and concurrent index builds check `pg_index.indisvalid` and rebuild invalid leftovers so a failed build cannot be recorded as applied.

---

## Deployment and operations

**Live:** <https://business-os-tqu3.onrender.com>

Production PostgreSQL is Neon. Use the direct endpoint, not the `-pooler` endpoint, because the API holds a long-lived connection pool.

```dotenv
DB_HOST=ep-xxxx.<region>.aws.neon.tech
DB_PORT=5432
DB_USER=<user>
DB_PASSWORD=<password>
DB_NAME=neondb
DB_SSLMODE=require
```

Migrations apply on startup. A local `backend/.env` may point at Neon, while the Compose database remains available for offline work.

### Backups

`.github/workflows/db-backup.yml` runs daily at 02:00 UTC, dumps the database with `pg_dump`, compresses it, uploads to S3-compatible storage, and prunes dumps older than 30 days. It can also be triggered manually.

Repository secrets: `NEON_DATABASE_URL`, `S3_BUCKET`, `S3_ENDPOINT_URL` (R2 only), `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_DEFAULT_REGION`.

Restore:

```bash
gunzip -c businessos-YYYY-MM-DD.sql.gz | psql "postgresql://user:pass@host/db?sslmode=require"
```

Dumps are produced with `--clean --if-exists`, so a restore replaces the existing objects.

---

## Roadmap and known limitations

- Notifications are API-complete but have no dedicated screen yet.
- The assistant covers sale entry; reporting questions, forecasting and anomaly detection are not implemented.
- The assistant and receipt OCR require an external OpenAI-compatible provider; both are optional.
- CORS accepts a configurable list of origins but is intended for a single deployed environment.
- Coverage varies by module; the CI gate is the race-enabled suite, not a coverage threshold.

---

## Contributing

1. Branch from `main` using a descriptive prefix (`feat/`, `fix/`, `test/`, `docs/`, `chore/`).
2. Keep changes scoped; unrelated work belongs in a separate pull request.
3. Commit in focused units and describe the behaviour change, not the mechanics.
4. Open a pull request against `main` with the goal, context, acceptance criteria and verification steps.
5. CI must pass and review comments must be resolved; the repository owner performs the merge.
6. Do not merge with pending review threads or failing checks.

---

## License

MIT. See [LICENSE](LICENSE).
