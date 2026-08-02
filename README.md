# Business OS

AI-powered operating system for hardware stores in Africa. Replaces notebooks, spreadsheets, and WhatsApp with a natural-language interface for inventory, sales, customer credit, and financial management.

## Status

Backend foundation is live and tested end to end: business creation, user registration, JWT auth, and JWT-scoped protected routes all work against a real Postgres database.

| Module | Status |
|---|---|
| `auth` | ✅ Built & tested — register, login, JWT issuing |
| `business` | ✅ Built & tested — create (public), get/update (JWT-scoped) |
| `products` | 🟡 Stub — next up |
| `inventory` | 🟡 Stub |
| `sales` | 🟡 Stub |
| `customers` | 🟡 Stub |
| `suppliers` | 🟡 Stub |
| `purchases` | 🟡 Stub |
| `finance` | 🟡 Stub |
| `reports` | 🟡 Stub |
| `notifications` | 🟡 Stub |
| `analytics` | 🟡 Stub |
| `assistant` | 🟡 Stub — deliberately last |
| Frontend | ⚪ Not started — backend-first |

## Stack

- **Backend:** Go, Gin, GORM, PostgreSQL, Redis
- **Frontend:** Next.js 14 (App Router), React, Tailwind CSS — not yet built
- **Auth:** JWT
- **Architecture:** Modular monolith

## Project Structure

    Business-OS/
    ├── backend/
    │   ├── cmd/api/main.go              # entrypoint, runs AutoMigrate on startup
    │   └── internal/
    │       ├── config/                  # env config, loaded once
    │       ├── router/                  # wires all modules together — the ONLY
    │       │                            # place that imports every module
    │       ├── shared/
    │       │   ├── database/            # postgres + redis connections
    │       │   ├── middleware/          # JWT auth, request logging
    │       │   └── response/            # consistent JSON response envelopes
    │       └── modules/
    │           ├── auth/                # full implementation — the template
    │           ├── business/            # full implementation
    │           └── ...                  # stubs, one folder per module
    ├── docker-compose.yml                # postgres + redis (backend/frontend added once built)
    ├── .env.example
    └── Makefile

## The module pattern

Every module that owns real data follows the same five files (see `auth/` or `business/` as the reference):

| File | Responsibility |
|---|---|
| `model.go` | GORM structs — the tables this module owns |
| `repository.go` | DB access only, behind an interface |
| `service.go` | Business logic, depends on `Repository` |
| `handler.go` | HTTP request/response, depends on `Service` |
| `routes.go` | Wires repo → service → handler → gin routes |

Modules never import each other directly. `router.go` is the only file that knows every module exists.

**Not every module needs all five files.** `reports` and `analytics` mostly read across other modules' tables rather than owning their own — they may have no `model.go`. `assistant` calls into other modules' services rather than a database at all. Use the full pattern only for modules that genuinely own and mutate their own data: `products`, `inventory`, `customers`, `suppliers`, `sales`, `purchases`, `finance`.

## Getting started

Environment:

    cp .env.example .env

Start Postgres + Redis:

    docker compose up -d postgres redis
    docker compose ps

Install Go dependencies (first time only):

    cd backend && go mod tidy

Run the API:

    go run ./cmd/api

or, from the project root:

    make backend-run

Health check: `curl http://localhost:8080/health` should return `{"status":"ok"}`

### Try the working flow

Create a business (public, no auth):

    curl -X POST http://localhost:8080/api/v1/business \
      -H "Content-Type: application/json" \
      -d '{"name": "My Hardware Store", "phone": "0700000000"}'

Copy the returned `id`, then register a user against that business:

    curl -X POST http://localhost:8080/api/v1/auth/register \
      -H "Content-Type: application/json" \
      -d '{"business_id": "<id>", "name": "Owner", "email": "owner@example.com", "password": "password123"}'

Copy the returned `token`, then fetch the business using it:

    curl http://localhost:8080/api/v1/business \
      -H "Authorization: Bearer <token>"

## Database migrations

Currently using GORM's `AutoMigrate` on startup (see `main.go`) — fine for early development, but it can't safely handle things like column renames. Plan to move to versioned SQL migrations (e.g. [golang-migrate](https://github.com/golang-migrate/migrate)) before this touches production data.

## Build order

Matches the MVP scope in the product vision doc:

1. ~~`business`~~ ✅
2. **`products`** ← next
3. `inventory`
4. `sales`
5. `customers`
6. Frontend dashboard
7. `reports`
8. `assistant` (deliberately last — depends on stable service layers from the modules above)

## Known gaps

- No test suite yet
- No CI pipeline yet
- Frontend not started
- `assistant`'s natural-language disambiguation flow (what happens when input is ambiguous — e.g. "sold hammers" with no quantity) is undecided