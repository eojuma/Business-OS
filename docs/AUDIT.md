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
- client IP
- timestamp

The shared middleware covers all successful `POST`, `PATCH`, `PUT` and `DELETE`
requests on protected business routes. Before snapshots are read from the
database for updates, deletes, payments, restores, receives and role changes.
After snapshots come from the API response.

## Secret handling

Passwords, password hashes, tokens, credentials, API keys and Google subject
ids are removed recursively before snapshots are stored. Personal auth routes
(login, register, Google link/unlink, email verification) are excluded so
credentials never enter the trail. Notifications are excluded because they are
routine per-user state updates.

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
