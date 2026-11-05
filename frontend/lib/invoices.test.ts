import { describe, expect, it } from "vitest";
import {
  invoiceCustomerName,
  invoiceDate,
  invoiceStatusClass,
  invoiceStatusLabel,
} from "./invoices";

describe("invoice presentation", () => {
  it("labels known statuses and preserves unknown values", () => {
    expect(invoiceStatusLabel("unpaid")).toBe("Unpaid");
    expect(invoiceStatusLabel("partial")).toBe("Partial");
    expect(invoiceStatusLabel("paid")).toBe("Paid");
    expect(invoiceStatusLabel("void")).toBe("void");
  });

  it("uses a danger style for overdue unpaid invoices only", () => {
    expect(invoiceStatusClass("unpaid", true)).toContain("danger");
    expect(invoiceStatusClass("partial", true)).toContain("danger");
    expect(invoiceStatusClass("paid", true)).toContain("primary");
    expect(invoiceStatusClass("unpaid")).toContain("surface-muted");
  });

  it("falls back to a walk-in label when no customer is linked", () => {
    expect(invoiceCustomerName({ customer: null })).toBe("Walk-in customer");
    expect(invoiceCustomerName({ customer: { id: "1", name: "Acme" } })).toBe("Acme");
  });

  it("formats issue dates", () => {
    expect(invoiceDate("2026-11-05T00:00:00Z")).toContain("Nov");
  });
});
