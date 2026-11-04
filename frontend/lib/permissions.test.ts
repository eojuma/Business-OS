import { describe, expect, it } from "vitest";
import {
  PERMISSIONS,
  ROLE_LABELS,
  ROLES,
  can,
  isRole,
  roleLabel,
} from "./permissions";

describe("frontend permission matrix", () => {
  it("mirrors the backend roles", () => {
    expect([...ROLES]).toEqual([
      "owner",
      "admin",
      "manager",
      "cashier",
      "accountant",
      "warehouse",
    ]);
  });

  it("grants owners and admins every permission", () => {
    for (const role of ["owner", "admin"] as const) {
      for (const permission of Object.values(PERMISSIONS)) {
        expect(can(role, permission), `${role} / ${permission}`).toBe(true);
      }
    }
  });

  it("keeps sensitive actions with the right roles", () => {
    expect(can("cashier", PERMISSIONS.salesCreate)).toBe(true);
    expect(can("cashier", PERMISSIONS.financeRead)).toBe(false);
    expect(can("cashier", PERMISSIONS.teamManage)).toBe(false);

    expect(can("accountant", PERMISSIONS.financeManage)).toBe(true);
    expect(can("accountant", PERMISSIONS.financeDelete)).toBe(false);
    expect(can("accountant", PERMISSIONS.teamManage)).toBe(false);

    expect(can("warehouse", PERMISSIONS.inventoryAdjust)).toBe(true);
    expect(can("warehouse", PERMISSIONS.purchasesReceive)).toBe(true);
    expect(can("warehouse", PERMISSIONS.purchasesCreate)).toBe(false);
    expect(can("warehouse", PERMISSIONS.salesCreate)).toBe(false);

    expect(can("manager", PERMISSIONS.teamRead)).toBe(true);
    expect(can("manager", PERMISSIONS.teamManage)).toBe(false);
  });

  it("rejects unknown or missing roles", () => {
    expect(can(null, PERMISSIONS.salesRead)).toBe(false);
    expect(can(undefined, PERMISSIONS.salesRead)).toBe(false);
    expect(can("superuser", PERMISSIONS.salesRead)).toBe(false);
    expect(isRole("nope")).toBe(false);
  });

  it("labels roles for the team UI", () => {
    expect(roleLabel("cashier")).toBe("Sales / Cashier");
    expect(roleLabel("accountant")).toBe(ROLE_LABELS.accountant);
    expect(roleLabel("removed-role")).toBe("removed-role");
    expect(roleLabel(null)).toBe("Unknown");
  });
});
