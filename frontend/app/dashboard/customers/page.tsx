"use client";

import { useEffect, useState } from "react";
import { api } from "@/lib/api";
import { WalletCards } from "lucide-react";

interface Customer {
  id: string;
  name: string;
  phone: string;
  balance: number; // cents
  credit_limit: number; // cents
}

function formatMoney(cents: number): string {
  return (cents / 100).toFixed(2);
}

export default function CustomersPage() {
  const [customers, setCustomers] = useState<Customer[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const [name, setName] = useState("");
  const [phone, setPhone] = useState("");
  const [creditLimitInput, setCreditLimitInput] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [formError, setFormError] = useState("");
  const [paying, setPaying] = useState<string | null>(null);
  const [paymentAmount, setPaymentAmount] = useState("");

  async function loadCustomers() {
    try {
      const res = await api.get("/customers");
      setCustomers(res.data.data || []);
    } catch (err: any) {
      setError(err.response?.data?.error || "Failed to load customers");
    } finally {
      setLoading(false);
    }
  }

  async function recordPayment(customerId: string) {
    const amount = Math.round(Number(paymentAmount) * 100);
    if (!amount || amount <= 0) { setFormError("Enter a valid payment amount"); return; }
    try {
      await api.post(`/customers/${customerId}/payments`, { amount });
      setPaying(null); setPaymentAmount(""); await loadCustomers();
    } catch (err: any) { setFormError(err.response?.data?.error || "Failed to record payment"); }
  }

  useEffect(() => {
    loadCustomers();
  }, []);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setFormError("");
    setSubmitting(true);

    try {
      
      const limitCents = creditLimitInput
        ? Math.round(parseFloat(creditLimitInput) * 100)
        : 0;

      await api.post("/customers", {
        name,
        phone,
        credit_limit: limitCents,
      });

      setName("");
      setPhone("");
      setCreditLimitInput("");
      await loadCustomers();
    } catch (err: any) {
      setFormError(err.response?.data?.error || "Failed to add customer");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <main className="min-h-screen p-8">
      <h1 className="mb-6 text-2xl font-semibold">Customers</h1>

      <form
        onSubmit={handleSubmit}
        className="mb-8 grid max-w-2xl gap-3 rounded-lg border border-gray-200 p-4 sm:grid-cols-2"
      >
        {formError && (
          <p className="text-sm text-red-600 sm:col-span-2">{formError}</p>
        )}
        <input
          type="text"
          placeholder="Name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          className="rounded border border-gray-300 px-3 py-2"
          required
        />
        <input
          type="text"
          placeholder="Phone"
          value={phone}
          onChange={(e) => setPhone(e.target.value)}
          className="rounded border border-gray-300 px-3 py-2"
        />
        <input
          type="text"
          placeholder="Credit limit (KSh, e.g. 20000 — leave blank for no credit)"
          value={creditLimitInput}
          onChange={(e) => setCreditLimitInput(e.target.value)}
          className="rounded border border-gray-300 px-3 py-2 sm:col-span-2"
        />
        <button
          type="submit"
          disabled={submitting}
          className="rounded bg-brand-600 px-3 py-2 text-white disabled:opacity-50 sm:col-span-2"
        >
          {submitting ? "Adding..." : "Add customer"}
        </button>
      </form>

      {loading ? (
        <p>Loading customers...</p>
      ) : error ? (
        <p className="text-red-600">{error}</p>
      ) : customers.length === 0 ? (
        <p className="text-gray-500">No customers yet — add your first one above.</p>
      ) : (
        <table className="w-full max-w-2xl border-collapse text-left">
          <thead>
            <tr className="border-b border-gray-200 text-sm text-gray-500">
              <th className="py-2">Name</th>
              <th className="py-2">Phone</th>
              <th className="py-2 text-right">Balance owed</th>
              <th className="py-2 text-right">Credit limit</th>
              <th className="py-2 text-right">Action</th>
            </tr>
          </thead>
          <tbody>
            {customers.map((c) => (
              <tr key={c.id} className="border-b border-gray-100">
                <td className="py-2">{c.name}</td>
                <td className="py-2">{c.phone || "—"}</td>
                <td className="py-2 text-right">
                  <span className={c.balance > 0 ? "text-orange-600" : "text-gray-400"}>
                    KSh {formatMoney(c.balance)}
                  </span>
                </td>
                <td className="py-2 text-right text-gray-400">
                  KSh {formatMoney(c.credit_limit)}
                </td>
                <td className="py-2 text-right">
                  {paying === c.id ? <span className="inline-flex gap-2"><input className="field w-28 py-1.5" type="number" min="0.01" step="0.01" placeholder="KSh" value={paymentAmount} onChange={(e) => setPaymentAmount(e.target.value)} /><button className="btn-primary py-1.5" onClick={() => recordPayment(c.id)}>Pay</button></span> : <button className="btn-secondary py-1.5" disabled={c.balance <= 0} onClick={() => setPaying(c.id)}><WalletCards size={15}/> Payment</button>}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </main>
  );
}
