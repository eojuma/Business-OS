"use client";

import { useEffect, useState } from "react";
import { api } from "@/lib/api";

interface Product {
  id: string;
  name: string;
  unit: string;
  price: number; // cents
}

interface Customer {
  id: string;
  name: string;
}

interface LineItem {
  productId: string;
  quantity: string;
}

function formatMoney(cents: number): string {
  return (cents / 100).toFixed(2);
}

export default function SalesPage() {
  const [products, setProducts] = useState<Product[]>([]);
  const [customers, setCustomers] = useState<Customer[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const [items, setItems] = useState<LineItem[]>([{ productId: "", quantity: "" }]);
  const [customerId, setCustomerId] = useState(""); // "" = walk-in/cash
  const [submitting, setSubmitting] = useState(false);
  const [formError, setFormError] = useState("");
  const [successMsg, setSuccessMsg] = useState("");

  useEffect(() => {
    async function load() {
      try {
        const [productsRes, customersRes] = await Promise.all([
          api.get("/products"),
          api.get("/customers"),
        ]);
        setProducts(productsRes.data.data || []);
        setCustomers(customersRes.data.data || []);
      } catch (err: any) {
        setError(err.response?.data?.error || "Failed to load sales page");
      } finally {
        setLoading(false);
      }
    }
    load();
  }, []);

  function updateItem(index: number, field: keyof LineItem, value: string) {
    const next = [...items];
    next[index] = { ...next[index], [field]: value };
    setItems(next);
  }

  function addItemRow() {
    setItems([...items, { productId: "", quantity: "" }]);
  }

  function removeItemRow(index: number) {
    setItems(items.filter((_, i) => i !== index));
  }

  // Estimated total, shown for the cashier's benefit only — the real,
  // authoritative total is computed server-side from live product
  // prices, same principle as backend/modules/sales/service.go.
  const estimatedTotal = items.reduce((sum, item) => {
    const product = products.find((p) => p.id === item.productId);
    const qty = parseInt(item.quantity, 10);
    if (!product || isNaN(qty)) return sum;
    return sum + product.price * qty;
  }, 0);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setFormError("");
    setSuccessMsg("");
    setSubmitting(true);

    try {
      const validItems = items
        .filter((item) => item.productId && item.quantity)
        .map((item) => ({
          product_id: item.productId,
          quantity: parseInt(item.quantity, 10),
        }));

      if (validItems.length === 0) {
        setFormError("Add at least one product to the sale");
        setSubmitting(false);
        return;
      }

      const payload: any = { items: validItems };
      if (customerId) {
        payload.customer_id = customerId;
      }

      const res = await api.post("/sales", payload);
      const sale = res.data.data;

      setSuccessMsg(`Sale recorded — total KSh ${formatMoney(sale.total_amount)}`);
      setItems([{ productId: "", quantity: "" }]);
      setCustomerId("");
    } catch (err: any) {
      setFormError(err.response?.data?.error || "Failed to record sale");
    } finally {
      setSubmitting(false);
    }
  }

  if (loading) return <main className="p-8">Loading...</main>;
  if (error) return <main className="p-8 text-red-600">{error}</main>;

  return (
    <main className="min-h-screen p-8">
      <h1 className="mb-6 text-2xl font-semibold">Record a Sale</h1>

      <form onSubmit={handleSubmit} className="max-w-2xl space-y-4 rounded-lg border border-gray-200 p-4">
        {formError && <p className="text-sm text-red-600">{formError}</p>}
        {successMsg && <p className="text-sm text-green-600">{successMsg}</p>}

        {products.length === 0 ? (
          <p className="text-gray-500">No products yet — add some on the Products page first.</p>
        ) : (
          <>
            <div className="space-y-2">
              {items.map((item, index) => (
                <div key={index} className="flex gap-2">
                  <select
                    value={item.productId}
                    onChange={(e) => updateItem(index, "productId", e.target.value)}
                    className="flex-1 rounded border border-gray-300 px-3 py-2"
                    required
                  >
                    <option value="">Select product</option>
                    {products.map((p) => (
                      <option key={p.id} value={p.id}>
                        {p.name} — KSh {formatMoney(p.price)}/{p.unit}
                      </option>
                    ))}
                  </select>
                  <input
                    type="text"
                    placeholder="Qty"
                    value={item.quantity}
                    onChange={(e) => updateItem(index, "quantity", e.target.value)}
                    className="w-24 rounded border border-gray-300 px-3 py-2"
                    required
                  />
                  {items.length > 1 && (
                    <button
                      type="button"
                      onClick={() => removeItemRow(index)}
                      className="rounded border border-gray-300 px-3 text-gray-500"
                    >
                      ×
                    </button>
                  )}
                </div>
              ))}
            </div>

            <button
              type="button"
              onClick={addItemRow}
              className="text-sm text-brand-600"
            >
              + Add another product
            </button>

            <div>
              <label className="mb-1 block text-sm text-gray-500">
                Customer (optional — leave blank for cash/walk-in sale)
              </label>
              <select
                value={customerId}
                onChange={(e) => setCustomerId(e.target.value)}
                className="w-full rounded border border-gray-300 px-3 py-2"
              >
                <option value="">Walk-in / cash sale</option>
                {customers.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.name} (on credit)
                  </option>
                ))}
              </select>
            </div>

            <p className="text-sm text-gray-500">
              Estimated total: KSh {formatMoney(estimatedTotal)}{" "}
              <span className="text-gray-400">(final total confirmed on save)</span>
            </p>

            <button
              type="submit"
              disabled={submitting}
              className="w-full rounded bg-brand-600 px-3 py-2 text-white disabled:opacity-50"
            >
              {submitting ? "Recording..." : "Complete sale"}
            </button>
          </>
        )}
      </form>
    </main>
  );
}