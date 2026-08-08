# Business OS

AI-powered operating system for hardware stores in Africa. Replaces notebooks, spreadsheets, and WhatsApp with a natural-language interface for inventory, sales, customer credit, and financial management.

## Status

Milestones 1–3 are done and tested live end to end — not just compiling, actually run against a real Postgres database and a real browser. Signup, products, inventory, sales, customer credit, and daily reports all work through the actual frontend, not just curl.

| Module | Backend | Frontend | Status |
|---|---|---|---|
| `auth` | ✅ | ✅ (login, signup) | Built & tested — register, login, JWT issuing, business-existence validation |
| `business` | ✅ | ✅ (via signup) | Built & tested — create (public), get/update (JWT-scoped) |
| `products` | ✅ | ✅ | Built & tested — full CRUD, price stored as int64 cents |
| `inventory` | ✅ | ✅ | Built & tested — stock levels + movement ledger, atomic via DB transaction |
| `sales` | ✅ | ✅ | Built & tested — multi-item sales, decrements stock, supports customer credit, all in one transaction |
| `customers` | ✅ | ✅ | Built & tested — profiles, credit limit enforcement, above-balance query |
| `reports` | ✅ | ✅ | Built & tested — daily sales aggregated via Postgres `SUM`/`GROUP BY`, not loaded row-by-row |
| Dashboard | — | ✅ | Today's sales, low-stock count, calls real endpoints |
| `suppliers` | 🟡 Stub | — | Deferred — Version 2 |
| `purchases` | 🟡 Stub | — | Deferred — Version 2 |
| `finance` | 🟡 Stub | — | Not started |
| `notifications` | 🟡 Stub | — | Deferred — Version 2 |
| `analytics` | 🟡 Stub | — | Not started |
| `assistant` | 🟡 Stub | — | **Next up** — deliberately last, since it depends on the service layers above |

## Stack

- **Backend:** Go, Gin, GORM, PostgreSQL, Redis
- **Frontend:** Next.js 14 (App Router), React, Tailwind CSS
- **Auth:** JWT
- **Architecture:** Modular monolith
- **Deployment:** Docker Compose (Postgres, Redis, backend, frontend all containerized)

## Project Structure

    Business-OS/
    ├── backend/
    │   ├── cmd/api/main.go              # entrypoint, runs AutoMigrate on startup
    │   ├── Dockerfile                    # multi-stage Go build
    │   └── internal/
    │       ├── config/                  # env config, loaded once
    │       ├── router/                  # wires all modules together — the ONLY
    │       │                            # place that imports every module
    │       ├── shared/
    │       │   ├── database/            # postgres + redis connections
    │       │   ├── middleware/          # JWT auth, CORS, request logging,
    │       │   │                        # CurrentBusinessID helper
    │       │   ├── response/            # consistent JSON response envelopes
    │       │   └── utils/               # money.go — cents <-> decimal conversion,
    │       │                            # the only place that conversion happens
    │       └── modules/
    │           ├── auth/                # full implementation — the template
    │           ├── business/            # full implementation
    │           ├── products/            # full implementation
    │           ├── inventory/           # full implementation
    │           ├── sales/               # full implementation, calls into
    │           │                        # inventory + products + customers
    │           ├── customers/           # full implementation
    │           ├── reports/             # read-only, no model.go — aggregates
    │           │                        # sales data directly via SQL
    │           └── ...                  # stubs, one folder per remaining module
    ├── frontend/
    │   ├── app/
    │   │   ├── login/                   # login page
    │   │   ├── signup/                  # combined business + owner signup
    │   │   └── dashboard/               # guarded by layout.tsx — redirects to
    │   │       ├── layout.tsx           # /login if no token in localStorage
    │   │       ├── page.tsx             # dashboard home
    │   │       ├── products/
    │   │       ├── inventory/
    │   │       ├── sales/
    │   │       ├── customers/
    │   │       └── reports/
    │   ├── lib/api.ts                   # single axios client, JWT auto-attached
    │   └── Dockerfile                    # production build (npm run build + start)
    ├── docker-compose.yml                # postgres + redis + backend + frontend
    ├── .env.example
    └── Makefile

## The module pattern

Every module that owns real data follows the same five files (see `auth/`, `business/`, or `products/` as the reference):

