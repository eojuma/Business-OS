"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import {
  ClipboardList,
  Loader2,
  PackageCheck,
  Pencil,
  Plus,
  Trash2,
  X,
} from "lucide-react";
import { api } from "@/lib/api";
import { PageHeader, formatMoney } from "@/components/app-shell";

type Product = { id: string; name: string; unit: string };
type Supplier = { id: string; name: string; deleted_at?: string | null };
type LineItem = {
  id?: string;
  product_id: string;
  quantity: number;
  unit_cost: number;
  subtotal: number;
  received_qty: number;
  note?: string;
};
type Purchase = {
  id: string;
  po_number: string;
  supplier_id: string;
  status: string;
  total_amount: number;
  amount_paid: number;
  note?: string;
  expected_at?: string | null;
  due_date?: string | null;
  created_at: string;
  received_at?: string | null;
  items: LineItem[];
};
type Meta = { page: number; limit: number; total: number; totalPages: number };
type FormRow = {
  product_id: string;
  quantity: string;
  unit_cost: string;
  note: string;
};

const emptyRow: FormRow = { product_id: "", quantity: "", unit_cost: "", note: "" };

function toCents(value: string): number {
  return Math.round(Number(value || 0) * 100);
}
function fromCents(value: number): string {
  const amount = Number(value || 0) / 100;
  return Number.isInteger(amount) ? String(amount) : amount.toFixed(2);
}
function toDateInput(value?: string | null): string {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value.slice(0, 10);
  // Date-only values are stored at UTC midnight, so keep the calendar date.
  // Legacy local-midnight instants fall back to the viewer's calendar date.
  if (
    date.getUTCHours() === 0 &&
    date.getUTCMinutes() === 0 &&
    date.getUTCSeconds() === 0 &&
    date.getUTCMilliseconds() === 0
  ) {
    return value.slice(0, 10);
  }
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}
function toISO(value: string): string | null {
  return value ? new Date(`${value}T00:00:00`).toISOString() : null;
}
function toCalendarDate(value: string): string | null {
  return value || null;
}
// The date inputs represent the user's local calendar day. Convert them to the
// matching UTC instants so the API range covers the whole local day.
function dayEndISO(value: string): string | null {
  if (!value) return null;
  const end = new Date(`${value}T00:00:00`);
  end.setDate(end.getDate() + 1);
  end.setMilliseconds(-1);
  // Keep the final microsecond: the API filters with created_at <= date_to and
  // the database stores microsecond-precision timestamps.
  return end.toISOString().replace(/\.\d{3}Z$/, ".999999Z");
}
function remaining(item: LineItem): number {
  return Math.max(0, item.quantity - (item.received_qty || 0));
}
function errMessage(error: unknown, fallback: string): string {
  const e = error as { response?: { data?: { error?: string } } };
  return e.response?.data?.error || fallback;
}

// useDialogFocus moves focus into an open modal, keeps Tab inside it, closes on
// Escape, and restores focus to the opener when the modal unmounts.
function useDialogFocus(open: boolean, onClose: () => void) {
  const dialogRef = useRef<HTMLDivElement>(null);
  const onCloseRef = useRef(onClose);
  onCloseRef.current = onClose;

  useEffect(() => {
    if (!open) return;
    const dialog = dialogRef.current;
    if (!dialog) return;
    const previous = document.activeElement as HTMLElement | null;

    const focusable = () =>
      Array.from(
        dialog.querySelectorAll<HTMLElement>(
          'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])',
        ),
      ).filter((element) => element.offsetParent !== null);

    const fields = Array.from(
      dialog.querySelectorAll<HTMLElement>("input, select, textarea"),
    ).filter((element) => !element.hasAttribute("disabled"));
    (fields[0] || focusable()[0])?.focus();

    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") {
        event.preventDefault();
        onCloseRef.current();
        return;
      }
      if (event.key !== "Tab") return;
      const elements = focusable();
      if (elements.length === 0) {
        event.preventDefault();
        return;
      }
      const first = elements[0];
      const last = elements[elements.length - 1];
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    }

    dialog.addEventListener("keydown", handleKeyDown);
    return () => {
      dialog.removeEventListener("keydown", handleKeyDown);
      previous?.focus();
    };
  }, [open]);

  return dialogRef;
}

