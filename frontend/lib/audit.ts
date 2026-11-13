export type AuditEntry = {
  id: string;
  actor_user_id?: string | null;
  actor_name?: string;
  actor_email?: string;
  action: string;
  entity_type: string;
  entity_id?: string | null;
  before?: unknown;
  after?: unknown;
  method: string;
  path: string;
  status_code: number;
  ip_address: string;
  created_at: string;
};

export const AUDIT_ACTIONS = [
  "create",
  "update",
  "delete",
  "payment",
  "restore",
  "receive",
  "role_change",
  "receipt_commit",
  "assistant_commit",
  "stock_movement",
] as const;

export const AUDIT_ENTITIES = [
  "product",
  "customer",
  "supplier",
  "sale",
  "purchase",
  "expense",
  "invoice",
  "user",
  "business",
  "stock_movement",
  "receipt",
] as const;

const ACTION_LABELS: Record<string, string> = {
  create: "Created",
  update: "Updated",
  delete: "Deleted",
  payment: "Payment",
  restore: "Restored",
  receive: "Received",
  role_change: "Role change",
  receipt_commit: "Receipt commit",
  assistant_commit: "Assistant commit",
  stock_movement: "Stock movement",
};

const ENTITY_LABELS: Record<string, string> = {
  product: "Product",
  customer: "Customer",
  supplier: "Supplier",
  sale: "Sale",
  purchase: "Purchase",
  expense: "Expense",
  invoice: "Invoice",
  user: "User",
  business: "Business",
  stock_movement: "Stock movement",
  receipt: "Receipt",
};

/** Returns the display label for an audit action. */
export function auditActionLabel(action: string): string {
  return ACTION_LABELS[action] ?? action;
}

/** Returns the display label for an audited entity type. */
export function auditEntityLabel(entity: string): string {
  return ENTITY_LABELS[entity] ?? entity;
}

/** Formats an audit timestamp for the current locale. */
export function auditTimestamp(value: string): string {
  return new Date(value).toLocaleString("en-KE", {
    dateStyle: "medium",
    timeStyle: "medium",
  });
}
