# Audit log

Business OS records successful mutating API requests in an append-only audit
trail so owners and admins can investigate mistakes and hold staff accountable.

## What is recorded

Each entry captures:

- business
- actor user id, name and email snapshot
- action (`create`, `update`, `delete`, `payment`, `restore`, `receive`,
  `role_change`, `receipt_commit`, `assistant_commit`, `stock_movement`)
- entity type and id
- before and after JSON snapshots
- HTTP method, route, status code
- client IP (proxy headers are trusted only when `TRUSTED_PROXIES` is configured)
- timestamp

The shared middleware covers successful `POST`, `PATCH`, `PUT` and `DELETE`
requests on protected business routes, with these deliberate exceptions:

- `POST /api/v1/assistant/interpret` is a read-only preview and is not recorded.
- `POST /api/v1/receipts/scan` only reads an uploaded image and is not recorded.
- Personal auth routes (login, register, Google link/unlink, email verification)
  are excluded so credentials never enter the trail.
- Notification read/mark actions are excluded as routine per-user state updates.

Before snapshots are read from the database immediately before the handler for
updates, deletes, payments, restores, receives and role changes. After snapshots
are read from the persisted row when the entity is known, otherwise from the API
response. Public `POST /api/v1/business` creation is audited without an actor and
uses the new business id from the response.

Handlers that mutate a row inside their own transaction can call
`audit.SetBeforeSnapshot` with the state they read under the row lock, so the
entry describes that request's transition even when concurrent writers target the
same row.

## Secret handling

Passwords, password hashes, tokens, credentials, API keys and Google subject
ids are removed recursively before snapshots are stored. Personal auth routes
(login, register, Google link/unlink, email verification) are excluded so
credentials never enter the trail. Notifications are excluded because they are
routine per-user state updates.

## Durability

Audit rows are written immediately after a successful response and the insert is
retried; any persistent failure is logged. Audit persistence is currently
best-effort rather than sharing each handler's mutation transaction, so a hard
database failure can still drop an entry. Use the application logs to detect and
investigate a failed audit insert.

Fallback before snapshots are captured just before the handler runs. Two
concurrent updates to the same row can therefore observe the same predecessor;
the entry still records the actor and the persisted after state, but exact
per-transaction history requires the handler to supply `SetBeforeSnapshot` from
inside its transaction.

## Immutability

The application only inserts and reads audit entries. There are no update or
delete endpoints for the trail. The viewer is read-only and limited to
`audit:read`, which only owner and admin roles hold.

## Retention

Audit entries are retained for at least 24 months. The application does not
automatically delete them. Operators may prune entries older than the retention
window only after exporting or archiving them for compliance. Workspace deletion
removes that workspace's audit rows through the `business_id` cascade.

## Viewer

Owners and admins can open **Audit log** in the dashboard or call:

```text
GET /api/v1/audit?actor_id=&action=&entity_type=&entity_id=&date_from=&date_to=&page=&limit=
```

Results are newest first and paginated.
