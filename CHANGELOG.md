# Changelog

All notable changes to Business OS are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org).

## [Unreleased]

## [0.1.0] - 2026-10-31

Initial release: a multi-tenant operations platform for hardware retailers,
covering the full purchase-to-sale loop with reporting and an assistant.

### Added

- Authentication: email/password registration and login, Google Identity
  Services sign-in (ID-token flow), JWT sessions, and clean re-login when a
  session expires.
- Businesses with per-tenant data scoping derived from the token.
- Products with integer-minor-unit pricing and full CRUD.
- Inventory: stock levels plus an append-only movement ledger, written
  transactionally with sales and purchases.
- Sales: multi-line sales, stock decrement, customer credit charging, and
  credit-limit enforcement in one transaction.
- Customers: profiles, balances, credit limits, and payment recording.
- Suppliers: profiles, payment terms and due dates, outstanding balances,
  payments, archive/restore, pagination, and search.
- Purchases: draft purchase orders, sequential PO numbers, expected delivery
  dates, line-item notes, partial receiving, and supplier debt updates.
- Finance: expenses, profit, and cash flow with date-filtered summaries.
- Reports: daily sales aggregates computed in SQL.
- Analytics: overview, top-profit products, and slow-moving stock.
- Notifications: low-stock alerts with severity and product context.
- Assistant: natural-language sale preview with explicit confirmation,
  multi-conversation history with a side drawer, auto-titled and renameable
  chats, and quick actions.
- Receipt OCR: photo capture to extracted line items, reviewed before saving
  as a purchase or sale.
- Payables aging report: current, 1–30, 31–60, 61–90, and 90+ day buckets with
  per-bucket totals, supplier detail links, a selectable as-of date, and
  print/export.
- Light, dark, and system themes with a no-flash bootstrap script and semantic
  colour tokens shared across the frontend.

### Fixed

- Google sign-in initialises once and survives route changes and remounts.
- Expired sessions clear the stored token and redirect to sign-in with an
  explanation instead of surfacing raw API errors.
- Stale supplier balances after payments are prevented by refetching the list
  and refreshing the open dialog.
- Paginated counts are snapshot-consistent, offsets are overflow-safe, and
  supplier search escapes SQL LIKE wildcards.

### Infrastructure

- GitHub Actions CI: build, vet, formatting, lint, race-enabled backend tests
  against PostgreSQL, frontend lint/type-check/build, and a Docker build.
- Integration tests across auth, suppliers, products, purchases, and
  notifications, with per-test schema isolation.
- `mise` tasks mirroring CI for local runs.
- Scheduled database backups to S3-compatible storage with restore
  instructions.

### Documentation

- Production README covering architecture, module pattern, local and hybrid
  workflows, configuration, testing, API conventions, migrations, theming,
  and operations.
- Google sign-in, theming, supplier aging, and receipt OCR documentation.
