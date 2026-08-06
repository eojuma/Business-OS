"use client";

import { useEffect, useState } from "react";
import { api } from "@/lib/api";

interface Product {
  id: string;
  name: string;
  unit: string;
}

interface StockLevel {
  product_id: string;
  quantity: number;
  low_stock_threshold: number;
}

export default function InventoryPage() {
  const [products, setProducts] = useState<Product[]>([]);
  const [levels, setLevels] = useState<Record<string, StockLevel>>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  // movement form state
  const [productId, setProductId] = useState("");
  const [type, setType] = useState("restock");
  const [quantity, setQuantity] = useState("");
  const [note, setNote] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [formError, setFormError] = useState("");

  async function loadInventory() {
    setLoading(true);
    try {
      const productsRes = await api.get("/products");
      const productList: Product[] = productsRes.data.data || [];
      setProducts(productList);

      const levelResults = await Promise.all(
        productList.map((p) =>
          api
            .get(`/inventory/levels/${p.id}`)
            .then((res) => res.data.data as StockLevel)
            .catch(() => ({
              product_id: p.id,
              quantity: 0,
              low_stock_threshold: 0,
            }))
        )
      );

      const levelMap: Record<string, StockLevel> = {};
      levelResults.forEach((lvl) => {
        levelMap[lvl.product_id] = lvl;
      });
      setLevels(levelMap);
    } catch (err: any) {
      setError(err.response?.data?.error || "Failed to load inventory");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    loadInventory();
  }, []);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setFormError("");
    setSubmitting(true);

    try {
      const qty = parseInt(quantity, 10);
      if (isNaN(qty) || qty <= 0) {
        setFormError("Enter a valid quantity");
        setSubmitting(false);
        return;
      }
      if (!productId) {
        setFormError("Select a product");
        setSubmitting(false);
        return;
      }

      await api.post("/inventory/movements", {
        product_id: productId,
        type,
        quantity: qty,
        note,
      });

      setQuantity("");
      setNote("");
      await loadInventory();
    } catch (err: any) {
      setFormError(err.response?.data?.error || "Failed to record movement");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <main className="min-h-screen p-8">
      <h1 className="mb-6 text-2xl font-semibold">Inventory</h1>

      <form
        onSubmit={handleSubmit}
        className="mb-8 grid max-w-2xl gap-3 rounded-lg border border-gray-200 p-4 sm:grid-cols-2"
      >
        {formError && (
          <p className="text-sm text-red-600 sm:col-span-2">{formError}</p>
        )}
        <select
          value={productId}
          onChange={(e) => setProductId(e.target.value)}
          className="rounded border border-gray-300 px-3 py-2"
          required
        >
          <option value="">Select product</option>
          {products.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name}
            </option>
          ))}
        </select>
        <select
          value={type}
          onChange={(e) => setType(e.target.value)}
          className="rounded border border-gray-300 px-3 py-2"
        >
          <option value="restock">Restock (+)</option>
          <option value="adjustment">Adjustment</option>
          <option value="return">Return (+)</option>
        </select>
        <input
          type="text"
          placeholder="Quantity"
          value={quantity}
          onChange={(e) => setQuantity(e.target.value)}
          className="rounded border border-gray-300 px-3 py-2"
          required
        />
        <input
          type="text"
          placeholder="Note (optional)"
          value={note}
          onChange={(e) => setNote(e.target.value)}
          className="rounded border border-gray-300 px-3 py-2"
        />
        <button
          type="submit"
          disabled={submitting}
          className="rounded bg-brand-600 px-3 py-2 text-white disabled:opacity-50 sm:col-span-2"
        >
          {submitting ? "Recording..." : "Record movement"}
        </button>
      </form>

      {loading ? (
        <p>Loading inventory...</p>
      ) : error ? (
        <p className="text-red-600">{error}</p>
      ) : products.length === 0 ? (
        <p className="text-gray-500">No products yet — add some on the Products page first.</p>
      ) : (
        <table className="w-full max-w-2xl border-collapse text-left">
          <thead>
            <tr className="border-b border-gray-200 text-sm text-gray-500">
              <th className="py-2">Product</th>
              <th className="py-2 text-right">Quantity</th>
              <th className="py-2 text-right">Status</th>
            </tr>
          </thead>
          <tbody>
            {products.map((p) => {
              const level = levels[p.id];
              const qty = level?.quantity ?? 0;
              const threshold = level?.low_stock_threshold ?? 0;
              const isLow = qty <= threshold;
              return (
                <tr key={p.id} className="border-b border-gray-100">
                  <td className="py-2">
                    {p.name} <span className="text-gray-400">({p.unit})</span>
                  </td>
                  <td className="py-2 text-right">{qty}</td>
                  <td className="py-2 text-right">
                    {isLow ? (
                      <span className="text-red-600">Low stock</span>
                    ) : (
                      <span className="text-gray-400">OK</span>
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
    </main>
  );
}