import { describe, expect, it } from "vitest";
import {
  AUDIT_ACTIONS,
  AUDIT_ENTITIES,
  auditActionLabel,
  auditEntityLabel,
  auditTimestamp,
} from "./audit";

describe("audit presentation", () => {
  it("labels known actions and keeps unknown values", () => {
    expect(auditActionLabel("role_change")).toBe("Role change");
    expect(auditActionLabel("payment")).toBe("Payment");
    expect(auditActionLabel("custom")).toBe("custom");
    expect(AUDIT_ACTIONS).toContain("assistant_commit");
  });

  it("labels known entity types and keeps unknown values", () => {
    expect(auditEntityLabel("invoice")).toBe("Invoice");
    expect(auditEntityLabel("stock_movement")).toBe("Stock movement");
    expect(auditEntityLabel("widget")).toBe("widget");
    expect(AUDIT_ENTITIES).toContain("purchase");
  });

  it("formats timestamps", () => {
    expect(auditTimestamp("2026-11-05T10:00:00Z")).toContain("2026");
  });
});
