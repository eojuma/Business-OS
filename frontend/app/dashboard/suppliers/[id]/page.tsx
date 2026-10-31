"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { ArrowLeft } from "lucide-react";
import { api } from "@/lib/api";
import { PageHeader, formatMoney } from "@/components/app-shell";

type Supplier = {
  id: string;
  name: string;
  phone: string;
  email?: string;
  address?: string;
  outstanding_balance: number;
  payment_terms: string;
  due_date?: string | null;
};
type Payment = { id: string; amount: number; note?: string; created_at: string };

function formatDate(value: string): string {
  return new Date(value).toLocaleDateString("en-KE", { day: "numeric", month: "short", year: "numeric", timeZone: "UTC" });
}

export default function SupplierDetailPage({ params }: { params: { id: string } }) {
  const [supplier, setSupplier] = useState<Supplier | null>(null);
  const [payments, setPayments] = useState<Payment[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [supplierReload, setSupplierReload] = useState(0);
  const [historyLoading, setHistoryLoading] = useState(true);
  const [historyError, setHistoryError] = useState(false);
  const [historyReload, setHistoryReload] = useState(0);
  const [asOf, setAsOf] = useState<string | null>(null);

  // Keep the aging report's selected date when navigating back.
  useEffect(() => {
    const value = new URLSearchParams(window.location.search).get("as_of");
    setAsOf(value && /^\d{4}-\d{2}-\d{2}$/.test(value) ? value : null);
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError("");
    setSupplier(null);
    api
      .get(`/suppliers/${encodeURIComponent(params.id)}`, { signal: controller.signal })
      .then((response) => {
        if (!controller.signal.aborted) setSupplier(response.data.data);
      })
      .catch((reason: unknown) => {
        if (controller.signal.aborted) return;
        const status = (reason as { response?: { status?: number } }).response?.status;
        setError(status === 404 ? "Supplier not found." : "Could not load this supplier. Please try again.");
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [params.id, supplierReload]);

  // Payment history loads and retries independently so a slow or failing
  // history request never hides the supplier details.
  useEffect(() => {
    const controller = new AbortController();
    setHistoryLoading(true);
    setHistoryError(false);
    setPayments([]);
    api
      .get(`/suppliers/${encodeURIComponent(params.id)}/payments`, { signal: controller.signal })
      .then((response) => {
        if (!controller.signal.aborted) setPayments(response.data.data || []);
      })
      .catch(() => {
        if (!controller.signal.aborted) setHistoryError(true);
      })
      .finally(() => {
        if (!controller.signal.aborted) setHistoryLoading(false);
      });
    return () => controller.abort();
  }, [params.id, historyReload]);

  const backHref = asOf
    ? `/dashboard/suppliers?tab=aging&as_of=${asOf}`
    : "/dashboard/suppliers?tab=aging";

  return (
    <>
      <Link href={backHref} className="mb-5 inline-flex items-center gap-2 text-sm font-medium text-primary hover:underline">
        <ArrowLeft size={16} /> Back to aging report
      </Link>
      <PageHeader eyebrow="Suppliers" title={supplier?.name || "Supplier details"} description="Outstanding balance, payment terms and recorded payments." />
      {loading ? (
        <p className="panel p-10 text-center text-muted" role="status">Loading supplier…</p>
      ) : error ? (
        <div className="panel p-8 text-center" role="alert">
          <p>{error}</p>
          <button type="button" className="btn-secondary mt-4" onClick={() => setSupplierReload((value) => value + 1)}>Try again</button>
        </div>
      ) : supplier && (
        <div className="space-y-6">
          <dl className="grid gap-4 sm:grid-cols-3">
            <div className="panel p-5"><dt className="text-sm text-muted">Outstanding balance</dt><dd className="mt-2 break-words text-2xl font-bold">{formatMoney(supplier.outstanding_balance)}</dd></div>
            <div className="panel p-5"><dt className="text-sm text-muted">Saved due date (UTC)</dt><dd className="mt-2 text-lg font-semibold">{supplier.due_date ? formatDate(supplier.due_date) : "No due date"}</dd></div>
            <div className="panel p-5"><dt className="text-sm text-muted">Payment terms</dt><dd className="mt-2 text-lg font-semibold">{supplier.payment_terms === "due_on_receipt" ? "Due on receipt" : supplier.payment_terms.replace(/^net/, "Net ")}</dd></div>
          </dl>
          <section className="panel p-5" aria-labelledby="contact-heading">
            <h2 id="contact-heading" className="font-semibold">Contact details</h2>
            <dl className="mt-4 grid gap-4 text-sm sm:grid-cols-3">
              <div><dt className="text-muted">Phone</dt><dd className="mt-1 break-words">{supplier.phone || "Not provided"}</dd></div>
              <div><dt className="text-muted">Email</dt><dd className="mt-1 break-words">{supplier.email || "Not provided"}</dd></div>
              <div><dt className="text-muted">Address</dt><dd className="mt-1 break-words">{supplier.address || "Not provided"}</dd></div>
            </dl>
          </section>
          <section className="panel overflow-hidden" aria-labelledby="payments-heading">
            <h2 id="payments-heading" className="border-b border-border-subtle px-5 py-4 font-semibold">Payment history</h2>
            {historyLoading ? (
              <p className="p-5 text-sm text-muted" role="status">Loading payments…</p>
            ) : historyError ? (
              <div className="p-5" role="alert"><p className="text-sm text-danger">Could not load payment history.</p><button type="button" className="btn-secondary mt-3" onClick={() => setHistoryReload((value) => value + 1)}>Try again</button></div>
            ) : payments.length === 0 ? (
              <p className="p-5 text-sm text-muted">No payments recorded.</p>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full min-w-[440px] text-left text-sm">
                  <caption className="sr-only">Recorded payments for {supplier.name}</caption>
                  <thead className="bg-surface-muted text-xs uppercase text-muted"><tr><th scope="col" className="px-5 py-3">Date (UTC)</th><th scope="col" className="px-5 py-3">Note</th><th scope="col" className="px-5 py-3 text-right">Amount</th></tr></thead>
                  <tbody>{payments.map((payment) => (
                    <tr key={payment.id} className="border-t border-border-subtle"><td className="px-5 py-4">{formatDate(payment.created_at)}</td><td className="px-5 py-4 text-muted">{payment.note || "—"}</td><td className="px-5 py-4 text-right font-medium">{formatMoney(payment.amount)}</td></tr>
                  ))}</tbody>
                </table>
              </div>
            )}
          </section>
        </div>
      )}
    </>
  );
}
