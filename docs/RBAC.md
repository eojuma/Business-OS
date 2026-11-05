# Role-based access control

Business OS uses a fixed role model with a permission matrix that is enforced at
the API route boundary and mirrored in the dashboard UI.

## Roles

| Role | Typical use |
| --- | --- |
| `owner` | Workspace creator. Full access. |
| `admin` | Delegated workspace administrator. Same permissions as owner. |
| `manager` | Day-to-day operations: sales, purchasing, stock, suppliers, finance. |
| `cashier` | Sales/cashier desk: sales, customer payments, stock visibility. |
| `accountant` | Finance, reporting, and read-only operational visibility. |
| `warehouse` | Stock control and receiving purchases. |

The role string is stored on the user row and included in the JWT `role` claim.
`RequireAuth` reloads the live role from the database on every request, so an
owner/admin role change applies to the member's next API call without waiting
for the token to expire.

## Permission matrix

`Yes` means the role is granted the action.

| Permission | Owner / Admin | Manager | Cashier | Accountant | Warehouse |
| --- | --- | --- | --- | --- | --- |
| `account:self` | Yes | Yes | Yes | Yes | Yes |
| `business:read` | Yes | Yes | Yes | Yes | Yes |
| `business:manage` | Yes |  |  |  |  |
| `team:read` | Yes | Yes |  | Yes |  |
| `team:manage` | Yes |  |  |  |  |
| `sales:read` | Yes | Yes | Yes | Yes |  |
| `sales:create` | Yes | Yes | Yes |  |  |
| `invoices:read` | Yes | Yes | Yes | Yes |  |
| `invoices:payment` | Yes | Yes | Yes | Yes |  |
| `purchases:read` | Yes | Yes |  | Yes | Yes |
| `purchases:create` | Yes | Yes |  |  |  |
| `purchases:receive` | Yes | Yes |  |  | Yes |
| `purchases:delete` | Yes | Yes |  |  |  |
| `products:read` | Yes | Yes | Yes | Yes | Yes |
| `products:manage` | Yes | Yes |  |  |  |
| `products:delete` | Yes | Yes |  |  |  |
| `inventory:read` | Yes | Yes | Yes | Yes | Yes |
| `inventory:adjust` | Yes | Yes |  |  | Yes |
| `customers:read` | Yes | Yes | Yes | Yes |  |
| `customers:manage` | Yes | Yes | Yes |  |  |
| `customers:payment` | Yes | Yes | Yes | Yes |  |
| `suppliers:read` | Yes | Yes | Yes | Yes | Yes |
| `suppliers:manage` | Yes | Yes |  |  |  |
| `suppliers:payment` | Yes | Yes |  | Yes |  |
| `suppliers:delete` | Yes | Yes |  |  |  |
| `finance:read` | Yes | Yes |  | Yes |  |
| `finance:manage` | Yes | Yes |  | Yes |  |
| `finance:delete` | Yes | Yes |  |  |  |
| `reports:read` | Yes | Yes |  | Yes |  |
| `analytics:read` | Yes | Yes |  | Yes |  |
| `notifications:read` | Yes | Yes | Yes | Yes | Yes |
| `notifications:write` | Yes | Yes | Yes | Yes | Yes |
| `assistant:use` | Yes | Yes | Yes | Yes | Yes |
| `receipts:use` | Yes | Yes | Yes |  |  |

## Enforcement

- `backend/internal/shared/permissions` owns the role list, permissions, matrix,
  and route-to-permission map.
- Protected routes pass through `RequireAuth` and `RequireRoutePermission`.
  Unmapped protected routes fail closed with `500`, and a role without the
  required permission receives `403 insufficient permissions`.
- Listing team members (`GET /api/v1/users`) requires `team:read`, which owner,
  admin, manager, and accountant hold.
- Creating members (`POST /api/v1/users`) and changing roles
  (`PATCH /api/v1/users/:id/role`) require `team:manage`, which only owner and
  admin hold. A role change that would remove the last owner/admin is rejected
  with `409` so the workspace cannot be left without an administrator.
- Receipt commits also require the matching record permission: `purchases:create`
  for a purchase and `sales:create` for a sale, even when the caller has
  `receipts:use`.
- Invoice listing and detail require `invoices:read`; applying a payment requires
  `invoices:payment`.
- The dashboard hides navigation items and action controls the current role
  cannot use, and short-circuits pages that require a missing read permission.

When adding a protected route, add its mapping in
`backend/internal/shared/permissions/routes.go`; the router test fails if a
protected route has no mapping.
