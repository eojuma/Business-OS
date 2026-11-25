export type MpesaTransaction = {
  id: string;
  business_id: string;
  transaction_type: "stk_push" | "c2b" | "b2c";
  merchant_request_id?: string;
  checkout_request_id?: string;
  mpesa_receipt_number?: string;
  phone_number: string;
  amount: number;
  status: "pending" | "completed" | "failed" | "cancelled";
  result_code?: number;
  result_desc?: string;
  reconciliation_status: "reconciled" | "unmatched" | "ignored" | "manual";
  account_reference?: string;
  created_at: string;
};

export type BankTransaction = {
  id: string;
  business_id: string;
  bank_name: string;
  reference: string;
  type: "inflow" | "outflow";
  amount: number;
  description: string;
  transacted_at: string;
  reconciliation_status: "reconciled" | "unmatched" | "ignored" | "manual";
};

export type ReviewQueueItem = {
  id: string;
  source: "mpesa" | "bank";
  reference: string;
  counterparty: string;
  amount: number;
  type: string;
  description: string;
  transacted_at: string;
  status: string;
};

/** Returns human-readable label for payment and transaction types. */
export function paymentTypeLabel(type: string): string {
  switch (type.toLowerCase()) {
    case "stk_push":
      return "STK Push";
    case "c2b":
      return "Paybill / Till (C2B)";
    case "b2c":
      return "Payout (B2C)";
    case "inflow":
      return "Bank Deposit";
    case "outflow":
      return "Bank Withdrawal";
    default:
      return type;
  }
}

/** Returns a human-readable bank-transfer message based on reconciliation state. */
export function bankTransferMessage(amount: number, reconciliationStatus: string): string {
  const amountLabel = `Bank transfer of ${(amount / 100).toLocaleString("en-KE", { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;
  if (reconciliationStatus.toLowerCase() === "reconciled") {
    return `${amountLabel} recorded and reconciled.`;
  }
  return `${amountLabel} recorded and queued for manual reconciliation.`;
}

/** Returns styling classes for reconciliation status badges. */
export function reconciliationStatusClass(status: string): string {
  switch (status.toLowerCase()) {
    case "reconciled":
      return "bg-success-soft text-success";
    case "unmatched":
      return "bg-warning-soft text-warning-strong";
    case "manual":
      return "bg-primary-soft text-primary";
    case "ignored":
    default:
      return "bg-surface-muted text-muted";
  }
}

/** Formats a Kenyan mobile number for display. */
export function formatPhone(phone: string): string {
  const cleaned = phone.replace(/\D/g, "");
  if (cleaned.startsWith("254") && cleaned.length === 12) {
    return `+254 ${cleaned.slice(3, 6)} ${cleaned.slice(6, 9)} ${cleaned.slice(9)}`;
  }
  if (cleaned.startsWith("0") && cleaned.length === 10) {
    return `0${cleaned.slice(1, 4)} ${cleaned.slice(4, 7)} ${cleaned.slice(7)}`;
  }
  return phone;
}
