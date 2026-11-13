"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { ShieldCheck, X } from "lucide-react";
import { api } from "@/lib/api";
import { PageHeader } from "@/components/app-shell";
import { usePermissions } from "@/hooks/usePermissions";
import { PERMISSIONS } from "@/lib/permissions";
import {
  AUDIT_ACTIONS,
  AUDIT_ENTITIES,
  auditActionLabel,
  auditEntityLabel,
  auditTimestamp,
  type AuditEntry,
} from "@/lib/audit";

type User = { id: string; name: string; email: string };
type Meta = { page: number; per_page: number; total: number };

/** Extracts the API error message with a safe fallback. */
function errorMessage(error: unknown, fallback: string): string {
  const e = error as { response?: { data?: { error?: string } } };
  return e.response?.data?.error || fallback;
}

/** Read-only audit trail viewer for owners and admins. */
export default function AuditPage() {
  const { can } = usePermissions();
  const canRead = can(PERMISSIONS.auditRead);

  const [entries, setEntries] = useState<AuditEntry[]>([]);
  const [users, setUsers] = useState<User[]>([]);
  const [meta, setMeta] = useState<Meta | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [filterError, setFilterError] = useState("");

  const [actorId, setActorId] = useState("");
  const [action, setAction] = useState("");
  const [entityType, setEntityType] = useState("");
  const [dateFrom, setDateFrom] = useState("");
  const [dateTo, setDateTo] = useState("");
  const [page, setPage] = useState(1);
  const limit = 20;

  /** Loads audit entries for the current filters and page. */
  const loadEntries = useCallback(async () => {
    if (!canRead) {
      setLoading(false);
      return;
    }
    setLoading(true);
    setError("");
    try {
      const params: Record<string, string | number> = { page, limit };
      if (actorId) params.actor_id = actorId;
      if (action) params.action = action;
      if (entityType) params.entity_type = entityType;
      if (dateFrom) params.date_from = dateFrom;
      if (dateTo) params.date_to = dateTo;
      const res = await api.get("/audit", { params });
      setEntries(res.data.data || []);
      setMeta(res.data.meta || null);
    } catch (e) {
      setEntries([]);
      setMeta(null);
      setError(errorMessage(e, "Failed to load the audit log"));
    } finally {
      setLoading(false);
    }
  }, [action, actorId, canRead, dateFrom, dateTo, entityType, page]);

  useEffect(() => {
    void loadEntries();
  }, [loadEntries]);

  useEffect(() => {
    if (!canRead) return;
    let active = true;
    api
      .get("/users")
      .then((res) => {
        if (active) setUsers(res.data.data || []);
      })
      .catch(() => {
        if (active) setUsers([]);
      });
    return () => {
      active = false;
    };
  }, [canRead]);

  if (!canRead) {
    return <div className="panel p-6 text-sm text-muted">Your role does not have access to the audit log.</div>;
  }

  const totalPages = meta ? Math.max(1, Math.ceil(meta.total / meta.per_page)) : 1;

  /** Clears every filter and returns to the first page. */
  function resetFilters() {
    setActorId("");
    setAction("");
    setEntityType("");
    setDateFrom("");
    setDateTo("");
    setPage(1);
  }

  return (
    <>
      <PageHeader
        eyebrow="Security"
        title="Audit log"
        description="Who changed what, when, and from where. Entries are append-only."
      />

      {filterError && (
        <p className="mb-4 rounded-lg bg-warning-soft p-3 text-sm text-warning-strong">{filterError}</p>
      )}

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
          <ShieldCheck size={15} /> Filters
        </div>
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-6">
          <label className="text-xs font-semibold text-muted">
            Actor
            <select
              className="field mt-1.5"
              value={actorId}
              onChange={(e) => {
                setActorId(e.target.value);
                setPage(1);
              }}
            >
              <option value="">All actors</option>
              {users.map((user) => (
                <option key={user.id} value={user.id}>
                  {user.name}
                </option>
              ))}
            </select>
          </label>
          <label className="text-xs font-semibold text-muted">
            Action
            <select
              className="field mt-1.5"
              value={action}
              onChange={(e) => {
                setAction(e.target.value);
                setPage(1);
              }}
            >
              <option value="">All actions</option>
              {AUDIT_ACTIONS.map((value) => (
                <option key={value} value={value}>
                  {auditActionLabel(value)}
                </option>
              ))}
            </select>
          </label>
          <label className="text-xs font-semibold text-muted">
            Entity
            <select
              className="field mt-1.5"
              value={entityType}
              onChange={(e) => {
                setEntityType(e.target.value);
                setPage(1);
              }}
            >
              <option value="">All entities</option>
              {AUDIT_ENTITIES.map((value) => (
                <option key={value} value={value}>
                  {auditEntityLabel(value)}
                </option>
              ))}
            </select>
          </label>
          <label className="text-xs font-semibold text-muted">
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
          <label className="text-xs font-semibold text-muted">
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
            <p className="eyebrow">Activity trail</p>
            <h2 className="mt-1 text-lg font-bold">Recorded changes</h2>
          </div>
          <span className="text-sm text-muted">{meta?.total ?? 0} entries</span>
        </div>

        {loading ? (
          <p className="p-8 text-sm text-muted">Loading audit entries...</p>
        ) : entries.length === 0 ? (
          <p className="p-8 text-sm text-muted">No audit entries match these filters.</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[900px] text-left text-sm">
              <thead className="bg-surface-muted text-xs uppercase text-muted">
                <tr>
                  <th className="px-5 py-3">When</th>
                  <th className="px-5 py-3">Actor</th>
                  <th className="px-5 py-3">Action</th>
                  <th className="px-5 py-3">Entity</th>
                  <th className="px-5 py-3">IP</th>
                  <th className="px-5 py-3">Details</th>
                </tr>
              </thead>
              <tbody>
                {entries.map((entry) => (
                  <tr key={entry.id} className="border-t border-border-subtle align-top">
                    <td className="whitespace-nowrap px-5 py-4 text-muted">{auditTimestamp(entry.created_at)}</td>
                    <td className="px-5 py-4">
                      <p className="font-semibold">{entry.actor_name || "System"}</p>
                      {entry.actor_email && <p className="text-xs text-muted">{entry.actor_email}</p>}
                    </td>
                    <td className="px-5 py-4">
                      <span className="rounded-full bg-hover px-2.5 py-1 text-xs font-bold">
                        {auditActionLabel(entry.action)}
                      </span>
                    </td>
                    <td className="px-5 py-4">
                      <p className="font-semibold">{auditEntityLabel(entry.entity_type)}</p>
                      {entry.entity_id && <p className="font-mono text-xs text-muted">{entry.entity_id.slice(0, 8)}</p>}
                    </td>
                    <td className="px-5 py-4 font-mono text-xs text-muted">{entry.ip_address || "—"}</td>
                    <td className="px-5 py-4">
                      <details>
                        <summary className="cursor-pointer text-xs font-semibold text-primary">View</summary>
                        <div className="mt-2 grid gap-2 text-xs">
                          <div>
                            <p className="font-semibold text-muted">Before</p>
                            <pre className="mt-1 max-h-40 overflow-auto rounded-lg bg-background p-2 text-[11px]">
                              {JSON.stringify(entry.before ?? null, null, 2)}
                            </pre>
                          </div>
                          <div>
                            <p className="font-semibold text-muted">After</p>
                            <pre className="mt-1 max-h-40 overflow-auto rounded-lg bg-background p-2 text-[11px]">
                              {JSON.stringify(entry.after ?? null, null, 2)}
                            </pre>
                          </div>
                          <p className="font-mono text-[11px] text-subtle">
                            {entry.method} {entry.path} · {entry.status_code}
                          </p>
                        </div>
                      </details>
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
