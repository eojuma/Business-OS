"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { AlertCircle, Printer, RefreshCw } from "lucide-react";
import { api } from "@/lib/api";
import { formatMoney } from "@/components/app-shell";

const buckets = [
  { key: "current", label: "Current", description: "Not overdue or no due date", color: "border-emerald-500 bg-emerald-50 text-emerald-900 dark:bg-emerald-950 dark:text-emerald-200" },
  { key: "days_1_30", label: "1–30 days", description: "1 to 30 days overdue", color: "border-yellow-500 bg-yellow-50 text-yellow-900 dark:bg-yellow-950 dark:text-yellow-200" },
  { key: "days_31_60", label: "31–60 days", description: "31 to 60 days overdue", color: "border-orange-500 bg-orange-50 text-orange-900 dark:bg-orange-950 dark:text-orange-200" },
  { key: "days_61_90", label: "61–90 days", description: "61 to 90 days overdue", color: "border-red-500 bg-red-50 text-red-900 dark:bg-red-950 dark:text-red-200" },
  { key: "days_90_plus", label: "90+ days", description: "More than 90 days overdue", color: "border-red-900 bg-red-950 text-red-100" },
] as const;

type Bucket = (typeof buckets)[number]["key"];
type AgingSupplier = {
  id: string;
  name: string;
  outstanding_balance: number;
  due_date: string | null;
  days_overdue: number;
};
type AgingReport = Record<Bucket, AgingSupplier[]> & {
  as_of: string;
  totals: Record<Bucket, number>;
};

function formatDate(value: string): string {
  return new Date(value).toLocaleDateString("en-KE", {
    day: "numeric", month: "short", year: "numeric", timeZone: "UTC",
  });
}

function isDateOnly(value: string | null | undefined): value is string {
  if (typeof value !== "string" || !/^\d{4}-\d{2}-\d{2}$/.test(value)) return false;
  const [year, month, day] = value.split("-").map(Number);
  if (month < 1 || month > 12 || day < 1) return false;
  // Day 0 of the following month is the last day of this one.
  const daysInMonth = new Date(Date.UTC(year, month, 0)).getUTCDate();
  return day <= daysInMonth;
}

