"use client";

import { useEffect, useState } from "react";
import { api } from "@/lib/api";

interface Sale {
  id: string;
  total_amount: number;
  created_at: string;
}

interface StockLevel {
  id: string;
  product_id: string;
  quantity: number;
  low_stock_threshold: number;
}

function formatMoney(cents: number): string {
  return (cents / 100).toFixed(2);
}

export default function DashboardPage() {
  const [sales, setSales] = useState<Sale[]>([]);
  const [lowStock, setLowStock] = useState<StockLevel[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    async function loadDashboard() {
      try {
        const [salesRes, lowStockRes] = await Promise.all([
          api.get("/sales"),
          api.get("/inventory/low-stock"),
        ]);
        setSales(salesRes.data.data || []);
        setLowStock(lowStockRes.data.data || []);
      } catch (err: any) {
        setError(err.response?.data?.error || "Failed to load dashboard");
      } finally {
        setLoading(false);
      }
    }
    loadDashboard();
  }, []);

  const today = new Date().toDateString();
  const todaysSales = sales.filter(
    (s) => new Date(s.created_at).toDateString() === today
  );
  const todaysTotal = todaysSales.reduce((sum, s) => sum + s.total_amount, 0);

  if (loading) {
    return <main className="p-8">Loading dashboard...</main>;
  }

  if (error) {
    return <main className="p-8 text-red-600">{error}</main>;
  }

  return (
    <main className="min-h-screen p-8">
      <h1 className="mb-6 text-2xl font-semibold">Dashboard</h1>

      <div className="grid gap-4 sm:grid-cols-2">
        <div className="rounded-lg border border-gray-200 p-4">
          <p className="text-sm text-gray-500">Today's Sales</p>
          <p className="text-2xl font-semibold">
            KSh {formatMoney(todaysTotal)}
          </p>
          <p className="text-sm text-gray-500">
            {todaysSales.length} transaction{todaysSales.length !== 1 && "s"}
          </p>
        </div>

        <div className="rounded-lg border border-gray-200 p-4">
          <p className="text-sm text-gray-500">Low Stock Alerts</p>
          <p className="text-2xl font-semibold">{lowStock.length}</p>
          <p className="text-sm text-gray-500">
            product{lowStock.length !== 1 && "s"} need restocking
          </p>
        </div>
      </div>
    </main>
  );
}