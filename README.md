# Business OS

Business OS is a multi-tenant operations platform for hardware retailers. It replaces notebooks, spreadsheets and ad-hoc messaging with a single system for products and stock, sales, customer credit, suppliers and purchases, expenses and profitability, reporting, and a natural-language assistant for day-to-day data entry.

**Live deployment:** <https://business-os-tqu3.onrender.com> — the frontend is served from Render and points at the API through `NEXT_PUBLIC_API_URL`.

**Current release:** [v0.1.0 — Initial release](https://github.com/eojuma/Business-OS/releases/tag/v0.1.0) · [Changelog](CHANGELOG.md)

The backend is a Go modular monolith (Gin + GORM + PostgreSQL + Redis). The frontend is a Next.js 14 App Router application. Data is scoped per business, money is stored in integer minor units, and every cross-module write runs in one database transaction.

---

## Contents

- [Capabilities](#capabilities)
- [Architecture](#architecture)
- [Repository layout](#repository-layout)
- [Backend module pattern](#backend-module-pattern)
- [Local development](#local-development)
- [Configuration reference](#configuration-reference)
- [Theming](#theming)
- [Testing and quality gates](#testing-and-quality-gates)
- [API conventions](#api-conventions)
- [Database migrations](#database-migrations)
- [Deployment and operations](#deployment-and-operations)
- [Roadmap and known limitations](#roadmap-and-known-limitations)
- [Releases and versioning](#releases-and-versioning)
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
| Suppliers | Yes | Yes | Profiles, payment terms and due dates, outstanding balances, payments, archive/restore, aging report |
| Purchases | Yes | Yes | Draft purchase orders, sequential PO numbers, partial receiving, supplier debt updates |
| Finance | Yes | Yes | Expenses, profit, cash flow, date-filtered summaries |
| Reports | Yes | Yes | Daily sales and period aggregates computed in SQL |
| Analytics | Yes | Yes | Overview, top-profit and slow-moving products |
| Notifications | Yes | — | Low-stock alerts with severity and product context; polling API |
| Assistant | Yes | Yes | Sale preview with confirmation; multi-conversation history in a drawer, auto-titled and renameable chats, quick actions |
| Receipt OCR | Yes | Yes | Photo to extracted line items, reviewed before saving |

### Supplier aging

Open **Suppliers → Aging** to view current outstanding balances in the current, 1–30, 31–60, 61–90, and more-than-90-day buckets. Each bucket has a total in KSh and links to supplier details and payment history.

`GET /api/v1/suppliers/aging` accepts an optional RFC3339 `as_of` timestamp (defaults to now). The normal `{ success, data }` response contains `as_of`, arrays named `current`, `days_1_30`, `days_31_60`, `days_61_90`, `days_90_plus`, and `totals` with the same bucket keys. Entries contain `id`, `name`, `outstanding_balance` in integer minor units, nullable `due_date`, and `days_overdue`. Empty buckets are arrays with zero totals; invalid timestamps return 400.

The report includes active suppliers with positive balances for the authenticated business. It ages **current balances** using each supplier's saved due date and UTC calendar days, rather than reconstructing historical balances or allocating debt per invoice. Missing, future, and same-day due dates are current; day 90 belongs to `days_61_90` and day 91 starts `days_90_plus`.

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
├── CHANGELOG.md                     Release history (Keep a Changelog format)
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

The templates are host-oriented (`DB_HOST=localhost`, `REDIS_URL=redis://localhost:6379`). Docker Compose overrides both with the service names (`postgres`, `redis`), so the same file works in either mode.

Settings worth checking before the first run:

- `APP_ENV` — `development`, `test`, `staging` or `production` (case-insensitive). Production enables strict configuration validation and removes the implicit localhost CORS origins; unknown values abort startup.
- `JWT_SECRET` — replace the placeholder outside local development.
- `FRONTEND_URL` — comma-separated allowed browser origins. Outside production the API also allows `http://localhost:3000` and `http://127.0.0.1:3000` automatically; with `APP_ENV=production` only the configured list is allowed.

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

- A password account is **never** linked to a Google identity automatically. When a Google email matches a verified password account, sign-in returns `409` with code `google_link_requires_password` (or `google_link_required` for passwordless accounts). The user signs in with their existing method and connects Google from **Settings → Connected accounts** (`POST /auth/google/link`), which requires the Google email to match the account email.
- A **verified** local account is never silently claimed. An **unverified** local account (password sign-up that was never confirmed) can be claimed by the Google-verified owner: the account is marked verified, the Google subject is attached, and the untrusted password is cleared. This resolves pre-registration/“pre-hijacking” abuse without locking out the real inbox owner.
- If the Google subject behind an email changes (Workspace user recreated, address reassigned), sign-in returns `409` with code `google_account_conflict`. Re-linking is a deliberate operator action: verify the new owner out of band and clear the stale link before the user signs in again.

```sql
UPDATE users SET google_id = NULL WHERE email = 'user@example.com';
```

- An account with a password always uses password sign-in; an unknown address and a Google-only account return the same generic credentials error, so account type is not disclosed.
- With `GOOGLE_CLIENT_ID` empty, `/auth/google` returns `503` and the UI reports that Google sign-in is not configured.

### 5b. Email verification

Password sign-ups must confirm their email address before they can sign in. Existing accounts are grandfathered as verified by migration `000018`.

- `POST /auth/register` creates the user unverified and sends a single-use link (`FRONTEND_URL/verify-email?token=…`); no session token is issued yet.
- `POST /auth/email/verify` consumes the token; `POST /auth/email/resend` issues a fresh link (always returns `202`, so addresses cannot be enumerated).
- `POST /auth/login` returns `403` with code `email_not_verified` until the address is confirmed.
- Tokens are stored only as SHA-256 hashes, expire after `EMAIL_VERIFICATION_TTL_HOURS` (default 24), and can be used once.
- SMTP is required in production (`APP_ENV=production` refuses to start without `SMTP_HOST`). In development the link is written to the backend log instead of being emailed.
- Connecting Google marks the account verified because Google has proven inbox ownership.

```bash
SMTP_HOST=smtp.example.com
SMTP_PORT=587
SMTP_USERNAME=...
SMTP_PASSWORD=...
SMTP_FROM="Business OS <no-reply@example.com>"
SMTP_TLS=true
EMAIL_VERIFICATION_TTL_HOURS=24
```

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
| `APP_ENV` | `development` | One of `development`, `test`, `staging`, `production` (case-insensitive). Unknown values abort startup; production enables strict validation and drops the implicit localhost CORS origins |
| `APP_PORT` | `8080` | API listen port |
| `FRONTEND_URL` | `http://localhost:3000` | Comma-separated allowed CORS origins; the two localhost origins are added automatically only outside production |
| `DB_HOST` | `localhost` | PostgreSQL host (`postgres` inside Compose) |
| `DB_PORT` | `5432` | PostgreSQL port |
| `DB_USER` / `DB_PASSWORD` / `DB_NAME` | `businessos` | PostgreSQL credentials and database |
| `DB_SSLMODE` | `disable` | `require` for managed PostgreSQL |
| `REDIS_URL` | `redis://localhost:6379` | Redis connection string (`redis://redis:6379` inside Compose) |
| `JWT_SECRET` | development placeholder | JWT signing key; must be set in production |
| `JWT_EXPIRY_HOURS` | `24` | Token lifetime |
| `GOOGLE_CLIENT_ID` | empty | Enables Google sign-in when set |
| `SMTP_HOST` | empty | SMTP relay for verification emails; required in production |
| `SMTP_PORT` | `587` | SMTP port (STARTTLS) |
| `SMTP_USERNAME` / `SMTP_PASSWORD` | empty | SMTP credentials (optional for open relays) |
| `SMTP_FROM` | `Business OS <no-reply@businessos.local>` | Envelope/from address |
| `SMTP_TLS` | `true` | Whether the relay is expected to offer STARTTLS |
| `EMAIL_VERIFICATION_TTL_HOURS` | `24` | Verification link lifetime |
| `AI_PROVIDER` / `AI_MODEL` / `AI_BASE_URL` | Gemini defaults | OpenAI-compatible assistant provider |
| `AI_API_KEY` | empty | Required for `/assistant/*`; those routes return `500` without it |
| `AI_OCR_MODEL` | empty | Vision model for receipt OCR; falls back to `AI_MODEL` |

### Frontend

| Variable | Purpose |
|---|---|
| `NEXT_PUBLIC_API_URL` | API base URL including `/api/v1` (for example `http://localhost:8080/api/v1`) |
| `NEXT_PUBLIC_GOOGLE_CLIENT_ID` | Same client ID as `GOOGLE_CLIENT_ID`; inlined at build time |

---

## Theming

The web app ships light and dark palettes with a system default.

- The toggle is available in the desktop sidebar footer, the mobile header, and the login and signup pages.
- The preference is stored in `localStorage` under the `theme` key (`light`, `dark`, or `system`); `system` follows the OS `prefers-color-scheme`.
- An inline script in `frontend/app/layout.tsx` applies the `dark` class to `<html>` before first paint, and `<html>` sets `suppressHydrationWarning` so the script-applied class does not cause a hydration mismatch.
- `frontend/lib/theme-provider.tsx` owns the state and persistence; `frontend/components/theme-toggle.tsx` renders a segmented control with `aria-pressed` and keyboard focus styles.

New components must use the semantic tokens defined in `frontend/app/globals.css` and mapped in `frontend/tailwind.config.ts`; hardcoded hex colours are not accepted in review.

| Token | Purpose |
|---|---|
| `bg-background` / `text-foreground` | Page background and primary text |
| `bg-card` | Panels, cards, inputs, sidebar |
| `bg-surface-muted` | Inset and secondary surfaces (table headers, skeletons) |
| `text-muted` / `text-subtle` | Secondary and tertiary text |
| `border-border` / `border-border-subtle` | Dividers and outlines |
| `bg-hover` | Hover surface |
| `text-primary`, `bg-primary-solid`, `bg-primary-soft`, `text-primary-hover` | Brand actions and accents |
| `text-warning`, `bg-warning-soft`, `text-danger`, `bg-danger-soft` | Status colours |

Each token has a light value on `:root` and a dark value under `.dark`, so token classes need no `dark:` variants. Values are space-separated RGB channels, so opacity modifiers such as `bg-primary/10` work.

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

## Releases and versioning

Releases follow [Semantic Versioning](https://semver.org) and are tagged from `main` only after CI passes.

- The `0.x` line is pre-1.0: minor versions add features, patch versions contain fixes.
- `v0.1.0` is the initial release. Everything that shipped is recorded in [CHANGELOG.md](CHANGELOG.md).
- Tags are annotated (`vMAJOR.MINOR.PATCH`) and every GitHub Release uses the matching changelog section as its notes.
- Release checklist: green CI on `main` → update `CHANGELOG.md` → tag on `main` → publish the GitHub Release.

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
