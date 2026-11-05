export type InvoiceStatus = "unpaid" | "partial" | "paid";

export type InvoiceCustomer = {
  id: string;
  name: string;
  phone?: string;
  email?: string;
};

export type InvoicePayment = {
  id: string;
  amount: number;
  note?: string;
  paid_at: string;
};

export type Invoice = {
  id: string;
  sale_id: string;
  invoice_number: string;
  customer_id?: string | null;
  customer?: InvoiceCustomer | null;
  status: InvoiceStatus;
  issue_date: string;
  due_date: string;
  subtotal: number;
  discount: number;
  total_amount: number;
  amount_paid: number;
  remaining_balance: number;
  is_overdue: boolean;
  payments?: InvoicePayment[];
};

export const INVOICE_STATUS_LABELS: Record<InvoiceStatus, string> = {
  unpaid: "Unpaid",
  partial: "Partial",
  paid: "Paid",
};

/** Returns the display label for a known invoice status. */
export function invoiceStatusLabel(status: string): string {
  return INVOICE_STATUS_LABELS[status as InvoiceStatus] ?? status;
}

/** Returns the badge classes for a status, giving overdue invoices priority. */
export function invoiceStatusClass(status: string, overdue = false): string {
  if (overdue && status !== "paid") {
    return "bg-danger-soft text-danger";
  }
  switch (status) {
    case "paid":
      return "bg-primary-soft text-primary";
    case "partial":
      return "bg-warning-soft text-warning-strong";
    default:
      return "bg-surface-muted text-muted";
  }
}

/** Returns the invoice customer name or the walk-in fallback. */
export function invoiceCustomerName(invoice: Pick<Invoice, "customer">): string {
  return invoice.customer?.name || "Walk-in customer";
}

/** Formats an invoice timestamp for the current locale. */
export function invoiceDate(value: string): string {
  return new Date(value).toLocaleDateString("en-KE", {
    day: "numeric",
    month: "short",
    year: "numeric",
  });
}
