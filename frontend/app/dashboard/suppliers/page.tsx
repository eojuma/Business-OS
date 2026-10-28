"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import {
  AlertCircle,
  CheckCircle2,
  History,
  Plus,
  Search,
  Truck,
  WalletCards,
  X,
} from "lucide-react";
import { api } from "@/lib/api";
import { PageHeader, formatMoney } from "@/components/app-shell";

type Supplier = {
  id: string;
  name: string;
  phone: string;
  email: string;
  address: string;
  outstanding_balance: number;
  payment_terms: string;
  due_date?: string | null;
  last_received_at?: string | null;
};

type Payment = {
  id: string;
  amount: number;
  note?: string;
  created_at: string;
};

const FOCUSABLE_SELECTOR =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

type Meta = {
  page: number;
  limit: number;
  total: number;
  totalPages: number;
};

type Toast = {
  id: number;
  tone: "success" | "error";
  message: string;
};

const PAGE_SIZES = [10, 20, 50];

const paymentTermOptions = [
  { value: "due_on_receipt", label: "Due on receipt" },
  { value: "net15", label: "Net 15" },
  { value: "net30", label: "Net 30" },
  { value: "net45", label: "Net 45" },
  { value: "net60", label: "Net 60" },
  { value: "net90", label: "Net 90" },
];

const paymentTermLabels: Record<string, string> = {
  due_on_receipt: "Due on receipt",
  net15: "Net 15",
  net30: "Net 30",
  net45: "Net 45",
  net60: "Net 60",
  net90: "Net 90",
};

function formatTerms(terms: string): string {
  return paymentTermLabels[terms] ?? terms;
}

function formatDate(value?: string | null): string {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  return date.toLocaleDateString("en-KE", {
    day: "numeric",
    month: "short",
    year: "numeric",
  });
}

function isOverdue(dueDate: string | null | undefined, balance: number): boolean {
  if (!dueDate || balance <= 0) return false;
  const due = new Date(dueDate);
  if (Number.isNaN(due.getTime())) return false;
  const today = new Date();
  due.setHours(0, 0, 0, 0);
  today.setHours(0, 0, 0, 0);
  return due.getTime() < today.getTime();
}