| File | Responsibility |
|---|---|
| `model.go` | GORM structs — the tables this module owns |
| `repository.go` | DB access only, behind an interface |
| `service.go` | Business logic, depends on `Repository` |
| `handler.go` | HTTP request/response, depends on `Service` |
| `routes.go` | Wires repo → service → handler → gin routes |

Modules never import each other directly. When one module genuinely needs another (e.g. `sales` needs to decrement `inventory` stock and check `customers` credit limits), the pattern is:

1. The dependent module (`sales`) declares a **narrow interface** describing only what it needs (`InventoryMover`, `CustomerCharger`) — never the other module's full `Repository` or `Service`.
2. `routes.go` constructs the real implementation and wires it in via an **adapter** that also translates error types across the module boundary (e.g. `inventory.ErrInsufficientStock` → `sales.ErrInsufficientStock`), so neither module needs to import the other's error types directly.

See `sales/repository.go` and `sales/routes.go` for the reference implementation of this pattern.

**Not every module needs all five files.** `reports` has no `model.go` — it queries other modules' tables directly (`r.db.Table("sales")...`) rather than owning any data of its own. `assistant` will call into other modules' services rather than a database at all.

## Getting started

Environment:

    cp .env.example .env

Start everything (Postgres, Redis, backend, frontend):

    make up
    docker compose ps

Or, for faster local iteration on the backend (no Docker rebuild per change):

    docker compose up -d postgres redis
    cd backend && go run ./cmd/api

Frontend dev server (hot reload):

    cd frontend && npm install && npm run dev

Health check: `curl http://localhost:8080/health` should return `{"status":"ok"}`

**Note:** don't run the Docker frontend container and `npm run dev` at the same time — they'll fight over port 3000, and the CORS middleware only allows the origin set in `FRONTEND_URL` (`.env`), so a port mismatch will silently break login with a CORS error in the browser console.

### Try the working flow

The full loop — sign up, add a product, restock it, sell it, see it in reports — works end to end through the actual UI at `http://localhost:3000/signup`. Or via curl:

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

## Conventions worth knowing

- **Money is always `int64` cents, never `float64`.** Prevents floating-point rounding drift across sales/inventory/reports. Convert to/from a decimal string only at the UI boundary — see `shared/utils/money.go` (backend) and the `formatMoney` helper duplicated in each frontend page (candidate for a shared frontend util once there are more pages).
- **Every protected route pulls `business_id` from the JWT**, never trusts one from the request body — see `middleware.CurrentBusinessID`. This is what prevents one store from reading or editing another store's data.
- **Cross-module writes that must succeed or fail together use a single DB transaction** — see `inventory.RecordMovementTx` and `sales.CreateSale`, which writes a sale, its line items, stock movements, and a customer credit charge all inside one `db.Transaction(...)` call.

## Database migrations

Currently using GORM's `AutoMigrate` on startup (see `main.go`) — every model from every module must be listed there explicitly, or its table silently never gets created (this bit us once already — caught via live testing, not the compiler). Fine for early development, but it can't safely handle things like column renames. Plan to move to versioned SQL migrations (e.g. [golang-migrate](https://github.com/golang-migrate/migrate)) before this touches production data.

## Build order

Matches the MVP scope in the product vision doc:

1. ~~`business`~~ ✅
2. ~~`products`~~ ✅
3. ~~`inventory`~~ ✅
4. ~~`sales`~~ ✅
5. ~~`customers`~~ ✅
6. ~~Frontend (signup, dashboard, products, inventory, sales, customers)~~ ✅
7. ~~`reports`~~ ✅
8. **`assistant`** ← next — depends on stable service layers from the modules above, which are now built and live-tested

## Known gaps

- No automated test suite yet — every fix so far has been verified by hand
- No CI pipeline yet
- `AutoMigrate` instead of real migrations (see above)
- CORS origin is configurable via `FRONTEND_URL` but still assumes one single allowed origin — fine for one environment, will need revisiting for staging + production
- No `suppliers`, `purchases`, `notifications` — deliberately deferred to post-MVP
- `assistant`'s natural-language disambiguation flow (what happens when input is ambiguous — e.g. "sold hammers" with no quantity) is undecided — worth designing before writing the module's service layer
- Not yet hardened for hosting: default JWT secret, default DB password, `GIN_MODE=debug` — see hosting checklist (tracked separately, not yet in this README)