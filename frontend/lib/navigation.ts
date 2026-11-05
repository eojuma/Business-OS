import {
  BarChart3,
  Bell,
  Boxes,
  Brain,
  CircleDollarSign,
  ClipboardList,
  Contact,
  FileText,
  LayoutDashboard,
  Package,
  ScanLine,
  Settings,
  ShoppingCart,
  Truck,
  Users,
  type LucideIcon,
} from "lucide-react";
import type { Permission } from "@/lib/permissions";
import { PERMISSIONS, can } from "@/lib/permissions";

export type NavItem = {
  href: string;
  label: string;
  icon: LucideIcon;
  permission: Permission;
};

export type NavGroup = {
  id: string;
  label: string;
  items: NavItem[];
};

export const NAV_GROUPS: NavGroup[] = [
  {
    id: "sales",
    label: "Sales",
    items: [
      { href: "/dashboard", label: "Overview", icon: LayoutDashboard, permission: PERMISSIONS.analyticsRead },
      { href: "/dashboard/sales", label: "Sales", icon: ShoppingCart, permission: PERMISSIONS.salesCreate },
      { href: "/dashboard/invoices", label: "Invoices", icon: FileText, permission: PERMISSIONS.invoicesRead },
      { href: "/dashboard/purchases", label: "Purchases", icon: ClipboardList, permission: PERMISSIONS.purchasesRead },
    ],
  },
  {
    id: "inventory",
    label: "Inventory",
    items: [
      { href: "/dashboard/products", label: "Products", icon: Package, permission: PERMISSIONS.productsRead },
      { href: "/dashboard/inventory", label: "Inventory", icon: Boxes, permission: PERMISSIONS.inventoryRead },
      { href: "/dashboard/receipts", label: "Receipt OCR", icon: ScanLine, permission: PERMISSIONS.receiptsUse },
    ],
  },
  {
    id: "finance",
    label: "Finance",
    items: [
      { href: "/dashboard/finance", label: "Finance", icon: CircleDollarSign, permission: PERMISSIONS.financeRead },
      { href: "/dashboard/reports", label: "Reports", icon: Contact, permission: PERMISSIONS.reportsRead },
      { href: "/dashboard/analytics", label: "Analytics", icon: BarChart3, permission: PERMISSIONS.analyticsRead },
    ],
  },
  {
    id: "people",
    label: "People",
    items: [
      { href: "/dashboard/customers", label: "Customers", icon: Users, permission: PERMISSIONS.customersRead },
      { href: "/dashboard/suppliers", label: "Suppliers", icon: Truck, permission: PERMISSIONS.suppliersRead },
    ],
  },
  {
    id: "system",
    label: "System",
    items: [
      { href: "/dashboard/notifications", label: "Notifications", icon: Bell, permission: PERMISSIONS.notificationsRead },
      { href: "/dashboard/assistant", label: "AI assistant", icon: Brain, permission: PERMISSIONS.assistantUse },
      { href: "/dashboard/settings", label: "Settings", icon: Settings, permission: PERMISSIONS.accountSelf },
    ],
  },
];

/**
 * Overview is only active on exactly `/dashboard`; every other item also owns
 * its descendants, so `/dashboard/suppliers/123` keeps Suppliers highlighted.
 */
export function isActiveRoute(href: string, pathname: string): boolean {
  if (href === "/dashboard") return pathname === href;
  return pathname === href || pathname.startsWith(`${href}/`);
}

/** Returns the group that owns the current path, if any. */
export function findActiveGroupId(
  groups: NavGroup[],
  pathname: string,
): string | null {
  const group = groups.find((candidate) =>
    candidate.items.some((item) => isActiveRoute(item.href, pathname)),
  );
  return group?.id ?? null;
}

/** Returns the first dashboard page the role can open. */
export function firstPermittedPath(role: string | null | undefined): string | null {
  for (const group of NAV_GROUPS) {
    for (const item of group.items) {
      if (can(role, item.permission)) return item.href;
    }
  }
  return null;
}

export const NAV_STATE_PREFIX = "nav-collapsed:";

/** Builds the storage key that keeps each user's collapse preference separate. */
export function navStateKey(userId: string): string {
  return `${NAV_STATE_PREFIX}${userId}`;
}

/**
 * The sidebar preference belongs to the signed-in account, so the storage key
 * is derived from the user id carried in the session token. A missing or
 * malformed token falls back to a shared key rather than throwing.
 */
export function decodeUserId(token: string | null): string | null {
  if (!token) return null;
  const payload = token.split(".")[1];
  if (!payload) return null;

  try {
    const normalized = payload.replace(/-/g, "+").replace(/_/g, "/");
    const padded = normalized.padEnd(Math.ceil(normalized.length / 4) * 4, "=");
    const claims = JSON.parse(atob(padded)) as { user_id?: unknown };
    return typeof claims.user_id === "string" && claims.user_id ? claims.user_id : null;
  } catch {
    return null;
  }
}

/** Unknown group ids are dropped so stale storage cannot hide a new group. */
export function parseCollapsedGroups(
  raw: string | null,
  groups: NavGroup[],
): string[] {
  if (!raw) return [];

  try {
    const parsed: unknown = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    return parsed.filter(
      (id): id is string =>
        typeof id === "string" && groups.some((group) => group.id === id),
    );
  } catch {
    return [];
  }
}

/** Serializes collapsed group ids for localStorage, removing duplicates. */
export function serializeCollapsedGroups(collapsed: Iterable<string>): string {
  return JSON.stringify([...new Set(collapsed)]);
}

/** Adds or removes a group from the collapsed set without mutating the input. */
export function toggleGroupCollapsed(
  collapsed: string[],
  groupId: string,
): string[] {
  return collapsed.includes(groupId)
    ? collapsed.filter((id) => id !== groupId)
    : [...collapsed, groupId];
}
