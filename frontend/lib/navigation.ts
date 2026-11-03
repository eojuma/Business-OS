import {
  BarChart3,
  Bell,
  Boxes,
  Brain,
  CircleDollarSign,
  ClipboardList,
  Contact,
  LayoutDashboard,
  Package,
  ScanLine,
  Settings,
  ShoppingCart,
  Truck,
  Users,
  type LucideIcon,
} from "lucide-react";

export type NavItem = {
  href: string;
  label: string;
  icon: LucideIcon;
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
      { href: "/dashboard", label: "Overview", icon: LayoutDashboard },
      { href: "/dashboard/sales", label: "Sales", icon: ShoppingCart },
      { href: "/dashboard/purchases", label: "Purchases", icon: ClipboardList },
    ],
  },
  {
    id: "inventory",
    label: "Inventory",
    items: [
      { href: "/dashboard/products", label: "Products", icon: Package },
      { href: "/dashboard/inventory", label: "Inventory", icon: Boxes },
      { href: "/dashboard/receipts", label: "Receipt OCR", icon: ScanLine },
    ],
  },
  {
    id: "finance",
    label: "Finance",
    items: [
      { href: "/dashboard/finance", label: "Finance", icon: CircleDollarSign },
      { href: "/dashboard/reports", label: "Reports", icon: Contact },
      { href: "/dashboard/analytics", label: "Analytics", icon: BarChart3 },
    ],
  },
  {
    id: "people",
    label: "People",
    items: [
      { href: "/dashboard/customers", label: "Customers", icon: Users },
      { href: "/dashboard/suppliers", label: "Suppliers", icon: Truck },
    ],
  },
  {
    id: "system",
    label: "System",
    items: [
      { href: "/dashboard/notifications", label: "Notifications", icon: Bell },
      { href: "/dashboard/assistant", label: "AI assistant", icon: Brain },
      { href: "/dashboard/settings", label: "Settings", icon: Settings },
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

export function findActiveGroupId(
  groups: NavGroup[],
  pathname: string,
): string | null {
  const group = groups.find((candidate) =>
    candidate.items.some((item) => isActiveRoute(item.href, pathname)),
  );
  return group?.id ?? null;
}

export const NAV_STATE_PREFIX = "nav-collapsed:";

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

export function serializeCollapsedGroups(collapsed: Iterable<string>): string {
  return JSON.stringify([...new Set(collapsed)]);
}

export function toggleGroupCollapsed(
  collapsed: string[],
  groupId: string,
): string[] {
  return collapsed.includes(groupId)
    ? collapsed.filter((id) => id !== groupId)
    : [...collapsed, groupId];
}
