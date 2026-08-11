"use client";

import { useState } from "react";
import { api } from "@/lib/api";

interface Preview {
  Understood: boolean;
  ProductID: string;
  ProductName: string;
  Quantity: number;
  UnitPrice: number; // cents
  TotalAmount: number; // cents
  Message: string;
}

interface ChatEntry {
  role: "user" | "assistant";
  text: string;
  preview?: Preview; // only set on assistant entries that are awaiting confirmation
  resolved?: boolean; // true once confirmed or dismissed — hides the buttons
}

function formatMoney(cents: number): string {
  return (cents / 100).toFixed(2);
}

export default function AssistantPage() {
  const [entries, setEntries] = useState<ChatEntry[]>([]);
  const [input, setInput] = useState("");
  const [loading, setLoading] = useState(false);

  async function handleSend(e: React.FormEvent) {
    e.preventDefault();
    if (!input.trim()) return;

    const text = input.trim();
    setInput("");
    setEntries((prev) => [...prev, { role: "user", text }]);
    setLoading(true);

    try {
      const res = await api.post("/assistant/interpret", { text });
      const preview: Preview = res.data.data;

      setEntries((prev) => [
        ...prev,
        {
          role: "assistant",
          text: preview.Message,
          preview: preview.Understood ? preview : undefined,
        },
      ]);
    } catch (err: any) {
      setEntries((prev) => [
        ...prev,
        {
          role: "assistant",
          text: err.response?.data?.error || "Something went wrong understanding that.",
        },
      ]);
    } finally {
      setLoading(false);
    }
  }

  async function handleConfirm(index: number, preview: Preview) {
    setLoading(true);
    try {
      const res = await api.post("/assistant/confirm", {
        product_id: preview.ProductID,
        quantity: preview.Quantity,
      });
      const sale = res.data.data;

      setEntries((prev) => {
        const next = [...prev];
        next[index] = { ...next[index], resolved: true };
        return [
          ...next,
          {
            role: "assistant",
            text: `Sale recorded — total KSh ${formatMoney(sale.TotalAmount)}.`,
          },
        ];
      });
    } catch (err: any) {
      setEntries((prev) => [
        ...prev,
        {
          role: "assistant",
          text: err.response?.data?.error || "Failed to record the sale.",
        },
      ]);
    } finally {
      setLoading(false);
    }
  }

  function handleDismiss(index: number) {
    setEntries((prev) => {
      const next = [...prev];
      next[index] = { ...next[index], resolved: true };
      return next;
    });
  }

  return (
    <main className="flex min-h-screen flex-col p-8">
      <h1 className="mb-2 text-2xl font-semibold">Assistant</h1>
      <p className="mb-6 text-sm text-gray-500">
        Try: &quot;sold 3 cement&quot; — nothing is recorded until you confirm.
      </p>

      <div className="mb-4 max-w-2xl flex-1 space-y-3">
        {entries.length === 0 && (
          <p className="text-sm text-gray-400">No messages yet.</p>
        )}

        {entries.map((entry, index) => (
          <div
            key={index}
            className={`rounded-lg p-3 text-sm ${
              entry.role === "user"
                ? "ml-auto max-w-[80%] bg-brand-600 text-white"
                : "max-w-[80%] bg-gray-100 text-gray-900"
            }`}
          >
            <p>{entry.text}</p>

            {entry.preview && !entry.resolved && (
              <div className="mt-3 flex gap-2">
                <button
                  onClick={() => handleConfirm(index, entry.preview!)}
                  disabled={loading}
                  className="rounded bg-brand-700 px-3 py-1 text-white disabled:opacity-50"
                >
                  Confirm
                </button>
                <button
                  onClick={() => handleDismiss(index)}
                  disabled={loading}
                  className="rounded border border-gray-300 px-3 py-1 text-gray-600"
                >
                  Cancel
                </button>
              </div>
            )}
          </div>
        ))}
      </div>

      <form onSubmit={handleSend} className="flex max-w-2xl gap-2">
        <input
          type="text"
          placeholder='e.g. "sold 3 cement"'
          value={input}
          onChange={(e) => setInput(e.target.value)}
          className="flex-1 rounded border border-gray-300 px-3 py-2"
          disabled={loading}
        />
        <button
          type="submit"
          disabled={loading || !input.trim()}
          className="rounded bg-brand-600 px-4 py-2 text-white disabled:opacity-50"
        >
          {loading ? "..." : "Send"}
        </button>
      </form>
    </main>
  );
}