export function SupplierAgingReport({ initialAsOf }: { initialAsOf?: string | null }) {
  const router = useRouter();
  const [asOf, setAsOf] = useState<string | null>(null);
  const [report, setReport] = useState<AgingReport | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [reload, setReload] = useState(0);

  useEffect(() => {
    // Prefer the date carried in the URL (via the page) so returning from a
    // supplier detail re-renders the same aging date; fall back to today.
    const fromUrl = new URLSearchParams(window.location.search).get("as_of");
    setAsOf(
      (isDateOnly(initialAsOf) && initialAsOf) ||
        (isDateOnly(fromUrl) && fromUrl) ||
        new Date().toISOString().slice(0, 10),
    );
  }, [initialAsOf]);

  useEffect(() => {
    if (asOf === null) return;
    const controller = new AbortController();
    setLoading(true);
    setError("");
    setReport(null);
    api.get("/suppliers/aging", {
      params: asOf ? { as_of: `${asOf}T00:00:00Z` } : undefined,
      signal: controller.signal,
    }).then((response) => {
      if (!controller.signal.aborted) setReport(response.data.data);
    }).catch((e: unknown) => {
      if (controller.signal.aborted) return;
      const failure = e as { response?: { data?: { error?: string } } };
      setError(failure.response?.data?.error || "Could not load the aging report. Please try again.");
    }).finally(() => {
      if (!controller.signal.aborted) setLoading(false);
    });
    return () => controller.abort();
  }, [asOf, reload]);

  return (
    <section id="aging-report" aria-label="Supplier aging report" className="space-y-6">
      <style>{`
        @media print {
          body * { visibility: hidden; }
          #aging-report, #aging-report * { visibility: visible; }
          #aging-report { position: absolute; left: 0; top: 0; width: 100%; }
          #aging-report, #aging-report * {
            /* Browsers print on white paper and omit backgrounds. */
            color: #111827 !important;
            background: #ffffff !important;
            box-shadow: none !important;
          }
          .no-print { display: none !important; }
          #aging-report section { break-inside: avoid; }
        }
      `}</style>
      <div className="panel flex flex-col gap-4 p-5 sm:flex-row sm:items-end sm:justify-between">
        <div className="max-w-2xl">
          <h2 className="text-lg font-semibold">Payables aging</h2>
          <p className="mt-1 text-sm text-muted">
            Current outstanding balances for active suppliers, grouped by each supplier&apos;s saved due date.
            Every selected date uses current balances. Dates and overdue days use UTC.
          </p>
          <p className="mt-1 text-xs text-subtle">Suppliers without a due date appear in Current.</p>
        </div>
        <div className="no-print flex flex-wrap items-end gap-3">
          <label className="text-sm font-medium">
            As of date (UTC)
            <input
              type="date"
              min="0001-01-01"
              max="9999-12-31"
              className="field mt-1 [color-scheme:light] dark:[color-scheme:dark]"
              value={asOf ?? ""}
              onChange={(event) => {
                const next = event.target.value;
                setAsOf(next);
                const params = new URLSearchParams(window.location.search);
                params.set("tab", "aging");
                if (next) params.set("as_of", next);
                else params.delete("as_of");
                router.replace(`/dashboard/suppliers?${params.toString()}`, { scroll: false });
              }}
            />
          </label>
          <button type="button" className="btn-secondary" onClick={() => setReload((value) => value + 1)} disabled={loading}>
            <RefreshCw size={15} /> Refresh
          </button>
          <button type="button" className="btn-secondary" onClick={() => window.print()}>
            <Printer size={15} /> Print / Save as PDF
          </button>
        </div>
      </div>

      {loading ? (
        <div className="panel p-10 text-center text-muted" role="status">Loading aging report…</div>
      ) : error ? (
        <div className="panel p-8 text-center" role="alert">
          <AlertCircle className="mx-auto text-danger" aria-hidden="true" />
          <p className="mt-3 text-sm">{error}</p>
          <button type="button" className="btn-secondary mt-4" onClick={() => setReload((value) => value + 1)}>Try again</button>
        </div>
      ) : report && (
        <>
          <p className="text-sm text-muted">Aged as of {formatDate(report.as_of)} (UTC)</p>
          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
            {buckets.map((bucket) => (
              <a key={bucket.key} href={`#aging-${bucket.key}`} className={`min-w-0 rounded-xl border-l-4 p-4 ${bucket.color}`}>
                <p className="text-sm font-semibold">{bucket.label}</p>
                <p className="mt-2 break-words text-xl font-bold">{formatMoney(report.totals[bucket.key])}</p>
                <p className="mt-1 text-xs">{report[bucket.key].length} supplier{report[bucket.key].length === 1 ? "" : "s"}</p>
              </a>
            ))}
          </div>
          {buckets.every((bucket) => report[bucket.key].length === 0) && (
            <p className="panel p-6 text-center text-muted" role="status">No outstanding supplier balances.</p>
          )}
          {buckets.map((bucket) => (
            <section key={bucket.key} id={`aging-${bucket.key}`} aria-labelledby={`heading-${bucket.key}`} className="panel scroll-mt-20 overflow-hidden">
              <div className={`border-l-4 px-5 py-3 ${bucket.color}`}>
                <h3 id={`heading-${bucket.key}`} className="font-semibold">{bucket.label}</h3>
                <p className="text-xs">{bucket.description}</p>
              </div>
              <div className="overflow-x-auto">
                <table className="w-full min-w-[560px] text-left text-sm">
                  <caption className="sr-only">{bucket.description}: outstanding supplier balances</caption>
                  <thead className="bg-surface-muted text-xs uppercase text-muted">
                    <tr>
                      <th scope="col" className="px-5 py-3">Supplier</th>
                      <th scope="col" className="px-5 py-3">Due date (UTC)</th>
                      <th scope="col" className="px-5 py-3 text-right">Days overdue</th>
                      <th scope="col" className="px-5 py-3 text-right">Outstanding</th>
                    </tr>
                  </thead>
                  <tbody>
                    {report[bucket.key].length === 0 ? (
                      <tr><td colSpan={4} className="px-5 py-5 text-muted">No suppliers in this bucket.</td></tr>
                    ) : report[bucket.key].map((supplier) => (
                      <tr key={supplier.id} className="border-t border-border-subtle">
                        <td className="px-5 py-4"><Link className="font-semibold text-primary underline-offset-4 hover:underline focus-visible:underline" href={`/dashboard/suppliers/${supplier.id}${asOf ? `?as_of=${asOf}` : ""}`}>{supplier.name}</Link></td>
                        <td className="px-5 py-4 text-muted">{supplier.due_date ? formatDate(supplier.due_date) : "No due date"}</td>
                        <td className="px-5 py-4 text-right tabular-nums">{supplier.days_overdue}</td>
                        <td className="px-5 py-4 text-right font-medium tabular-nums">{formatMoney(supplier.outstanding_balance)}</td>
                      </tr>
                    ))}
                  </tbody>
                  <tfoot className="border-t border-border bg-surface-muted font-semibold">
                    <tr><th scope="row" colSpan={3} className="px-5 py-3">Bucket total</th><td className="px-5 py-3 text-right tabular-nums">{formatMoney(report.totals[bucket.key])}</td></tr>
                  </tfoot>
                </table>
              </div>
            </section>
          ))}
        </>
      )}
    </section>
  );
}
