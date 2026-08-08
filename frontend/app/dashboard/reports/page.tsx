"use client";

import { useEffect, useState } from "react";
import { api } from "@/lib/api";

interface DailySummary {
  date: string;
  total_revenue: number; // cents
  sale_count: number;
}

function formatMoney(cents: number): string {
  return (cents / 100).toFixed(2);
}

function formatDate(iso: string): string {
  return new Date(iso).toLocaleDateString("en-KE", {
    weekday: "short",
    month: "short",
    day: "numeric",
  });
}

export default function ReportsPage() {
  const [summaries, setSummaries] = useState<DailySummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    async function load() {
      try {
        const res = await api.get("/reports/daily-sales");
        setSummaries(res.data.data || []);
      } catch (err: any) {
        setError(err.response?.data?.error || "Failed to load report");
      } finally {
        setLoading(false);
      }
    }
    load();
  }, []);

  const totalRevenue = summaries.reduce((sum, s) => sum + s.total_revenue, 0);
  const totalSales = summaries.reduce((sum, s) => sum + s.sale_count, 0);

  if (loading) return <main className="p-8">Loading report...</main>;
  if (error) return <main className="p-8 text-red-600">{error}</main>;

  return (
    <main className="min-h-screen p-8">
      <h1 className="mb-2 text-2xl font-semibold">Daily Sales Report</h1>
      <p className="mb-6 text-sm text-gray-500">Last 30 days</p>

      <div className="mb-6 grid max-w-2xl gap-4 sm:grid-cols-2">
        <div className="rounded-lg border border-gray-200 p-4">
          <p className="text-sm text-gray-500">Total Revenue</p>
          <p className="text-2xl font-semibold">KSh {formatMoney(totalRevenue)}</p>
        </div>
        <div className="rounded-lg border border-gray-200 p-4">
          <p className="text-sm text-gray-500">Total Sales</p>
          <p className="text-2xl font-semibold">{totalSales}</p>
        </div>
      </div>

      {summaries.length === 0 ? (
        <p className="text-gray-500">No sales recorded in this period yet.</p>
      ) : (
        <table className="w-full max-w-2xl border-collapse text-left">
          <thead>
            <tr className="border-b border-gray-200 text-sm text-gray-500">
              <th className="py-2">Date</th>
              <th className="py-2 text-right">Sales</th>
              <th className="py-2 text-right">Revenue</th>
            </tr>
          </thead>
          <tbody>
            {summaries.map((s) => (
              <tr key={s.date} className="border-b border-gray-100">
                <td className="py-2">{formatDate(s.date)}</td>
                <td className="py-2 text-right">{s.sale_count}</td>
                <td className="py-2 text-right">KSh {formatMoney(s.total_revenue)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </main>
  );
}