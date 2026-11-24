import { describe, expect, it } from "vitest";
import { bankTransferMessage, formatPhone, paymentTypeLabel, reconciliationStatusClass } from "./payments";

describe("payments presentation helpers", () => {
  it("formats payment and transaction type labels", () => {
    expect(paymentTypeLabel("stk_push")).toBe("STK Push");
    expect(paymentTypeLabel("c2b")).toBe("Paybill / Till (C2B)");
    expect(paymentTypeLabel("b2c")).toBe("Payout (B2C)");
    expect(paymentTypeLabel("inflow")).toBe("Bank Deposit");
    expect(paymentTypeLabel("outflow")).toBe("Bank Withdrawal");
    expect(paymentTypeLabel("custom")).toBe("custom");
  });

  it("applies the expected badge styling for reconciliation statuses", () => {
    expect(reconciliationStatusClass("reconciled")).toContain("success");
    expect(reconciliationStatusClass("unmatched")).toContain("warning");
    expect(reconciliationStatusClass("manual")).toContain("primary");
    expect(reconciliationStatusClass("ignored")).toContain("surface-muted");
  });

  it("describes bank transfers using the reconciliation status", () => {
    expect(bankTransferMessage(1200, "reconciled")).toBe("Bank transfer of 1,200.00 recorded and reconciled.");
    expect(bankTransferMessage(1200, "manual")).toBe(
      "Bank transfer of 1,200.00 recorded and queued for manual reconciliation.",
    );
  });

  it("formats Kenyan phone numbers into readable groups", () => {
    expect(formatPhone("254712345678")).toBe("+254 712 345 678");
    expect(formatPhone("0712345678")).toBe("0712 345 678");
    expect(formatPhone("12345")).toBe("12345");
  });
});