export default function SuppliersPage() {
  const [items, setItems] = useState<Supplier[]>([]);
  const [meta, setMeta] = useState<Meta>({ page: 1, limit: 20, total: 0, totalPages: 0 });
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const [reloadKey, setReloadKey] = useState(0);

  const [query, setQuery] = useState("");
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(1);
  const [limit, setLimit] = useState(20);
  const [pageInput, setPageInput] = useState("1");

  const [showCreate, setShowCreate] = useState(false);
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState("");
  const [phone, setPhone] = useState("");
  const [email, setEmail] = useState("");
  const [terms, setTerms] = useState("net30");

  const [paymentSupplier, setPaymentSupplier] = useState<Supplier | null>(null);
  const [amount, setAmount] = useState("");
  const [note, setNote] = useState("");
  const [paymentError, setPaymentError] = useState("");
  const [savingPayment, setSavingPayment] = useState(false);
  const [payments, setPayments] = useState<Payment[]>([]);
  const [historyLoading, setHistoryLoading] = useState(false);
  const [historyError, setHistoryError] = useState("");

  const [toasts, setToasts] = useState<Toast[]>([]);

  const firstQueryRef = useRef(true);
  const toastIdRef = useRef(0);
  const historyRequestRef = useRef(0);
  const paymentDialogRef = useRef(0);
  const dialogFocusedRef = useRef(0);
  const paymentPanelRef = useRef<HTMLDivElement>(null);
  const paymentOpenerRef = useRef<HTMLElement | null>(null);
  const amountRef = useRef<HTMLInputElement>(null);
  const closeButtonRef = useRef<HTMLButtonElement>(null);

  const pushToast = useCallback((tone: "success" | "error", message: string) => {
    const id = ++toastIdRef.current;
    setToasts((previous) => [...previous, { id, tone, message }]);
    window.setTimeout(() => {
      setToasts((previous) => previous.filter((toast) => toast.id !== id));
    }, 4500);
  }, []);

  function dismissToast(id: number) {
    setToasts((previous) => previous.filter((toast) => toast.id !== id));
  }

  // Debounce the search box so typing does not fire a request per keystroke.
  useEffect(() => {
    if (firstQueryRef.current) {
      firstQueryRef.current = false;
      return;
    }
    const timer = window.setTimeout(() => {
      setPage(1);
      setSearch(query.trim());
    }, 300);
    return () => window.clearTimeout(timer);
  }, [query]);

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setLoadError("");

    api
      .get("/suppliers", {
        params: { page, limit, search: search || undefined },
        signal: controller.signal,
      })
      .then((response) => {
        // A paginated response wraps the rows and metadata inside `data`;
        // fall back to a bare array for the unpaginated shape.
        const payload = response.data?.data;
        const paginated = payload && !Array.isArray(payload) ? payload : undefined;
        const nextItems: Supplier[] = Array.isArray(payload)
          ? payload
          : Array.isArray(paginated?.data)
            ? paginated.data
            : [];
        const nextMeta: Meta =
          paginated?.meta || {
            page,
            limit,
            total: nextItems.length,
            totalPages: nextItems.length > 0 ? 1 : 0,
          };
        setItems(nextItems);
        setMeta(nextMeta);
      })
      .catch((error: any) => {
        if (error?.code === "ERR_CANCELED" || error?.name === "CanceledError") return;
        const message =
          error.response?.data?.error ||
          "Could not load suppliers. Check your connection and try again.";
        setLoadError(message);
        pushToast("error", message);
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });

    return () => controller.abort();
  }, [page, limit, search, reloadKey, pushToast]);

  useEffect(() => {
    setPageInput(String(page));
  }, [page]);

  // A filtered list can leave the current page out of range.
  useEffect(() => {
    if (page > Math.max(1, meta.totalPages)) {
      setPage(Math.max(1, meta.totalPages));
    }
  }, [meta.totalPages, page]);

  const closePayments = useCallback(() => {
    historyRequestRef.current += 1;
    paymentDialogRef.current += 1;
    setPaymentSupplier(null);
    setAmount("");
    setNote("");
    setPaymentError("");
    setSavingPayment(false);
    setPayments([]);
    setHistoryLoading(false);
  }, []);

  useEffect(() => {
    if (!paymentSupplier) return;
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";

    // Focus once per opened dialog: the amount input, or the close button when
    // the amount is disabled because there is no balance to pay.
    if (dialogFocusedRef.current !== paymentDialogRef.current) {
      dialogFocusedRef.current = paymentDialogRef.current;
      if (paymentSupplier.outstanding_balance <= 0) {
        closeButtonRef.current?.focus();
      } else {
        amountRef.current?.focus();
      }
    }

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        closePayments();
        return;
      }
      if (event.key !== "Tab") return;

      const panel = paymentPanelRef.current;
      if (!panel) return;
      const focusable = Array.from(
        panel.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR),
      ).filter((element) => element.offsetParent !== null);
      if (focusable.length === 0) {
        event.preventDefault();
        panel.focus();
        return;
      }

      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      const active = document.activeElement;
      const inside = active instanceof HTMLElement && active !== panel && panel.contains(active);
      if (event.shiftKey) {
        if (active === first || !inside) {
          event.preventDefault();
          last.focus();
        }
      } else if (active === last || !inside) {
        event.preventDefault();
        first.focus();
      }
    };
    window.addEventListener("keydown", onKeyDown);

    return () => {
      document.body.style.overflow = previousOverflow;
      window.removeEventListener("keydown", onKeyDown);
    };
  }, [paymentSupplier, closePayments]);

  // Return focus to the control that opened the dialog when it closes.
  useEffect(() => {
    if (paymentSupplier) return;
    const opener = paymentOpenerRef.current;
    paymentOpenerRef.current = null;
    opener?.focus();
  }, [paymentSupplier]);

  async function createSupplier(event: React.FormEvent) {
    event.preventDefault();
    const supplierName = name.trim();
    if (!supplierName) return;

    setCreating(true);
    try {
      await api.post("/suppliers", {
        name: supplierName,
        phone: phone.trim(),
        email: email.trim(),
        payment_terms: terms,
      });
      pushToast("success", `${supplierName} added.`);
      setName("");
      setPhone("");
      setEmail("");
      setTerms("net30");
      setShowCreate(false);
      setPage(1);
      setReloadKey((value) => value + 1);
    } catch (error: any) {
      pushToast(
        "error",
        error.response?.data?.error || "Could not add the supplier. Try again.",
      );
    } finally {
      setCreating(false);
    }
  }

  const loadPayments = useCallback((supplier: Supplier) => {
    setHistoryLoading(true);
    setHistoryError("");

    const requestId = ++historyRequestRef.current;
    api
      .get(`/suppliers/${supplier.id}/payments`)
      .then((response) => {
        if (historyRequestRef.current === requestId) {
          setPayments(response.data.data || []);
        }
      })
      .catch((error: any) => {
        if (historyRequestRef.current === requestId) {
          setHistoryError(
            error.response?.data?.error || "Could not load payment history.",
          );
        }
      })
      .finally(() => {
        if (historyRequestRef.current === requestId) setHistoryLoading(false);
      });
  }, []);

  function openPayments(supplier: Supplier) {
    paymentDialogRef.current += 1;
    paymentOpenerRef.current =
      document.activeElement instanceof HTMLElement ? document.activeElement : null;
    setPaymentSupplier(supplier);
    setAmount("");
    setNote("");
    setPaymentError("");
    setSavingPayment(false);
    setPayments([]);
    setHistoryError("");
    loadPayments(supplier);
  }

  async function submitPayment(event: React.FormEvent) {
    event.preventDefault();
    if (!paymentSupplier || savingPayment) return;

    const supplier = paymentSupplier;
    const dialogId = paymentDialogRef.current;
    const cents = Math.round(Number(amount) * 100);
    if (!Number.isFinite(cents) || cents <= 0) {
      setPaymentError("Enter an amount greater than zero.");
      return;
    }
    if (cents > supplier.outstanding_balance) {
      setPaymentError(
        `Amount cannot exceed the outstanding balance of ${formatMoney(
          supplier.outstanding_balance,
        )}.`,
      );
      return;
    }

    setPaymentError("");
    setSavingPayment(true);
    try {
      const response = await api.post(`/suppliers/${supplier.id}/payments`, {
        amount: cents,
        note: note.trim(),
      });
      const updated: Supplier = response.data.data;
      // The row update and toast are dialog-independent; the dialog-only
      // state is applied solely when the same dialog is still open.
      setItems((previous) =>
        previous.map((item) => (item.id === updated.id ? updated : item)),
      );
      setPaymentSupplier((current) =>
        current && current.id === updated.id ? updated : current,
      );
      setReloadKey((value) => value + 1);
      pushToast(
        "success",
        `Payment recorded. ${updated.name}'s balance is now ${formatMoney(
          updated.outstanding_balance,
        )}.`,
      );
      if (paymentDialogRef.current === dialogId) {
        closePayments();
      }
    } catch (error: any) {
      const message =
        error.response?.data?.error || "Could not record the payment. Try again.";
      pushToast("error", message);
      if (paymentDialogRef.current === dialogId) {
        setPaymentError(message);
      }
    } finally {
      if (paymentDialogRef.current === dialogId) {
        setSavingPayment(false);
      }
    }
  }

  const totalPages = Math.max(1, meta.totalPages || 1);
  const start = meta.total === 0 ? 0 : (meta.page - 1) * meta.limit + 1;
  const end = meta.total === 0 ? 0 : Math.min(meta.page * meta.limit, meta.total);
  const selectedOverdue = paymentSupplier
    ? isOverdue(paymentSupplier.due_date, paymentSupplier.outstanding_balance)
    : false;

  function commitPage() {
    const parsed = Number(pageInput);
    if (!Number.isFinite(parsed)) {
      setPageInput(String(page));
      return;
    }
    const next = Math.min(Math.max(1, Math.trunc(parsed)), totalPages);
    setPageInput(String(next));
    setPage(next);
  }

  return (
    <>
      <PageHeader
        eyebrow="Operations"
        title="Suppliers"
        description="Manage vendors, purchase history and outstanding payments."
        action={
          <button
            className="btn-primary"
            onClick={() => setShowCreate((value) => !value)}
            aria-expanded={showCreate}
            aria-controls="add-supplier-form"
            disabled={creating}
          >
            <Plus size={16} /> Add supplier
          </button>
        }
      />

      {showCreate && (
        <form
          id="add-supplier-form"
          onSubmit={createSupplier}
          className="panel mb-6 grid gap-4 p-5 sm:grid-cols-2"
        >
          <label className="text-sm font-semibold sm:col-span-2">
            Supplier name
            <input
              className="field mt-1.5"
              value={name}
              onChange={(event) => setName(event.target.value)}
              placeholder="e.g. Nairobi Hardware Ltd"
              required
              disabled={creating}
            />
          </label>
          <label className="text-sm font-semibold">
            Phone
            <input
              className="field mt-1.5"
              value={phone}
              onChange={(event) => setPhone(event.target.value)}
              placeholder="0700 000 000"
              disabled={creating}
            />
          </label>
          <label className="text-sm font-semibold">
            Email
            <input
              className="field mt-1.5"
              type="email"
              value={email}
              onChange={(event) => setEmail(event.target.value)}
              placeholder="accounts@supplier.co.ke"
              disabled={creating}
            />
          </label>
          <label className="text-sm font-semibold sm:col-span-2">
            Payment terms
            <select
              className="field mt-1.5"
              value={terms}
              onChange={(event) => setTerms(event.target.value)}
              disabled={creating}
            >
              {paymentTermOptions.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
          </label>
          <div className="flex gap-2 sm:col-span-2">
            <button className="btn-primary" disabled={creating}>
              {creating ? "Saving…" : "Save supplier"}
            </button>
            <button
              type="button"
              className="btn-secondary"
              onClick={() => setShowCreate(false)}
              disabled={creating}
            >
              Cancel
            </button>
          </div>
        </form>
      )}

      <div className="panel overflow-hidden">
        <div className="flex flex-col gap-3 border-b border-[#eef1ed] p-4 sm:flex-row sm:items-center">
          <div className="relative min-w-0 flex-1">
            <Search
              size={16}
              className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-[#9aa59d]"
            />
            <input
              className="field pl-9"
              placeholder="Search by name, phone or email"
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              aria-label="Search suppliers"
            />
          </div>
          <span className="shrink-0 text-sm text-[#718078]">
            {loading
              ? "Loading…"
              : loadError
                ? "—"
                : `${meta.total} supplier${meta.total === 1 ? "" : "s"}`}
          </span>
        </div>

        {loading ? (
          <div className="space-y-2 p-4">
            {Array.from({ length: 4 }).map((_, index) => (
              <div key={index} className="h-12 animate-pulse rounded-lg bg-[#f2f5f1]" />
            ))}
          </div>
        ) : loadError ? (
          <div className="p-10 text-center">
            <AlertCircle className="mx-auto text-red-500" />
            <p className="mt-3 font-semibold">Could not load suppliers</p>
            <p className="mt-1 text-sm text-[#718078]">{loadError}</p>
            <button
              className="btn-secondary mt-4"
              onClick={() => setReloadKey((value) => value + 1)}
            >
              Try again
            </button>
          </div>
        ) : items.length === 0 ? (
          <div className="p-10 text-center">
            <Truck className="mx-auto text-[#9aa59d]" />
            <p className="mt-3 font-semibold">
              {search ? "No matches found" : "No suppliers yet"}
            </p>
            <p className="mt-1 text-sm text-[#718078]">
              {search
                ? `No suppliers match "${search}". Try a different name, phone or email.`
                : "Add your first supplier to start receiving purchases."}
            </p>
            {!search && (
              <button className="btn-primary mt-4" onClick={() => setShowCreate(true)}>
                <Plus size={16} /> Add supplier
              </button>
            )}
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[760px] text-left text-sm">
              <thead className="bg-[#f8faf7] text-xs uppercase text-[#718078]">
                <tr>
                  <th className="px-5 py-3">Supplier</th>
                  <th className="px-5 py-3">Contact</th>
                  <th className="px-5 py-3 text-right">Outstanding</th>
                  <th className="px-5 py-3">Due</th>
                  <th className="px-5 py-3 text-right">Actions</th>
                </tr>
              </thead>
              <tbody>
                {items.map((supplier) => {
                  const overdue = isOverdue(
                    supplier.due_date,
                    supplier.outstanding_balance,
                  );
                  return (
                    <tr key={supplier.id} className="border-t border-[#eef1ed]">
                      <td className="px-5 py-4">
                        <p className="font-semibold">{supplier.name}</p>
                        <p className="text-xs text-[#8a958d]">
                          {formatTerms(supplier.payment_terms)}
                        </p>
                      </td>
                      <td className="px-5 py-4 text-[#718078]">
                        {supplier.phone || supplier.email ? (
                          <>
                            <p>{supplier.phone || "—"}</p>
                            {supplier.email && (
                              <p className="text-xs text-[#8a958d]">{supplier.email}</p>
                            )}
                          </>
                        ) : (
                          "No contact"
                        )}
                      </td>
                      <td className="px-5 py-4 text-right font-semibold">
                        {formatMoney(supplier.outstanding_balance)}
                      </td>
                      <td className="px-5 py-4">
                        <span
                          className={
                            overdue
                              ? "text-xs font-semibold text-red-600"
                              : "text-xs text-[#718078]"
                          }
                        >
                          {formatDate(supplier.due_date)}
                          {overdue ? " · Overdue" : ""}
                        </span>
                      </td>
                      <td className="px-5 py-4">
                        <div className="flex justify-end gap-2">
                          <button
                            type="button"
                            className="btn-secondary px-3 py-1.5 text-xs"
                            onClick={() => openPayments(supplier)}
                            aria-label={`Payment history for ${supplier.name}`}
                          >
                            <History size={14} /> History
                          </button>
                          <button
                            type="button"
                            className="btn-primary px-3 py-1.5 text-xs"
                            disabled={supplier.outstanding_balance <= 0}
                            onClick={() => openPayments(supplier)}
                          >
                            <WalletCards size={14} /> Pay
                          </button>
                        </div>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}

        {!loadError && !loading && (
          <div className="flex flex-col gap-3 border-t border-[#eef1ed] p-4 sm:flex-row sm:items-center sm:justify-between">
            <p className="text-sm text-[#718078]">
              {meta.total === 0
                ? "No results"
                : `Showing ${start}–${end} of ${meta.total} results`}
            </p>
            <div className="flex flex-wrap items-center gap-2">
              <label className="flex items-center gap-2 text-xs text-[#718078]">
                Rows
                <select
                  className="field w-auto py-1.5"
                  value={limit}
                  onChange={(event) => {
                    setLimit(Number(event.target.value));
                    setPage(1);
                  }}
                  disabled={loading}
                >
                  {PAGE_SIZES.map((size) => (
                    <option key={size} value={size}>
                      {size}
                    </option>
                  ))}
                </select>
              </label>
              <button
                type="button"
                className="btn-secondary px-3 py-1.5 text-xs"
                onClick={() => setPage((value) => Math.max(1, value - 1))}
                disabled={loading || page <= 1}
                aria-label="Previous page"
              >
                Prev
              </button>
              <label className="flex items-center gap-1 text-xs text-[#718078]">
                Page
                <input
                  className="field w-16 py-1.5 text-center"
                  type="number"
                  min={1}
                  max={totalPages}
                  value={pageInput}
                  onChange={(event) => setPageInput(event.target.value)}
                  onBlur={commitPage}
                  onKeyDown={(event) => {
                    if (event.key === "Enter") {
                      event.preventDefault();
                      commitPage();
                    }
                  }}
                  aria-label="Page number"
                  disabled={loading}
                />
                of {totalPages}
              </label>
              <button
                type="button"
                className="btn-secondary px-3 py-1.5 text-xs"
                onClick={() => setPage((value) => Math.min(totalPages, value + 1))}
                disabled={loading || page >= totalPages}
                aria-label="Next page"
              >
                Next
              </button>
            </div>
          </div>
        )}
      </div>

      {paymentSupplier && (
        <div
          className="fixed inset-0 z-50 flex items-end justify-center bg-black/30 p-0 sm:items-center sm:p-4"
          role="presentation"
          onMouseDown={(event) => {
            if (event.target === event.currentTarget) closePayments();
          }}
        >
          <div
            ref={paymentPanelRef}
            tabIndex={-1}
            role="dialog"
            aria-modal="true"
            aria-labelledby="payment-title"
            className="panel max-h-[92vh] w-full max-w-2xl overflow-y-auto rounded-b-none p-5 sm:rounded-xl"
            onClick={(event) => event.stopPropagation()}
          >
            <div className="flex items-start justify-between gap-4">
              <div>
                <p className="eyebrow">Supplier account</p>
                <h2 id="payment-title" className="mt-1 text-xl font-bold">
                  {paymentSupplier.name}
                </h2>
                <p className="mt-1 text-sm text-[#718078]">
                  {paymentSupplier.phone || paymentSupplier.email || "No contact details"}
                </p>
              </div>
              <button
                ref={closeButtonRef}
                type="button"
                onClick={closePayments}
                className="rounded-lg p-2 text-[#718078] transition hover:bg-[#f2f5f1]"
                aria-label="Close payment dialog"
              >
                <X size={18} />
              </button>
            </div>

            <div className="mt-4 grid gap-3 sm:grid-cols-3">
              <div className="rounded-lg bg-[#f8faf7] p-3">
                <p className="eyebrow">Outstanding</p>
                <p className="mt-1 text-lg font-bold">
                  {formatMoney(paymentSupplier.outstanding_balance)}
                </p>
              </div>
              <div className="rounded-lg bg-[#f8faf7] p-3">
                <p className="eyebrow">Due date</p>
                <p
                  className={`mt-1 text-sm font-semibold ${
                    selectedOverdue ? "text-red-600" : "text-[#17211b]"
                  }`}
                >
                  {formatDate(paymentSupplier.due_date)}
                  {selectedOverdue ? " · Overdue" : ""}
                </p>
              </div>
              <div className="rounded-lg bg-[#f8faf7] p-3">
                <p className="eyebrow">Terms</p>
                <p className="mt-1 text-sm font-semibold">
                  {formatTerms(paymentSupplier.payment_terms)}
                </p>
              </div>
            </div>

            <form onSubmit={submitPayment} className="mt-5 space-y-3">
              <div className="flex items-end gap-2">
                <label className="flex-1 text-sm font-semibold">
                  Amount (KSh)
                  <input
                    ref={amountRef}
                    className="field mt-1.5"
                    type="number"
                    min="0.01"
                    step="0.01"
                    max={(paymentSupplier.outstanding_balance / 100).toFixed(2)}
                    value={amount}
                    onChange={(event) => {
                      setAmount(event.target.value);
                      setPaymentError("");
                    }}
                    placeholder="0.00"
                    disabled={savingPayment || paymentSupplier.outstanding_balance <= 0}
                  />
                </label>
                <button
                  type="button"
                  className="btn-secondary mb-0.5 px-3 py-2 text-xs"
                  onClick={() => {
                    setAmount((paymentSupplier.outstanding_balance / 100).toFixed(2));
                    setPaymentError("");
                  }}
                  disabled={savingPayment || paymentSupplier.outstanding_balance <= 0}
                >
                  Pay full
                </button>
              </div>
              <label className="block text-sm font-semibold">
                Note <span className="font-normal text-[#8a958d]">(optional)</span>
                <input
                  className="field mt-1.5"
                  value={note}
                  onChange={(event) => {
                    setNote(event.target.value);
                    setPaymentError("");
                  }}
                  placeholder="e.g. M-Pesa reference"
                  disabled={savingPayment}
                />
              </label>
              {paymentError && (
                <p className="rounded-lg bg-red-50 p-3 text-sm text-red-700">
                  {paymentError}
                </p>
              )}
              <button
                type="submit"
                className="btn-primary w-full"
                disabled={savingPayment || paymentSupplier.outstanding_balance <= 0}
              >
                {savingPayment ? "Recording payment…" : "Record payment"}
              </button>
              {paymentSupplier.outstanding_balance <= 0 && (
                <p className="text-center text-xs text-[#8a958d]">
                  This supplier has no outstanding balance.
                </p>
              )}
            </form>

            <div className="mt-6 border-t border-[#eef1ed] pt-4">
              <div className="flex items-center justify-between">
                <h3 className="text-sm font-bold">Payment history</h3>
                <span className="text-xs text-[#8a958d]">
                  {payments.length} payment{payments.length === 1 ? "" : "s"}
                </span>
              </div>
              {historyLoading ? (
                <p className="mt-3 text-sm text-[#718078]">Loading payments…</p>
              ) : historyError ? (
                <div className="mt-3 rounded-lg bg-red-50 p-3 text-sm text-red-700">
                  <p>{historyError}</p>
                  <button
                    type="button"
                    onClick={() => paymentSupplier && loadPayments(paymentSupplier)}
                    className="mt-2 text-xs font-semibold underline"
                  >
                    Try again
                  </button>
                </div>
              ) : payments.length === 0 ? (
                <p className="mt-3 text-sm text-[#718078]">No payments recorded yet.</p>
              ) : (
                <ul className="mt-3 divide-y divide-[#eef1ed]">
                  {payments.map((payment) => (
                    <li
                      key={payment.id}
                      className="flex items-start justify-between gap-3 py-3"
                    >
                      <div className="min-w-0">
                        <p className="text-sm font-semibold">
                          {formatMoney(payment.amount)}
                        </p>
                        <p className="break-words text-xs text-[#718078]">
                          {formatDate(payment.created_at)}
                          {payment.note ? ` · ${payment.note}` : ""}
                        </p>
                      </div>
                      <span className="shrink-0 text-xs text-[#8a958d]">
                        {new Date(payment.created_at).toLocaleTimeString("en-KE", {
                          hour: "2-digit",
                          minute: "2-digit",
                        })}
                      </span>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          </div>
        </div>
      )}

      <div className="pointer-events-none fixed inset-x-4 bottom-4 z-[60] flex flex-col items-end gap-2 sm:left-auto sm:right-6 sm:w-96">
        {toasts.map((toast) => (
          <div
            key={toast.id}
            role={toast.tone === "error" ? "alert" : "status"}
            className={`pointer-events-auto flex w-full items-start gap-3 rounded-xl border p-3 shadow-lg ${
              toast.tone === "success"
                ? "border-[#b7e4c7] bg-[#f0faf4] text-[#12633e]"
                : "border-[#f1c7c7] bg-[#fff5f5] text-[#9b1c1c]"
            }`}
          >
            {toast.tone === "success" ? (
              <CheckCircle2 size={18} className="mt-0.5 shrink-0" />
            ) : (
              <AlertCircle size={18} className="mt-0.5 shrink-0" />
            )}
            <p className="flex-1 text-sm">{toast.message}</p>
            <button
              type="button"
              onClick={() => dismissToast(toast.id)}
              className="shrink-0 rounded p-0.5 transition hover:bg-black/5"
              aria-label="Dismiss notification"
            >
              <X size={14} />
            </button>
          </div>
        ))}
      </div>
    </>
  );
}
