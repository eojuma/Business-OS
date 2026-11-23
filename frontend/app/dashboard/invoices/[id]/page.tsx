"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { useParams } from "next/navigation";
import { ArrowLeft, CheckCircle2, Clock, Smartphone } from "lucide-react";
import { api } from "@/lib/api";
import { PageHeader, formatMoney } from "@/components/app-shell";
import { usePermissions } from "@/hooks/usePermissions";
import { PERMISSIONS } from "@/lib/permissions";
import {
  invoiceCustomerName,
  invoiceDate,
  invoiceStatusClass,
  invoiceStatusLabel,
  type Invoice,
} from "@/lib/invoices";

/** Extracts the API error message with a safe fallback. */
function errorMessage(error: unknown, fallback: string): string {
  const e = error as { response?: { data?: { error?: string } } };
  return e.response?.data?.error || fallback;
}

/** Shows one invoice with its payments and lets the user apply a payment. */
export default function InvoiceDetailPage() {
  const params = useParams<{ id: string }>();
  const id = params?.id;
  const { can } = usePermissions();
  const canPay = can(PERMISSIONS.invoicesPayment);
  const [invoice, setInvoice] = useState<Invoice | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [amount, setAmount] = useState("");
  const [note, setNote] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [paymentError, setPaymentError] = useState("");
  const [payMode, setPayMode] = useState<"manual" | "mpesa">("manual");
  const [stkPhone, setStkPhone] = useState("");
  const [stkSuccess, setStkSuccess] = useState("");
  const [pollTimer, setPollTimer] = useState<NodeJS.Timeout | null>(null);

  /** Loads the invoice and resets the payment amount to the remainder. */
  const loadInvoice = useCallback(async () => {
    if (!id) return;
    setLoading(true);
    setError("");
    try {
      const res = await api.get(`/invoices/${id}`);
      const next = res.data.data as Invoice;
      setInvoice(next);
      // Reset the STK phone to the customer's current phone. If the customer
      // has no phone, clear the field so a stale value from a previous
      // invoice cannot be reused.
      setStkPhone(next.customer?.phone || "");
      if (next.remaining_balance > 0) {
        setAmount((next.remaining_balance / 100).toFixed(2));
      } else {
        setAmount("");
      }
    } catch (e) {
      setInvoice(null);
      setError(errorMessage(e, "Failed to load invoice"));
    } finally {
      setLoading(false);
    }
  }, [id]);

  useEffect(() => {
    void loadInvoice();
  }, [loadInvoice]);

  // Clear any pending STK reload timer on unmount so stale timers do not
  // trigger setState after the component has left.
  useEffect(() => {
    return () => {
      if (pollTimer) clearTimeout(pollTimer);
    };
  }, [pollTimer]);

  /** Applies a payment and refreshes the invoice from the response. */
  async function recordPayment(event: React.FormEvent) {
    event.preventDefault();
    if (!invoice) return;
    const cents = Math.round(Number(amount) * 100);
    if (!Number.isFinite(cents) || cents <= 0) {
      setPaymentError("Enter a valid payment amount");
      return;
    }
    setSubmitting(true);
    setPaymentError("");
    try {
      const res = await api.post(`/invoices/${invoice.id}/payments`, {
        amount: cents,
        note,
      });
      const next = res.data.data as Invoice;
      setInvoice(next);
      setNote("");
      setAmount(next.remaining_balance > 0 ? (next.remaining_balance / 100).toFixed(2) : "");
    } catch (e) {
      setPaymentError(errorMessage(e, "Failed to record payment"));
    } finally {
      setSubmitting(false);
    }
  }

  /** Triggers M-Pesa STK push prompt directly to the customer's phone. */
  async function triggerSTKPush(event: React.FormEvent) {
    event.preventDefault();
    if (!invoice) return;
    const cents = Math.round(Number(amount) * 100);
    if (!Number.isFinite(cents) || cents <= 0) {
      setPaymentError("Enter a valid payment amount");
      return;
    }
    if (!stkPhone.trim()) {
      setPaymentError("Enter customer phone number");
      return;
    }
    setSubmitting(true);
    setPaymentError("");
    setStkSuccess("");
    try {
      await api.post("/payments/mpesa/stk-push", {
        phone_number: stkPhone,
        amount: cents,
        account_reference: invoice.invoice_number,
        transaction_desc: `Payment for ${invoice.invoice_number}`,
        // Stable key so retries of this submit do not create duplicate
        // requests. The server accepts them as one idempotent operation.
        idempotency_key: `stk-${invoice.id}`,
      });
      setStkSuccess("M-Pesa STK push prompt sent! Payment will automatically reconcile upon confirmation.");
      // Poll up to three times with two-second gaps so a pending callback
      // has time to arrive before the UI falls back to manual refresh.
      let attempts = 0;
      const maxAttempts = 3;
      function tick() {
        const timer = setTimeout(async () => {
          attempts += 1;
          if (attempts >= maxAttempts) return;
          await loadInvoice();
          tick();
        }, 2000);
        setPollTimer(timer);
      }
      void tick();
    } catch (e) {
      setPaymentError(errorMessage(e, "Failed to send STK push"));
    } finally {
      setSubmitting(false);
    }
  }

  if (loading) return <p className="p-8 text-sm text-muted">Loading invoice...</p>;
  if (error) return <p className="p-8 text-sm text-danger">{error}</p>;
  if (!invoice) return <p className="p-8 text-sm text-muted">Invoice not found.</p>;

  const payments = invoice.payments || [];

  return (
    <>
      <Link
        href="/dashboard/invoices"
        className="mb-4 inline-flex items-center gap-1.5 text-sm font-semibold text-muted hover:text-foreground"
      >
        <ArrowLeft size={15} /> Back to invoices
      </Link>

      <PageHeader
        eyebrow="Invoice"
        title={invoice.invoice_number}
        description={`Issued ${invoiceDate(invoice.issue_date)} to ${invoiceCustomerName(invoice)}.`}
        action={
          <span className={`rounded-full px-3 py-1 text-xs font-bold ${invoiceStatusClass(invoice.status, invoice.is_overdue)}`}>
            {invoice.is_overdue ? "Overdue" : invoiceStatusLabel(invoice.status)}
          </span>
        }
      />

      {invoice.is_overdue && (
        <p className="mb-5 flex items-center gap-2 rounded-lg bg-danger-soft p-3 text-sm text-danger">
          <Clock size={16} /> This invoice was due on {invoiceDate(invoice.due_date)}.
        </p>
      )}

      <div className="grid gap-6 xl:grid-cols-[1.2fr_.8fr]">
        <section className="panel p-5">
          <div className="mb-5 flex items-start justify-between gap-4">
            <div>
              <p className="eyebrow">Bill to</p>
              <h2 className="mt-1 text-lg font-bold">{invoiceCustomerName(invoice)}</h2>
              {invoice.customer?.phone && <p className="text-sm text-muted">{invoice.customer.phone}</p>}
              {invoice.customer?.email && <p className="text-sm text-muted">{invoice.customer.email}</p>}
            </div>
            <div className="text-right text-sm text-muted">
              <p>Due {invoiceDate(invoice.due_date)}</p>
              <p className="mt-1 font-mono text-xs">{invoice.sale_id.slice(0, 8).toUpperCase()}</p>
            </div>
          </div>

          <dl className="space-y-2 border-t border-border-subtle pt-4 text-sm">
            <div className="flex justify-between">
              <dt className="text-muted">Subtotal</dt>
              <dd>{formatMoney(invoice.subtotal)}</dd>
            </div>
            <div className="flex justify-between">
              <dt className="text-muted">Discount</dt>
              <dd>-{formatMoney(invoice.discount)}</dd>
            </div>
            <div className="flex justify-between border-t border-border-subtle pt-2 text-base font-bold">
              <dt>Total</dt>
              <dd>{formatMoney(invoice.total_amount)}</dd>
            </div>
            <div className="flex justify-between text-primary">
              <dt>Paid</dt>
              <dd>{formatMoney(invoice.amount_paid)}</dd>
            </div>
            <div className="flex justify-between text-base font-bold">
              <dt>Remaining</dt>
              <dd>{formatMoney(invoice.remaining_balance)}</dd>
            </div>
          </dl>
        </section>

        <section className="panel p-5">
          <p className="eyebrow">Settlement</p>
          <h2 className="mt-1 text-lg font-bold">
            {invoice.remaining_balance > 0 ? "Record a payment" : "Invoice settled"}
          </h2>
          {invoice.remaining_balance > 0 && !canPay ? (
            <p className="mt-4 rounded-lg bg-warning-soft p-3 text-sm text-warning-strong">
              Your role cannot record invoice payments.
            </p>
          ) : invoice.remaining_balance > 0 ? (
            <div className="mt-4">
              <div className="mb-4 flex rounded-lg bg-surface-muted p-1 text-xs font-semibold">
                <button
                  type="button"
                  onClick={() => setPayMode("manual")}
                  className={`flex-1 rounded-md py-1.5 transition-colors ${
                    payMode === "manual" ? "bg-surface shadow text-foreground" : "text-muted hover:text-foreground"
                  }`}
                >
                  Manual Entry
                </button>
                <button
                  type="button"
                  onClick={() => setPayMode("mpesa")}
                  className={`flex-1 rounded-md py-1.5 transition-colors ${
                    payMode === "mpesa" ? "bg-surface shadow text-foreground" : "text-muted hover:text-foreground"
                  }`}
                >
                  M-Pesa STK Push
                </button>
              </div>

              {payMode === "manual" ? (
                <form onSubmit={recordPayment} className="space-y-3">
                  <label className="block text-sm font-semibold">
                    Amount (KSh)
                    <input
                      className="field mt-1.5"
                      type="number"
                      min="0.01"
                      step="0.01"
                      max={(invoice.remaining_balance / 100).toFixed(2)}
                      value={amount}
                      onChange={(e) => setAmount(e.target.value)}
                      required
                    />
                  </label>
                  <label className="block text-sm font-semibold">
                    Note <span className="font-normal text-subtle">(optional)</span>
                    <input
                      className="field mt-1.5"
                      value={note}
                      onChange={(e) => setNote(e.target.value)}
                      placeholder="e.g. M-Pesa reference"
                    />
                  </label>
                  {paymentError && <p className="rounded-lg bg-danger-soft p-3 text-sm text-danger">{paymentError}</p>}
                  <button className="btn-primary w-full" disabled={submitting}>
                    {submitting ? "Recording…" : "Record payment"}
                  </button>
                </form>
              ) : (
                <form onSubmit={triggerSTKPush} className="space-y-3">
                  <label className="block text-sm font-semibold">
                    Customer Phone Number
                    <input
                      className="field mt-1.5"
                      placeholder="07XXXXXXXX or 2547XXXXXXXX"
                      value={stkPhone}
                      onChange={(e) => setStkPhone(e.target.value)}
                      required
                    />
                  </label>
                  <label className="block text-sm font-semibold">
                    Amount (KSh)
                    <input
                      className="field mt-1.5"
                      type="number"
                      min="1"
                      step="0.01"
                      max={(invoice.remaining_balance / 100).toFixed(2)}
                      value={amount}
                      onChange={(e) => setAmount(e.target.value)}
                      required
                    />
                  </label>
                  {paymentError && <p className="rounded-lg bg-danger-soft p-3 text-sm text-danger">{paymentError}</p>}
                  {stkSuccess && (
                    <p className="rounded-lg bg-primary-soft p-3 text-sm text-primary">{stkSuccess}</p>
                  )}
                  <button className="btn-primary w-full flex items-center justify-center gap-2" disabled={submitting}>
                    <Smartphone size={16} />
                    {submitting ? "Sending prompt…" : "Send M-Pesa STK Prompt"}
                  </button>
                </form>
              )}
            </div>
          ) : (
            <p className="mt-4 flex items-center gap-2 rounded-lg bg-primary-soft p-3 text-sm text-primary">
              <CheckCircle2 size={16} /> This invoice has been fully paid.
            </p>
          )}
        </section>
      </div>

      <section className="panel mt-6 overflow-hidden">
        <div className="border-b border-border-subtle p-5">
          <p className="eyebrow">Payment history</p>
          <h2 className="mt-1 text-lg font-bold">Payments applied</h2>
        </div>
        {payments.length === 0 ? (
          <p className="p-8 text-sm text-muted">No payments recorded yet.</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm">
              <thead className="bg-surface-muted text-xs uppercase text-muted">
                <tr>
                  <th className="px-5 py-3">Date</th>
                  <th className="px-5 py-3">Note</th>
                  <th className="px-5 py-3 text-right">Amount</th>
                </tr>
              </thead>
              <tbody>
                {payments.map((payment) => (
                  <tr key={payment.id} className="border-t border-border-subtle">
                    <td className="px-5 py-4 text-muted">{invoiceDate(payment.paid_at)}</td>
                    <td className="px-5 py-4">{payment.note || "—"}</td>
                    <td className="px-5 py-4 text-right font-semibold">{formatMoney(payment.amount)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </>
  );
}
