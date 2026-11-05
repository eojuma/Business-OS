export const ROLES = [
  "owner",
  "admin",
  "manager",
  "cashier",
  "accountant",
  "warehouse",
] as const;

export type Role = (typeof ROLES)[number];

export const ROLE_LABELS: Record<Role, string> = {
  owner: "Owner",
  admin: "Admin",
  manager: "Manager",
  cashier: "Sales / Cashier",
  accountant: "Accountant",
  warehouse: "Warehouse",
};

export const PERMISSIONS = {
  accountSelf: "account:self",
  businessRead: "business:read",
  businessManage: "business:manage",
  teamRead: "team:read",
  teamManage: "team:manage",
  salesRead: "sales:read",
  salesCreate: "sales:create",
  invoicesRead: "invoices:read",
  invoicesPayment: "invoices:payment",
  purchasesRead: "purchases:read",
  purchasesCreate: "purchases:create",
  purchasesReceive: "purchases:receive",
  purchasesDelete: "purchases:delete",
  productsRead: "products:read",
  productsManage: "products:manage",
  productsDelete: "products:delete",
  inventoryRead: "inventory:read",
  inventoryAdjust: "inventory:adjust",
  customersRead: "customers:read",
  customersManage: "customers:manage",
  customersPayment: "customers:payment",
  suppliersRead: "suppliers:read",
  suppliersManage: "suppliers:manage",
  suppliersPayment: "suppliers:payment",
  suppliersDelete: "suppliers:delete",
  financeRead: "finance:read",
  financeManage: "finance:manage",
  financeDelete: "finance:delete",
  reportsRead: "reports:read",
  analyticsRead: "analytics:read",
  notificationsRead: "notifications:read",
  notificationsWrite: "notifications:write",
  assistantUse: "assistant:use",
  receiptsUse: "receipts:use",
} as const;

export type Permission = (typeof PERMISSIONS)[keyof typeof PERMISSIONS];

const ALL_PERMISSIONS = Object.values(PERMISSIONS);

const MATRIX: Record<Role, readonly Permission[]> = {
  owner: ALL_PERMISSIONS,
  admin: ALL_PERMISSIONS,
  manager: [
    PERMISSIONS.accountSelf,
    PERMISSIONS.businessRead,
    PERMISSIONS.teamRead,
    PERMISSIONS.salesRead,
    PERMISSIONS.salesCreate,
    PERMISSIONS.invoicesRead,
    PERMISSIONS.invoicesPayment,
    PERMISSIONS.purchasesRead,
    PERMISSIONS.purchasesCreate,
    PERMISSIONS.purchasesReceive,
    PERMISSIONS.purchasesDelete,
    PERMISSIONS.productsRead,
    PERMISSIONS.productsManage,
    PERMISSIONS.productsDelete,
    PERMISSIONS.inventoryRead,
    PERMISSIONS.inventoryAdjust,
    PERMISSIONS.customersRead,
    PERMISSIONS.customersManage,
    PERMISSIONS.customersPayment,
    PERMISSIONS.suppliersRead,
    PERMISSIONS.suppliersManage,
    PERMISSIONS.suppliersPayment,
    PERMISSIONS.suppliersDelete,
    PERMISSIONS.financeRead,
    PERMISSIONS.financeManage,
    PERMISSIONS.financeDelete,
    PERMISSIONS.reportsRead,
    PERMISSIONS.analyticsRead,
    PERMISSIONS.notificationsRead,
    PERMISSIONS.notificationsWrite,
    PERMISSIONS.assistantUse,
    PERMISSIONS.receiptsUse,
  ],
  cashier: [
    PERMISSIONS.accountSelf,
    PERMISSIONS.businessRead,
    PERMISSIONS.salesRead,
    PERMISSIONS.salesCreate,
    PERMISSIONS.invoicesRead,
    PERMISSIONS.invoicesPayment,
    PERMISSIONS.productsRead,
    PERMISSIONS.inventoryRead,
    PERMISSIONS.customersRead,
    PERMISSIONS.customersManage,
    PERMISSIONS.customersPayment,
    PERMISSIONS.suppliersRead,
    PERMISSIONS.notificationsRead,
    PERMISSIONS.notificationsWrite,
    PERMISSIONS.assistantUse,
    PERMISSIONS.receiptsUse,
  ],
  accountant: [
    PERMISSIONS.accountSelf,
    PERMISSIONS.businessRead,
    PERMISSIONS.teamRead,
    PERMISSIONS.salesRead,
    PERMISSIONS.invoicesRead,
    PERMISSIONS.invoicesPayment,
    PERMISSIONS.purchasesRead,
    PERMISSIONS.productsRead,
    PERMISSIONS.inventoryRead,
    PERMISSIONS.customersRead,
    PERMISSIONS.customersPayment,
    PERMISSIONS.suppliersRead,
    PERMISSIONS.suppliersPayment,
    PERMISSIONS.financeRead,
    PERMISSIONS.financeManage,
    PERMISSIONS.reportsRead,
    PERMISSIONS.analyticsRead,
    PERMISSIONS.notificationsRead,
    PERMISSIONS.notificationsWrite,
    PERMISSIONS.assistantUse,
  ],
  warehouse: [
    PERMISSIONS.accountSelf,
    PERMISSIONS.businessRead,
    PERMISSIONS.purchasesRead,
    PERMISSIONS.purchasesReceive,
    PERMISSIONS.productsRead,
    PERMISSIONS.inventoryRead,
    PERMISSIONS.inventoryAdjust,
    PERMISSIONS.suppliersRead,
    PERMISSIONS.notificationsRead,
    PERMISSIONS.notificationsWrite,
    PERMISSIONS.assistantUse,
  ],
};

/** Narrows a stored role string to one the UI knows how to render. */
export function isRole(value: string | null | undefined): value is Role {
  return typeof value === "string" && (ROLES as readonly string[]).includes(value);
}

/** Returns the display label for a role, preserving unknown stored values. */
export function roleLabel(role: string | null | undefined): string {
  return isRole(role) ? ROLE_LABELS[role] : role || "Unknown";
}

/** Checks a single permission against the signed-in user role. */
export function can(role: string | null | undefined, permission: Permission): boolean {
  if (!isRole(role)) return false;
  return MATRIX[role].includes(permission);
}
