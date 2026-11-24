"use client";

import { useCallback, useEffect, useState } from "react";
import { Package, Plus, Truck, CheckCircle2, XCircle, Clock, FileText } from "lucide-react";
import { api } from "@/lib/api";
import { PageHeader } from "@/components/app-shell";
import { usePermissions } from "@/hooks/usePermissions";
import { PERMISSIONS } from "@/lib/permissions";

type DeliveryItem = { id?: string; product_id: string; quantity: number };
type Delivery = {
  id: string;
  source_type: string;
  source_id: string;
  status: string;
  tracking_number: string;
  carrier: string;
  notes: string;
  failure_reason: string;
  dispatched_at?: string | null;
  in_transit_at?: string | null;
  delivered_at?: string | null;
  failed_at?: string | null;
  created_at: string;
  items: DeliveryItem[];
};
type History = { id: string; from_status: string; to_status: string; note: string; created_at: string };

const STATUSES = ["pending", "dispatched", "in_transit", "delivered", "failed"];

const STATUS_LABEL: Record<string, string> = {
  pending: "Pending",
  dispatched: "Dispatched",
  in_transit: "In Transit",
  delivered: "Delivered",
  failed: "Failed",
};

export default function DeliveriesPage() {
  const { can } = usePermissions();
  const canRead = can(PERMISSIONS.deliveriesRead);
  const canCreate = can(PERMISSIONS.deliveriesCreate);
  const canUpdate = can(PERMISSIONS.deliveriesUpdate);
  const canDelete = can(PERMISSIONS.deliveriesDelete);

  const [items, setItems] = useState<Delivery[]>([]);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [loading, setLoading] = useState(true);

  const [showCreate, setShowCreate] = useState(false);
  const [sourceType, setSourceType] = useState("sale");
  const [sourceId, setSourceId] = useState("");
  const [tracking, setTracking] = useState("");
  const [carrier, setCarrier] = useState("");
  const [notes, setNotes] = useState("");
  const [itemRows, setItemRows] = useState([{ product_id: "", quantity: "1" }]);

  const [transitionFor, setTransitionFor] = useState<Delivery | null>(null);
  const [newStatus, setNewStatus] = useState("dispatched");
  const [transitionNote, setTransitionNote] = useState("");

  const [historyFor, setHistoryFor] = useState<Delivery | null>(null);
  const [history, setHistory] = useState<History[]>([]);

  const [noteFor, setNoteFor] = useState<Delivery | null>(null);
  const [noteData, setNoteData] = useState<any>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const res = await api.get("/deliveries");
      setItems(res.data.data?.items || res.data.data || []);
    } catch (e: any) {
      setError(e.response?.data?.error || "Failed to load deliveries");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (canRead) void load();
  }, [canRead, load]);

  async function submitCreate(e: React.FormEvent) {
    e.preventDefault();
    try {
      await api.post("/deliveries", {
        source_type: sourceType,
        source_id: sourceId,
        tracking_number: tracking,
        carrier: carrier,
        notes: notes,
        items: itemRows.map((r) => ({ product_id: r.product_id, quantity: Number(r.quantity) })),
      });
      setShowCreate(false);
      setSourceId("");
      setTracking("");
      setCarrier("");
      setNotes("");
      setItemRows([{ product_id: "", quantity: "1" }]);
      setMessage("Delivery created.");
      await load();
    } catch (e: any) {
      setError(e.response?.data?.error || "Failed to create delivery");
    }
  }

  async function submitTransition(e: React.FormEvent) {
    e.preventDefault();
    if (!transitionFor) return;
    try {
      await api.post(`/deliveries/${transitionFor.id}/transition`, {
        status: newStatus,
        note: transitionNote,
      });
      setTransitionFor(null);
      setTransitionNote("");
      setMessage("Status updated.");
      await load();
    } catch (e: any) {
      setError(e.response?.data?.error || "Failed to update status");
    }
  }

  async function openHistory(d: Delivery) {
    setHistoryFor(d);
    try {
      const res = await api.get(`/deliveries/${d.id}/history`);
      setHistory(res.data.data || []);
    } catch (e: any) {
      setError(e.response?.data?.error || "Failed to load history");
    }
  }

  async function openNote(d: Delivery) {
    setNoteFor(d);
    try {
      const res = await api.get(`/deliveries/${d.id}/note`);
      setNoteData(res.data.data);
    } catch (e: any) {
      setError(e.response?.data?.error || "Failed to load note");
    }
  }

  async function remove(d: Delivery) {
    if (!confirm("Delete this delivery?")) return;
    try {
      await api.delete(`/deliveries/${d.id}`);
      setMessage("Delivery deleted.");
      await load();
    } catch (e: any) {
      setError(e.response?.data?.error || "Failed to delete");
    }
  }

  if (!canRead) {
    return <div className="panel p-6 text-sm text-muted">Your role does not have access to deliveries.</div>;
  }

  return (
    <>
      <PageHeader
        eyebrow="Logistics"
        title="Deliveries"
        description="Track goods from dispatch to delivery."
        action={
          canCreate ? (
            <button className="btn-primary" onClick={() => setShowCreate(true)}>
              <Plus size={16} /> New Delivery
            </button>
          ) : undefined
        }
      />
      {error && <p className="mb-4 rounded-lg bg-danger-soft p-3 text-sm text-danger">{error}</p>}
      {message && <p className="mb-4 rounded-lg bg-primary-soft p-3 text-sm text-primary">{message}</p>}

      {loading ? (
        <p className="p-6 text-sm text-muted">Loading...</p>
      ) : (
        <div className="panel overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border-subtle text-left text-xs text-muted">
                <th className="p-3">Source</th>
                <th className="p-3">Status</th>
                <th className="p-3">Tracking</th>
                <th className="p-3">Carrier</th>
                <th className="p-3">Items</th>
                <th className="p-3">Created</th>
                <th className="p-3">Actions</th>
              </tr>
            </thead>
            <tbody>
              {items.map((d) => (
                <tr key={d.id} className="border-b border-border-subtle">
                  <td className="p-3">{d.source_type} {d.source_id.slice(0, 8)}</td>
                  <td className="p-3">
                    <span className="rounded-full bg-muted-soft px-2 py-0.5 text-xs font-semibold">
                      {STATUS_LABEL[d.status] || d.status}
                    </span>
                  </td>
                  <td className="p-3">{d.tracking_number || "—"}</td>
                  <td className="p-3">{d.carrier || "—"}</td>
                  <td className="p-3">{d.items?.length || 0}</td>
                  <td className="p-3">{new Date(d.created_at).toLocaleDateString()}</td>
                  <td className="p-3 flex gap-2">
                    {canUpdate && d.status !== "delivered" && (
                      <button className="btn-secondary text-xs" onClick={() => { setTransitionFor(d); setNewStatus("dispatched"); }}>
                        Update
                      </button>
                    )}
                    <button className="btn-secondary text-xs" onClick={() => openHistory(d)}>
                      <Clock size={12} />
                    </button>
                    <button className="btn-secondary text-xs" onClick={() => openNote(d)}>
                      <FileText size={12} />
                    </button>
                    {canDelete && (
                      <button className="btn-secondary text-xs" onClick={() => remove(d)}>
                        <XCircle size={12} />
                      </button>
                    )}
                  </td>
                </tr>
              ))}
              {items.length === 0 && (
                <tr>
                  <td colSpan={7} className="p-6 text-center text-muted">No deliveries yet.</td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      )}

      {showCreate && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4">
          <div className="panel w-full max-w-lg p-6">
            <h3 className="mb-4 text-lg font-semibold">New Delivery</h3>
            <form onSubmit={submitCreate} className="space-y-3">
              <div>
                <label className="block text-xs font-semibold text-muted">Source Type</label>
                <select className="field mt-1" value={sourceType} onChange={(e) => setSourceType(e.target.value)}>
                  <option value="sale">Sale</option>
                  <option value="purchase">Purchase</option>
                </select>
              </div>
              <div>
                <label className="block text-xs font-semibold text-muted">Source ID</label>
                <input className="field mt-1 font-mono" value={sourceId} onChange={(e) => setSourceId(e.target.value)} required />
              </div>
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="block text-xs font-semibold text-muted">Tracking #</label>
                  <input className="field mt-1" value={tracking} onChange={(e) => setTracking(e.target.value)} />
                </div>
                <div>
                  <label className="block text-xs font-semibold text-muted">Carrier</label>
                  <input className="field mt-1" value={carrier} onChange={(e) => setCarrier(e.target.value)} />
                </div>
              </div>
              <div>
                <label className="block text-xs font-semibold text-muted">Notes</label>
                <input className="field mt-1" value={notes} onChange={(e) => setNotes(e.target.value)} />
              </div>
              <div>
                <label className="block text-xs font-semibold text-muted">Items</label>
                {itemRows.map((row, i) => (
                  <div key={i} className="mt-1 flex gap-2">
                    <input className="field flex-1 font-mono" placeholder="product_id" value={row.product_id} onChange={(e) => {
                      const next = [...itemRows]; next[i] = { ...next[i], product_id: e.target.value }; setItemRows(next);
                    }} required />
                    <input className="field w-20" type="number" min="1" value={row.quantity} onChange={(e) => {
                      const next = [...itemRows]; next[i] = { ...next[i], quantity: e.target.value }; setItemRows(next);
                    }} required />
                  </div>
                ))}
                <button type="button" className="mt-2 text-xs text-primary" onClick={() => setItemRows([...itemRows, { product_id: "", quantity: "1" }])}>
                  + Add item
                </button>
              </div>
              <div className="mt-4 flex justify-end gap-2">
                <button type="button" className="btn-secondary" onClick={() => setShowCreate(false)}>Cancel</button>
                <button className="btn-primary">Create</button>
              </div>
            </form>
          </div>
        </div>
      )}

      {transitionFor && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4">
          <div className="panel w-full max-w-md p-6">
            <h3 className="mb-4 text-lg font-semibold">Update Status</h3>
            <form onSubmit={submitTransition} className="space-y-3">
              <div>
                <label className="block text-xs font-semibold text-muted">New Status</label>
                <select className="field mt-1" value={newStatus} onChange={(e) => setNewStatus(e.target.value)}>
                  {STATUSES.map((s) => (
                    <option key={s} value={s}>{STATUS_LABEL[s]}</option>
                  ))}
                </select>
              </div>
              <div>
                <label className="block text-xs font-semibold text-muted">Note</label>
                <input className="field mt-1" value={transitionNote} onChange={(e) => setTransitionNote(e.target.value)} />
              </div>
              <div className="mt-4 flex justify-end gap-2">
                <button type="button" className="btn-secondary" onClick={() => setTransitionFor(null)}>Cancel</button>
                <button className="btn-primary">Apply</button>
              </div>
            </form>
          </div>
        </div>
      )}

      {historyFor && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4">
          <div className="panel w-full max-w-md p-6">
            <div className="mb-4 flex items-center justify-between">
              <h3 className="text-lg font-semibold">Delivery Timeline</h3>
              <button className="btn-secondary text-xs" onClick={() => setHistoryFor(null)}>Close</button>
            </div>
            <ul className="space-y-3">
              {history.map((h) => (
                <li key={h.id} className="border-l-2 border-primary pl-3">
                  <p className="text-sm font-semibold">{h.from_status ? `${h.from_status} → ${h.to_status}` : h.to_status}</p>
                  {h.note && <p className="text-xs text-muted">{h.note}</p>}
                  <p className="text-xs text-subtle">{new Date(h.created_at).toLocaleString()}</p>
                </li>
              ))}
              {history.length === 0 && <p className="text-sm text-muted">No history.</p>}
            </ul>
          </div>
        </div>
      )}

      {noteFor && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4">
          <div className="panel w-full max-w-md p-6">
            <div className="mb-4 flex items-center justify-between">
              <h3 className="text-lg font-semibold">Delivery Note</h3>
              <button className="btn-secondary text-xs" onClick={() => setNoteFor(null)}>Close</button>
            </div>
            <pre className="max-h-80 overflow-auto rounded bg-muted-soft p-3 text-xs">{JSON.stringify(noteData, null, 2)}</pre>
          </div>
        </div>
      )}
    </>
  );
}