export default function PurchasesPage() {
  const [products, setProducts] = useState<Product[]>([]);
  const [suppliers, setSuppliers] = useState<Supplier[]>([]);
  const [purchases, setPurchases] = useState<Purchase[]>([]);
  const [meta, setMeta] = useState<Meta | null>(null);

  // Filters & pagination
  const [statusFilter, setStatusFilter] = useState("");
  const [supplierFilter, setSupplierFilter] = useState("");
  const [dateFrom, setDateFrom] = useState("");
  const [dateTo, setDateTo] = useState("");
  const [page, setPage] = useState(1);
  const [limit, setLimit] = useState(20);

  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [dialogError, setDialogError] = useState("");

  // Create / edit form state
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<Purchase | null>(null);
  const [formSupplier, setFormSupplier] = useState("");
  const [formPaid, setFormPaid] = useState("");
  const [formNote, setFormNote] = useState("");
  const [formExpected, setFormExpected] = useState("");
  const [formRows, setFormRows] = useState<FormRow[]>([{ ...emptyRow }]);
  const [saving, setSaving] = useState(false);

  // Partial receive state
  const [receiving, setReceiving] = useState<Purchase | null>(null);
  const [receiveValues, setReceiveValues] = useState<Record<number, string>>({});
  const [receivingBusy, setReceivingBusy] = useState(false);

  const formDialogRef = useDialogFocus(formOpen, () => setFormOpen(false));
  const receiveDialogRef = useDialogFocus(receiving !== null, () => setReceiving(null));

  // dialogSession invalidates completions from a dialog that has since been
  // closed or replaced, so a stale response cannot close the new dialog.
  const dialogSession = useRef(0);
  const latestRequest = useRef(0);
  const loadPurchases = useCallback(async () => {
    const requestId = ++latestRequest.current;
    setLoading(true);
    setError("");
    try {
      const params: Record<string, string | number> = { page, limit };
      if (statusFilter) params.status = statusFilter;
      if (supplierFilter) params.supplier_id = supplierFilter;
      if (dateFrom) {
        const from = toISO(dateFrom);
        if (from) params.date_from = from;
      }
      if (dateTo) {
        const to = dayEndISO(dateTo);
        if (to) params.date_to = to;
      }
      const res = await api.get("/purchases", { params });
      if (requestId !== latestRequest.current) return;
      const payload = res.data?.data;
      const rows: Purchase[] = Array.isArray(payload) ? payload : payload?.data || [];
      setPurchases(rows);
      setMeta(Array.isArray(payload) ? res.data?.meta || null : payload?.meta || null);
    } catch (e) {
      if (requestId !== latestRequest.current) return;
      setPurchases([]);
      setMeta(null);
      setError(errMessage(e, "Failed to load purchases"));
    } finally {
      if (requestId === latestRequest.current) setLoading(false);
    }
  }, [page, limit, statusFilter, supplierFilter, dateFrom, dateTo]);

  useEffect(() => {
    void loadPurchases();
  }, [loadPurchases]);

  useEffect(() => {
    let active = true;
    (async () => {
      try {
        const [p, s] = await Promise.all([
          api.get("/products"),
          // Include archived suppliers so their purchases can still be filtered
          // and labelled; the form picker below restricts to active suppliers.
          api.get("/suppliers", { params: { include_archived: true } }),
        ]);
        if (!active) return;
        setProducts(p.data.data || []);
        setSuppliers(s.data.data || []);
      } catch (e) {
        if (active) setError(errMessage(e, "Failed to load products and suppliers"));
      }
    })();
    return () => {
      active = false;
    };
  }, []);

  const formTotalCents = formRows.reduce(
    (sum, row) => sum + Number(row.quantity || 0) * toCents(row.unit_cost),
    0,
  );

  function resetFilters() {
    setStatusFilter("");
    setSupplierFilter("");
    setDateFrom("");
    setDateTo("");
    setPage(1);
  }

  function openCreate() {
    dialogSession.current += 1;
    setSaving(false);
    setDialogError("");
    setEditing(null);
    setFormSupplier("");
    setFormPaid("");
    setFormNote("");
    setFormExpected("");
    setFormRows([{ ...emptyRow }]);
    setFormOpen(true);
  }

  function openEdit(purchase: Purchase) {
    dialogSession.current += 1;
    setSaving(false);
    setDialogError("");
    setEditing(purchase);
    setFormSupplier(purchase.supplier_id);
    setFormPaid(fromCents(purchase.amount_paid));
    setFormNote(purchase.note || "");
    setFormExpected(toDateInput(purchase.expected_at));
    setFormRows(
      purchase.items.map((item) => ({
        product_id: item.product_id,
        quantity: String(item.quantity),
        unit_cost: fromCents(item.unit_cost),
        note: item.note || "",
      })),
    );
    setFormOpen(true);
  }

  function updateRow(index: number, key: keyof FormRow, value: string) {
    setFormRows((rows) => rows.map((row, i) => (i === index ? { ...row, [key]: value } : row)));
  }

  function removeRow(index: number) {
    setFormRows((rows) => (rows.length === 1 ? rows : rows.filter((_, i) => i !== index)));
  }

  async function submitForm(event: React.FormEvent) {
    event.preventDefault();
    const session = dialogSession.current;
    setSaving(true);
    setError("");
    setDialogError("");
    const body = {
      supplier_id: formSupplier,
      amount_paid: toCents(formPaid),
      note: formNote,
      expected_at: toCalendarDate(formExpected),
      items: formRows.map((row) => ({
        product_id: row.product_id,
        quantity: Number(row.quantity || 0),
        unit_cost: toCents(row.unit_cost),
        note: row.note,
      })),
    };
    try {
      if (editing) {
        await api.patch(`/purchases/${editing.id}`, body);
      } else {
        await api.post("/purchases", body);
      }
      if (dialogSession.current !== session) return;
      setFormOpen(false);
      if (!editing && page !== 1) {
        // New purchases are newest-first, so show page one where the row lands.
        setPage(1);
      } else {
        await loadPurchases();
      }
    } catch (e) {
      if (dialogSession.current !== session) return;
      setDialogError(errMessage(e, editing ? "Failed to update purchase" : "Failed to create purchase"));
    } finally {
      if (dialogSession.current === session) setSaving(false);
    }
  }

  function openReceive(purchase: Purchase) {
    dialogSession.current += 1;
    setReceivingBusy(false);
    setDialogError("");
    const values: Record<number, string> = {};
    purchase.items.forEach((item, index) => {
      values[index] = String(remaining(item));
    });
    setReceiveValues(values);
    setReceiving(purchase);
  }

  async function submitReceive(event: React.FormEvent) {
    event.preventDefault();
    if (!receiving) return;
    const session = dialogSession.current;
    const items = Object.entries(receiveValues)
      .map(([key, value]) => ({
        item: receiving.items[Number(key)],
        quantity: Number(value || 0),
      }))
      .filter(
        (entry): entry is { item: LineItem; quantity: number } =>
          Boolean(entry.item) && entry.quantity > 0,
      )
      .map(({ item, quantity }) => ({
        item_id: item.id,
        product_id: item.product_id,
        received_qty: quantity,
      }));
    if (items.length === 0) {
      setDialogError("Enter at least one quantity to receive");
      return;
    }
    setReceivingBusy(true);
    setError("");
    setDialogError("");
    try {
      await api.post(`/purchases/${receiving.id}/receive`, { items });
      if (dialogSession.current !== session) return;
      setReceiving(null);
      await loadPurchases();
    } catch (e) {
      if (dialogSession.current !== session) return;
      setDialogError(errMessage(e, "Failed to receive purchase"));
    } finally {
      if (dialogSession.current === session) setReceivingBusy(false);
    }
  }

  async function removePurchase(purchase: Purchase) {
    if (!window.confirm(`Delete ${purchase.po_number}? This cannot be undone.`)) return;
    setError("");
    try {
      await api.delete(`/purchases/${purchase.id}`);
      if (purchases.length === 1 && page > 1) {
        // The page is now empty; stepping back triggers the page-change effect
        // to reload the previous page.
        setPage(page - 1);
      } else {
        await loadPurchases();
      }
    } catch (e) {
      setError(errMessage(e, "Failed to delete purchase"));
    }
  }

  function canEdit(purchase: Purchase): boolean {
    return purchase.status === "draft" && purchase.items.every((item) => (item.received_qty || 0) === 0);
  }

  function statusBadge(purchase: Purchase) {
    if (purchase.status === "received") {
      return <span className="rounded-full bg-primary-soft px-2 py-0.5 text-xs font-bold uppercase text-primary">Received</span>;
    }
    const partiallyReceived = purchase.items.some((item) => (item.received_qty || 0) > 0);
    if (partiallyReceived) {
      return <span className="rounded-full bg-warning-soft px-2 py-0.5 text-xs font-bold uppercase text-warning-strong">Partial</span>;
    }
    return <span className="rounded-full bg-surface-muted px-2 py-0.5 text-xs font-bold uppercase text-muted">Draft</span>;
  }

  function receivedProgress(purchase: Purchase): string {
    const ordered = purchase.items.reduce((sum, item) => sum + item.quantity, 0);
    const arrived = purchase.items.reduce((sum, item) => sum + (item.received_qty || 0), 0);
    return `${arrived} of ${ordered} received`;
  }

  const showPagination = meta && meta.total > 0;
  const rangeStart = meta && meta.total > 0 ? (meta.page - 1) * meta.limit + 1 : 0;
  const rangeEnd = meta ? Math.min(meta.page * meta.limit, meta.total) : 0;

  return (
    <>
      <PageHeader
        eyebrow="Procurement"
        title="Purchases"
        description="Create purchase orders, edit drafts, and receive goods in as many shipments as the supplier sends."
        action={
          <button className="btn-primary" onClick={openCreate}>
            <Plus size={16} /> New purchase
          </button>
        }
      />

      {error && !formOpen && !receiving && (
        <p className="mb-4 flex items-start justify-between gap-3 rounded-lg bg-danger-soft p-3 text-sm text-danger">
          <span>{error}</span>
          <button type="button" aria-label="Dismiss error" onClick={() => setError("")}>
            <X size={15} />
          </button>
        </p>
      )}

      {/* Filters */}
      <div className="panel mb-5 grid gap-3 p-4 sm:grid-cols-2 lg:grid-cols-6">
        <label className="text-xs font-semibold uppercase tracking-wide text-muted">
          Status
          <select
            className="field mt-1.5"
            value={statusFilter}
            onChange={(e) => {
              setStatusFilter(e.target.value);
              setPage(1);
            }}
          >
            <option value="">All</option>
            <option value="draft">Draft</option>
            <option value="received">Received</option>
          </select>
        </label>
        <label className="text-xs font-semibold uppercase tracking-wide text-muted">
          Supplier
          <select
            className="field mt-1.5"
            value={supplierFilter}
            onChange={(e) => {
              setSupplierFilter(e.target.value);
              setPage(1);
            }}
          >
            <option value="">All suppliers</option>
            {suppliers.map((supplier) => (
              <option key={supplier.id} value={supplier.id}>
                {supplier.name}
                {supplier.deleted_at ? " (archived)" : ""}
              </option>
            ))}
          </select>
        </label>
        <label className="text-xs font-semibold uppercase tracking-wide text-muted">
          From
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
        <label className="text-xs font-semibold uppercase tracking-wide text-muted">
          To
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
        <label className="text-xs font-semibold uppercase tracking-wide text-muted">
          Page size
          <select
            className="field mt-1.5"
            value={limit}
            onChange={(e) => {
              setLimit(Number(e.target.value));
              setPage(1);
            }}
          >
            <option value={10}>10</option>
            <option value={20}>20</option>
            <option value={50}>50</option>
            <option value={100}>100</option>
          </select>
        </label>
        <div className="flex items-end">
          <button type="button" className="btn-secondary w-full" onClick={resetFilters}>
            Reset filters
          </button>
        </div>
      </div>

      {/* Table */}
      <div className="panel overflow-hidden">
        {loading ? (
          <div className="flex items-center justify-center gap-2 p-10 text-sm text-muted">
            <Loader2 size={18} className="animate-spin" /> Loading purchases…
          </div>
        ) : purchases.length === 0 ? (
          <div className="p-10 text-center">
            <ClipboardList className="mx-auto text-subtle" />
            <p className="mt-3 font-semibold">No purchases match these filters</p>
            <p className="mt-1 text-sm text-muted">Try widening the date range or clearing the filters.</p>
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm">
              <thead className="bg-surface-muted text-xs uppercase text-muted">
                <tr>
                  <th className="px-5 py-3">PO number</th>
                  <th className="px-5 py-3">Created</th>
                  <th className="px-5 py-3">Supplier</th>
                  <th className="px-5 py-3">Expected</th>
                  <th className="px-5 py-3">Items</th>
                  <th className="px-5 py-3 text-right">Total</th>
                  <th className="px-5 py-3 text-right">Status</th>
                  <th className="px-5 py-3 text-right">Actions</th>
                </tr>
              </thead>
              <tbody>
                {purchases.map((purchase) => (
                  <tr key={purchase.id} className="border-t border-border-subtle align-top">
                    <td className="px-5 py-4 font-mono text-xs font-semibold">{purchase.po_number}</td>
                    <td className="px-5 py-4 text-muted">
                      {new Date(purchase.created_at).toLocaleDateString("en-KE")}
                    </td>
                    <td className="px-5 py-4 font-semibold">
                      {suppliers.find((s) => s.id === purchase.supplier_id)?.name || "Supplier"}
                    </td>
                    <td className="px-5 py-4 text-muted">
                      {purchase.expected_at
                        ? purchase.expected_at.slice(0, 10)
                        : "—"}
                    </td>
                    <td className="px-5 py-4 text-muted">
                      {purchase.items?.length || 0} line{purchase.items?.length === 1 ? "" : "s"}
                    </td>
                    <td className="px-5 py-4 text-right font-semibold">{formatMoney(purchase.total_amount)}</td>
                    <td className="px-5 py-4 text-right">
                      <div className="flex flex-col items-end gap-1">
                        {statusBadge(purchase)}
                        {purchase.status !== "received" && (
                          <span className="text-[11px] text-subtle">{receivedProgress(purchase)}</span>
                        )}
                      </div>
                    </td>
                    <td className="px-5 py-4">
                      <div className="flex flex-wrap items-center justify-end gap-2">
                        {canEdit(purchase) && (
                          <button
                            type="button"
                            className="btn-secondary py-1.5"
                            onClick={() => openEdit(purchase)}
                          >
                            <Pencil size={14} /> Edit
                          </button>
                        )}
                        {purchase.status !== "received" &&
                          purchase.items.some((item) => remaining(item) > 0) && (
                            <button
                              type="button"
                              className="btn-secondary py-1.5"
                              onClick={() => openReceive(purchase)}
                            >
                              <PackageCheck size={14} /> Receive
                            </button>
                          )}
                        {canEdit(purchase) && (
                          <button
                            type="button"
                            aria-label={`Delete ${purchase.po_number}`}
                            className="rounded-lg p-2 text-danger transition hover:bg-danger-soft"
                            onClick={() => removePurchase(purchase)}
                          >
                            <Trash2 size={15} />
                          </button>
                        )}
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {/* Pagination */}
      {showPagination && meta && (
        <div className="mt-4 flex flex-col gap-3 text-sm text-muted sm:flex-row sm:items-center sm:justify-between">
          <p>
            Showing {rangeStart}–{rangeEnd} of {meta.total} purchase{meta.total === 1 ? "" : "s"}
          </p>
          <div className="flex items-center gap-2">
            <button
              type="button"
              className="btn-secondary py-1.5"
              disabled={meta.page <= 1}
              onClick={() => setPage((p) => Math.max(1, p - 1))}
            >
              Previous
            </button>
            <span className="px-1">
              Page {meta.page} of {Math.max(1, meta.totalPages)}
            </span>
            <button
              type="button"
              className="btn-secondary py-1.5"
              disabled={meta.page >= meta.totalPages}
              onClick={() => setPage((p) => p + 1)}
            >
              Next
            </button>
          </div>
        </div>
      )}

      {/* Create / edit modal */}
      {formOpen && (
        <div
          ref={formDialogRef}
          className="fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-black/40 p-4 sm:items-center"
          role="dialog"
          aria-modal="true"
          aria-label={editing ? `Edit ${editing.po_number}` : "New purchase"}
        >
          <form onSubmit={submitForm} className="panel my-6 w-full max-w-3xl space-y-4 p-5">
            <div className="flex items-start justify-between">
              <div>
                <p className="eyebrow mb-1">{editing ? "Edit draft" : "New purchase"}</p>
                <h2 className="text-xl font-bold">{editing ? editing.po_number : "Create a purchase order"}</h2>
              </div>
              <button type="button" aria-label="Close" onClick={() => setFormOpen(false)} className="rounded-lg p-2 text-muted hover:bg-hover">
                <X size={18} />
              </button>
            </div>

            {dialogError && (
              <p role="alert" className="rounded-lg bg-danger-soft p-3 text-sm text-danger">
                {dialogError}
              </p>
            )}

            <div className="grid gap-3 sm:grid-cols-2">
              <label className="text-sm font-semibold">
                Supplier
                <select className="field mt-1.5" value={formSupplier} onChange={(e) => setFormSupplier(e.target.value)} required>
                  <option value="">Select supplier</option>
                  {suppliers
                    .filter((supplier) => !supplier.deleted_at)
                    .map((supplier) => (
                      <option key={supplier.id} value={supplier.id}>
                        {supplier.name}
                      </option>
                    ))}
                </select>
              </label>
              <label className="text-sm font-semibold">
                Expected delivery
                <input type="date" className="field mt-1.5" value={formExpected} onChange={(e) => setFormExpected(e.target.value)} />
              </label>
              <label className="text-sm font-semibold">
                Amount paid (KSh)
                <input type="number" min="0" step="0.01" className="field mt-1.5" placeholder="0.00" value={formPaid} onChange={(e) => setFormPaid(e.target.value)} />
              </label>
              <label className="text-sm font-semibold">
                Note
                <input className="field mt-1.5" placeholder="Optional note" value={formNote} onChange={(e) => setFormNote(e.target.value)} />
              </label>
            </div>

            <div className="space-y-2">
              <p className="text-sm font-semibold">Line items</p>
              {formRows.map((row, index) => (
                <div key={index} className="grid gap-2 sm:grid-cols-[1fr_90px_120px_1fr_auto]">
                  <select className="field" value={row.product_id} onChange={(e) => updateRow(index, "product_id", e.target.value)} required>
                    <option value="">Select product</option>
                    {products.map((product) => (
                      <option key={product.id} value={product.id}>
                        {product.name} / {product.unit}
                      </option>
                    ))}
                  </select>
                  <input className="field" type="number" min="1" placeholder="Qty" value={row.quantity} onChange={(e) => updateRow(index, "quantity", e.target.value)} required />
                  <input className="field" type="number" min="0" step="0.01" placeholder="Cost / unit" value={row.unit_cost} onChange={(e) => updateRow(index, "unit_cost", e.target.value)} required />
                  <input className="field" placeholder="Line note" value={row.note} onChange={(e) => updateRow(index, "note", e.target.value)} />
                  <button type="button" aria-label="Remove line" className="rounded-lg p-2 text-danger hover:bg-danger-soft" onClick={() => removeRow(index)}>
                    <Trash2 size={15} />
                  </button>
                </div>
              ))}
              <button type="button" className="btn-secondary" onClick={() => setFormRows([...formRows, { ...emptyRow }])}>
                <Plus size={15} /> Add line
              </button>
            </div>

            <div className="flex flex-col gap-3 border-t border-border-subtle pt-4 sm:flex-row sm:items-center sm:justify-between">
              <p className="text-sm text-muted">
                Purchase total <strong className="ml-2 text-foreground">{formatMoney(formTotalCents)}</strong>
              </p>
              <div className="flex gap-2">
                <button type="button" className="btn-secondary" onClick={() => setFormOpen(false)}>
                  Cancel
                </button>
                <button className="btn-primary" disabled={saving}>
                  {saving && <Loader2 size={15} className="animate-spin" />}
                  {editing ? "Save changes" : "Save purchase"}
                </button>
              </div>
            </div>
          </form>
        </div>
      )}

      {/* Partial receive modal */}
      {receiving && (
        <div
          ref={receiveDialogRef}
          className="fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-black/40 p-4 sm:items-center"
          role="dialog"
          aria-modal="true"
          aria-label={`Receive ${receiving.po_number}`}
        >
          <form onSubmit={submitReceive} className="panel my-6 w-full max-w-2xl space-y-4 p-5">
            <div className="flex items-start justify-between">
              <div>
                <p className="eyebrow mb-1">Receive goods</p>
                <h2 className="text-xl font-bold">{receiving.po_number}</h2>
                <p className="mt-1 text-sm text-muted">
                  Enter the quantity that arrived in this shipment. Everything is pre-filled with the remaining
                  quantity; reduce any line that is short.
                </p>
              </div>
              <button type="button" aria-label="Close" onClick={() => setReceiving(null)} className="rounded-lg p-2 text-muted hover:bg-hover">
                <X size={18} />
              </button>
            </div>

            {dialogError && (
              <p role="alert" className="rounded-lg bg-danger-soft p-3 text-sm text-danger">
                {dialogError}
              </p>
            )}

            <div className="space-y-2">
              {receiving.items.map((item, index) => {
                const product = products.find((p) => p.id === item.product_id);
                const outstanding = remaining(item);
                return (
                  <div key={item.id || index} className="grid gap-2 rounded-lg border border-border-subtle p-3 sm:grid-cols-[1fr_auto_auto]">
                    <div>
                      <p className="font-semibold">{product?.name || "Product"}</p>
                      <p className="text-xs text-muted">
                        Ordered {item.quantity} · already received {item.received_qty || 0} · remaining {outstanding}
                      </p>
                    </div>
                    <div className="flex items-center gap-2">
                      <label className="text-xs font-semibold text-muted" htmlFor={`receive-${index}`}>
                        Receive now
                      </label>
                      <input
                        id={`receive-${index}`}
                        className="field w-24"
                        type="number"
                        min="0"
                        max={outstanding}
                        step="1"
                        disabled={outstanding === 0}
                        value={receiveValues[index] ?? ""}
                        onChange={(e) => setReceiveValues((values) => ({ ...values, [index]: e.target.value }))}
                      />
                    </div>
                    <div className="flex items-center justify-end">
                      {outstanding === 0 ? (
                        <span className="text-xs font-bold uppercase text-primary">Complete</span>
                      ) : (
                        <span className="text-xs font-bold uppercase text-warning-strong">Pending</span>
                      )}
                    </div>
                  </div>
                );
              })}
            </div>

            <div className="flex flex-col gap-3 border-t border-border-subtle pt-4 sm:flex-row sm:items-center sm:justify-between">
              <p className="text-xs text-muted">
                The purchase is marked received only when every line is complete.
              </p>
              <div className="flex gap-2">
                <button type="button" className="btn-secondary" onClick={() => setReceiving(null)}>
                  Cancel
                </button>
                <button className="btn-primary" disabled={receivingBusy}>
                  {receivingBusy && <Loader2 size={15} className="animate-spin" />}
                  Receive shipment
                </button>
              </div>
            </div>
          </form>
        </div>
      )}
    </>
  );
}
