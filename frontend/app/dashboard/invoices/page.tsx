"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { FileText, Filter, X } from "lucide-react";
import { api } from "@/lib/api";
import { PageHeader, formatMoney } from "@/components/app-shell";
import {
  invoiceCustomerName,
  invoiceDate,
  invoiceStatusClass,
  invoiceStatusLabel,
  type Invoice,
} from "@/lib/invoices";

type Customer = { id: string; name: string };
type Meta = { page: number; per_page: number; total: number };

/** Extracts the API error message with a safe fallback. */
function errorMessage(error: unknown, fallback: string): string {
  const e = error as { response?: { data?: { error?: string } } };
  return e.response?.data?.error || fallback;
}

/** Lists invoices with status, customer and issue-date filters. */
export default function InvoicesPage() {
  const [invoices, setInvoices] = useState<Invoice[]>([]);
  const [customers, setCustomers] = useState<Customer[]>([]);
  const [meta, setMeta] = useState<Meta | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const [status, setStatus] = useState("");
  const [customerId, setCustomerId] = useState("");
  const [dateFrom, setDateFrom] = useState("");
  const [dateTo, setDateTo] = useState("");
  const [page, setPage] = useState(1);
  const limit = 20;

  /** Loads invoices for the current filter and page. */
  const loadInvoices = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const params: Record<string, string | number> = { page, limit };
      if (status) params.status = status;
      if (customerId) params.customer_id = customerId;
      if (dateFrom) params.date_from = dateFrom;
      if (dateTo) params.date_to = dateTo;
      const res = await api.get("/invoices", { params });
      setInvoices(res.data.data || []);
      setMeta(res.data.meta || null);
    } catch (e) {
      setInvoices([]);
      setMeta(null);
      setError(errorMessage(e, "Failed to load invoices"));
    } finally {
      setLoading(false);
    }
  }, [customerId, dateFrom, dateTo, page, status]);

  useEffect(() => {
    void loadInvoices();
  }, [loadInvoices]);

  useEffect(() => {
    let active = true;
    api
      .get("/customers")
      .then((res) => {
        if (active) setCustomers(res.data.data || []);
      })
      .catch(() => {
        if (active) setCustomers([]);
      });
    return () => {
      active = false;
    };
  }, []);

  const totalPages = meta ? Math.max(1, Math.ceil(meta.total / meta.per_page)) : 1;

  /** Clears every list filter and returns to the first page. */
  function resetFilters() {
    setStatus("");
    setCustomerId("");
    setDateFrom("");
    setDateTo("");
    setPage(1);
  }

  return (
    <>
      <PageHeader
        eyebrow="Receivables"
        title="Invoices"
        description="Track what customers owe, apply payments, and spot overdue balances."
      />

      {error && (
        <p className="mb-4 flex items-start justify-between gap-3 rounded-lg bg-danger-soft p-3 text-sm text-danger">
          <span>{error}</span>
          <button type="button" aria-label="Dismiss error" onClick={() => setError("")}>
            <X size={15} />
          </button>
        </p>
      )}

      <section className="panel mb-6 p-4">
        <div className="mb-3 flex items-center gap-2 text-sm font-semibold">
          <Filter size={15} /> Filters
        </div>
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
          <label className="text-xs font-semibold text-muted">
            Status
            <select
              className="field mt-1.5"
              value={status}
              onChange={(e) => {
                setStatus(e.target.value);
                setPage(1);
              }}
            >
              <option value="">All statuses</option>
              <option value="unpaid">Unpaid</option>
              <option value="partial">Partial</option>
              <option value="paid">Paid</option>
              <option value="overdue">Overdue</option>
            </select>
          </label>
          <label className="text-xs font-semibold text-muted">
            Customer
            <select
              className="field mt-1.5"
              value={customerId}
              onChange={(e) => {
                setCustomerId(e.target.value);
                setPage(1);
              }}
            >
              <option value="">All customers</option>
              {customers.map((customer) => (
                <option key={customer.id} value={customer.id}>
                  {customer.name}
                </option>
              ))}
            </select>
          </label>
          <label className="text-xs font-semibold text-muted">
            Issued from
            <input
              type="date"
              className="field mt-1.5"
              value={dateFrom}
              onChange={(e) => {
                setDateFrom(e.target.value);
                setPage(1);
              }}
            />
          </label>
          <label className="text-xs font-semibold text-muted">
            Issued to
            <input
              type="date"
              className="field mt-1.5"
              value={dateTo}
              onChange={(e) => {
                setDateTo(e.target.value);
                setPage(1);
              }}
            />
          </label>
          <div className="flex items-end">
            <button type="button" className="btn-secondary w-full" onClick={resetFilters}>
              Clear filters
            </button>
          </div>
        </div>
      </section>

      <section className="panel overflow-hidden">
        <div className="flex items-center justify-between border-b border-border-subtle p-5">
          <div>
            <p className="eyebrow">Receivables ledger</p>
            <h2 className="mt-1 text-lg font-bold">All invoices</h2>
          </div>
          <span className="text-sm text-muted">{meta?.total ?? 0} total</span>
        </div>

        {loading ? (
          <p className="p-8 text-sm text-muted">Loading invoices...</p>
        ) : invoices.length === 0 ? (
          <div className="p-10 text-center text-sm text-muted">
            <FileText className="mx-auto mb-2" />
            No invoices match these filters.
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[820px] text-left text-sm">
              <thead className="bg-surface-muted text-xs uppercase text-muted">
                <tr>
                  <th className="px-5 py-3">Invoice</th>
                  <th className="px-5 py-3">Customer</th>
                  <th className="px-5 py-3">Issued</th>
                  <th className="px-5 py-3">Due</th>
                  <th className="px-5 py-3 text-right">Total</th>
                  <th className="px-5 py-3 text-right">Paid</th>
                  <th className="px-5 py-3 text-right">Remaining</th>
                  <th className="px-5 py-3 text-right">Status</th>
                </tr>
              </thead>
              <tbody>
                {invoices.map((invoice) => (
                  <tr key={invoice.id} className="border-t border-border-subtle hover:bg-hover">
                    <td className="px-5 py-4">
                      <Link
                        href={`/dashboard/invoices/${invoice.id}`}
                        className="font-mono text-xs font-semibold text-primary hover:underline"
                      >
                        {invoice.invoice_number}
                      </Link>
                    </td>
                    <td className="px-5 py-4">{invoiceCustomerName(invoice)}</td>
                    <td className="px-5 py-4 text-muted">{invoiceDate(invoice.issue_date)}</td>
                    <td className="px-5 py-4 text-muted">
                      <span className={invoice.is_overdue ? "font-semibold text-danger" : ""}>
                        {invoiceDate(invoice.due_date)}
                      </span>
                    </td>
                    <td className="px-5 py-4 text-right font-semibold">{formatMoney(invoice.total_amount)}</td>
                    <td className="px-5 py-4 text-right text-muted">{formatMoney(invoice.amount_paid)}</td>
                    <td className="px-5 py-4 text-right font-semibold">{formatMoney(invoice.remaining_balance)}</td>
                    <td className="px-5 py-4 text-right">
                      <span className={`rounded-full px-2.5 py-1 text-xs font-bold ${invoiceStatusClass(invoice.status, invoice.is_overdue)}`}>
                        {invoice.is_overdue ? "Overdue" : invoiceStatusLabel(invoice.status)}
                      </span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}

        {meta && meta.total > 0 && (
          <div className="flex items-center justify-between border-t border-border-subtle p-4 text-sm text-muted">
            <span>
              Page {meta.page} of {totalPages}
            </span>
            <div className="flex gap-2">
              <button
                type="button"
                className="btn-secondary py-1.5"
                disabled={page <= 1}
                onClick={() => setPage((current) => Math.max(1, current - 1))}
              >
                Previous
              </button>
              <button
                type="button"
                className="btn-secondary py-1.5"
                disabled={page >= totalPages}
                onClick={() => setPage((current) => Math.min(totalPages, current + 1))}
              >
                Next
              </button>
            </div>
          </div>
        )}
      </section>
    </>
  );
}
