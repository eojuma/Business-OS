import { describe, expect, it } from "vitest";
import {
  NAV_GROUPS,
  decodeUserId,
  findActiveGroupId,
  isActiveRoute,
  navStateKey,
  parseCollapsedGroups,
  serializeCollapsedGroups,
  toggleGroupCollapsed,
} from "./navigation";

function tokenWith(claims: Record<string, unknown>): string {
  const payload = Buffer.from(JSON.stringify(claims)).toString("base64url");
  return `header.${payload}.signature`;
}

describe("navigation groups", () => {
  it("matches the agreed module groupings", () => {
    const shape = NAV_GROUPS.map((group) => ({
      label: group.label,
      items: group.items.map((item) => item.label),
    }));

    expect(shape).toEqual([
      { label: "Sales", items: ["Overview", "Sales", "Purchases"] },
      { label: "Inventory", items: ["Products", "Inventory", "Receipt OCR"] },
      { label: "Finance", items: ["Finance", "Reports", "Analytics"] },
      { label: "People", items: ["Customers", "Suppliers"] },
      { label: "System", items: ["Notifications", "AI assistant", "Settings"] },
    ]);
  });

  it("gives every item a unique href", () => {
    const hrefs = NAV_GROUPS.flatMap((group) => group.items.map((item) => item.href));
    expect(new Set(hrefs).size).toBe(hrefs.length);
  });

  it("gives every item a permission", () => {
    for (const item of NAV_GROUPS.flatMap((group) => group.items)) {
      expect(item.permission).toBeTruthy();
    }
  });

  it("routes every item to its module page", () => {
    const hrefs = NAV_GROUPS.flatMap((group) => group.items.map((item) => item.href));
    expect(hrefs).toEqual([
      "/dashboard",
      "/dashboard/sales",
      "/dashboard/purchases",
      "/dashboard/products",
      "/dashboard/inventory",
      "/dashboard/receipts",
      "/dashboard/finance",
      "/dashboard/reports",
      "/dashboard/analytics",
      "/dashboard/customers",
      "/dashboard/suppliers",
      "/dashboard/notifications",
      "/dashboard/assistant",
      "/dashboard/settings",
    ]);
  });
});

describe("active routes", () => {
  it("keeps Overview off nested dashboard routes", () => {
    expect(isActiveRoute("/dashboard", "/dashboard")).toBe(true);
    expect(isActiveRoute("/dashboard", "/dashboard/sales")).toBe(false);
  });

  it("highlights a module on its own and nested routes", () => {
    expect(isActiveRoute("/dashboard/sales", "/dashboard/sales")).toBe(true);
    expect(isActiveRoute("/dashboard/suppliers", "/dashboard/suppliers/abc")).toBe(true);
    expect(isActiveRoute("/dashboard/sales", "/dashboard/sales-archive")).toBe(false);
  });

  it("finds the group that owns the current path", () => {
    expect(findActiveGroupId(NAV_GROUPS, "/dashboard")).toBe("sales");
    expect(findActiveGroupId(NAV_GROUPS, "/dashboard/receipts")).toBe("inventory");
    expect(findActiveGroupId(NAV_GROUPS, "/dashboard/suppliers/abc")).toBe("people");
    expect(findActiveGroupId(NAV_GROUPS, "/dashboard/settings")).toBe("system");
    expect(findActiveGroupId(NAV_GROUPS, "/login")).toBeNull();
  });
});

describe("collapsed group persistence", () => {
  it("scopes storage to the signed-in user", () => {
    expect(navStateKey("user-a")).not.toBe(navStateKey("user-b"));
    expect(navStateKey("user-a")).toContain("user-a");
  });

  it("reads back a serialized preference", () => {
    const raw = serializeCollapsedGroups(["finance", "people"]);
    expect(parseCollapsedGroups(raw, NAV_GROUPS)).toEqual(["finance", "people"]);
  });

  it("deduplicates what it writes", () => {
    expect(serializeCollapsedGroups(["finance", "finance"])).toBe('["finance"]');
  });

  it("ignores malformed or outdated values", () => {
    expect(parseCollapsedGroups(null, NAV_GROUPS)).toEqual([]);
    expect(parseCollapsedGroups("not json", NAV_GROUPS)).toEqual([]);
    expect(parseCollapsedGroups('{"finance":true}', NAV_GROUPS)).toEqual([]);
    expect(parseCollapsedGroups('["finance","removed-module",7]', NAV_GROUPS)).toEqual([
      "finance",
    ]);
  });

  it("toggles a group without disturbing the others", () => {
    expect(toggleGroupCollapsed([], "finance")).toEqual(["finance"]);
    expect(toggleGroupCollapsed(["finance"], "finance")).toEqual([]);
    expect(toggleGroupCollapsed(["people"], "finance")).toEqual(["people", "finance"]);
  });
});

describe("user id from token", () => {
  it("extracts the user id claim", () => {
    expect(decodeUserId(tokenWith({ user_id: "user-123", role: "owner" }))).toBe("user-123");
  });

  it("returns null for missing, malformed, or empty claims", () => {
    expect(decodeUserId(null)).toBeNull();
    expect(decodeUserId("not-a-jwt")).toBeNull();
    expect(decodeUserId("header.not-base64!.signature")).toBeNull();
    expect(decodeUserId(tokenWith({ role: "owner" }))).toBeNull();
    expect(decodeUserId(tokenWith({ user_id: "" }))).toBeNull();
  });
});
