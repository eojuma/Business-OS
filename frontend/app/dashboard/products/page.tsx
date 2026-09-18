"use client";

import { useEffect, useState } from "react";
import { api } from "@/lib/api";

interface Product {
  id: string;
  name: string;
  category: string;
  unit: string;
  price: number; // cents
  sku?: string;
}

function formatMoney(cents: number): string {
  return (cents / 100).toFixed(2);
}

export default function ProductsPage() {
  const [products, setProducts] = useState<Product[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  // form state
  const [name, setName] = useState("");
  const [category, setCategory] = useState("");
  const [unit, setUnit] = useState("");
  const [priceInput, setPriceInput] = useState(""); // shillings, as typed
  const [submitting, setSubmitting] = useState(false);
  const [formError, setFormError] = useState("");

  const [editingId, setEditingId] = useState<string | null>(null);
  const [editPrice, setEditPrice] = useState("");
  const [saving, setSaving] = useState(false);

  async function savePrice(id: string) {
    const cents = Math.round(parseFloat(editPrice) * 100);
    if (isNaN(cents) || cents < 0) {
      setError("Enter a valid price");
      return;
    }
    setSaving(true);
    try {
      await api.patch(`/products/${id}`, { price: cents });
      setEditingId(null);
      setEditPrice("");
      await loadProducts();
    } catch (err: any) {
      setError(err.response?.data?.error || "Failed to update price");
    } finally {
      setSaving(false);
    }
  }

  async function loadProducts() {
    try {
      const res = await api.get("/products");
      setProducts(res.data.data || []);
    } catch (err: any) {
      setError(err.response?.data?.error || "Failed to load products");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    loadProducts();
  }, []);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setFormError("");
    setSubmitting(true);

    try {
    
      const priceCents = Math.round(parseFloat(priceInput) * 100);
      if (isNaN(priceCents) || priceCents < 0) {
        setFormError("Enter a valid price");
        setSubmitting(false);
        return;
      }

      await api.post("/products", {
        name,
        category,
        unit,
        price: priceCents,
      });

      setName("");
      setCategory("");
      setUnit("");
      setPriceInput("");
      await loadProducts();
    } catch (err: any) {
      setFormError(err.response?.data?.error || "Failed to add product");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <main className="min-h-screen p-8">
      <h1 className="mb-6 text-2xl font-semibold">Products</h1>

      <form
        onSubmit={handleSubmit}
        className="mb-8 grid max-w-2xl gap-3 rounded-lg border border-gray-200 p-4 sm:grid-cols-2"
      >
        {formError && (
          <p className="text-sm text-red-600 sm:col-span-2">{formError}</p>
        )}
        <input
          type="text"
          placeholder="Name (e.g. Cement 50kg)"
          value={name}
          onChange={(e) => setName(e.target.value)}
          className="rounded border border-gray-300 px-3 py-2"
          required
        />
        <input
          type="text"
          placeholder="Category (e.g. Building Materials)"
          value={category}
          onChange={(e) => setCategory(e.target.value)}
          className="rounded border border-gray-300 px-3 py-2"
        />
        <input
          type="text"
          placeholder="Unit (e.g. bag, piece, kg)"
          value={unit}
          onChange={(e) => setUnit(e.target.value)}
          className="rounded border border-gray-300 px-3 py-2"
          required
        />
        <input
          type="text"
          placeholder="Price (KSh, e.g. 450.50)"
          value={priceInput}
          onChange={(e) => setPriceInput(e.target.value)}
          className="rounded border border-gray-300 px-3 py-2"
          required
        />
        <button
          type="submit"
          disabled={submitting}
          className="rounded bg-brand-600 px-3 py-2 text-white disabled:opacity-50 sm:col-span-2"
        >
          {submitting ? "Adding..." : "Add product"}
        </button>
      </form>

      {loading ? (
        <p>Loading products...</p>
      ) : error ? (
        <p className="text-red-600">{error}</p>
      ) : products.length === 0 ? (
        <p className="text-gray-500">No products yet — add your first one above.</p>
      ) : (
        <table className="w-full max-w-2xl border-collapse text-left">
          <thead>
            <tr className="border-b border-gray-200 text-sm text-gray-500">
              <th className="py-2">Name</th>
              <th className="py-2">Category</th>
              <th className="py-2">Unit</th>
              <th className="py-2 text-right">Price</th>
              <th className="py-2 text-right">Adjust</th>
            </tr>
          </thead>
          <tbody>
            {products.map((p) => (
              <tr key={p.id} className="border-b border-gray-100">
                <td className="py-2">{p.name}</td>
                <td className="py-2">{p.category || "—"}</td>
                <td className="py-2">{p.unit}</td>
                <td className="py-2 text-right">KSh {formatMoney(p.price)}</td>
                <td className="py-2 text-right">
                  {editingId === p.id ? (
                    <span className="inline-flex items-center gap-2">
                      <input
                        type="text"
                        value={editPrice}
                        onChange={(e) => setEditPrice(e.target.value)}
                        placeholder="New price"
                        className="w-24 rounded border border-gray-300 px-2 py-1 text-sm"
                      />
                      <button
                        onClick={() => savePrice(p.id)}
                        disabled={saving}
                        className="rounded bg-brand-600 px-2 py-1 text-xs text-white disabled:opacity-50"
                      >
                        Save
                      </button>
                      <button
                        onClick={() => {
                          setEditingId(null);
                          setEditPrice("");
                        }}
                        className="text-xs text-gray-500"
                      >
                        Cancel
                      </button>
                    </span>
                  ) : (
                    <button
                      onClick={() => {
                        setEditingId(p.id);
                        setEditPrice((p.price / 100).toString());
                      }}
                      className="text-xs font-semibold text-brand-600"
                    >
                      Adjust price
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </main>
  );
}