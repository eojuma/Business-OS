"use client";

import { useEffect, useRef, useState } from "react";
import { Bot, Send, Sparkles, User } from "lucide-react";
import { api } from "@/lib/api";

interface Preview {
  Kind: "sale" | "answer";
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

const suggestions = [
  { label: "Record a sale", prompt: "sold 3 cement" },
  { label: "Profit today", prompt: "what's my profit today?" },
  { label: "Low stock", prompt: "which products are low on stock?" },
  { label: "Who owes me?", prompt: "who owes me money?" },
];

export default function AssistantPage() {
  const [entries, setEntries] = useState<ChatEntry[]>([]);
  const [input, setInput] = useState("");
  const [loading, setLoading] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const endRef = useRef<HTMLDivElement>(null);
  const focusInputRef = useRef(false);

  useEffect(() => {
    endRef.current?.scrollIntoView({ behavior: "smooth", block: "end" });
  }, [entries.length, loading]);

  useEffect(() => {
    if (!loading && focusInputRef.current) {
      focusInputRef.current = false;
      inputRef.current?.focus();
    }
  }, [loading]);

  async function handleSend(e: React.FormEvent) {
    e.preventDefault();
    if (!input.trim()) return;

    const text = input.trim();
    setInput("");
    focusInputRef.current = true;
    const history = entries
      .slice(-6)
      .map((entry) => ({ role: entry.role, text: entry.text }));
    setEntries((prev) => [...prev, { role: "user", text }]);
    setLoading(true);

    try {
      const res = await api.post("/assistant/interpret", { text, history });
      const preview: Preview = res.data.data;

      setEntries((prev) => [
        ...prev,
        {
          role: "assistant",
          text: preview.Message,
          preview: preview.Kind === "sale" && preview.Understood ? preview : undefined,
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
    focusInputRef.current = true;
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

  function applySuggestion(prompt: string) {
    setInput(prompt);
    inputRef.current?.focus();
  }

  return (
    <div className="mx-auto flex h-[calc(100dvh-6rem)] w-full max-w-3xl flex-col sm:h-[calc(100dvh-7rem)] md:h-[calc(100dvh-3rem)] lg:h-[calc(100dvh-4rem)]">
      <section className="panel flex min-h-0 flex-1 flex-col overflow-hidden">
        <header className="shrink-0 border-b border-[#eef1ed] px-4 py-4 sm:px-6">
          <div className="flex items-start gap-3">
            <span className="rounded-xl bg-[#e7f4ec] p-2.5 text-[#16794c]">
              <Bot size={18} />
            </span>
            <div className="min-w-0">
              <p className="eyebrow">AI workspace</p>
              <h1 className="mt-0.5 text-xl font-bold tracking-tight sm:text-2xl">Assistant</h1>
              <p className="mt-1 text-sm text-[#718078]">
                Record a sale or ask about stock, profit and balances. Nothing is saved
                until you confirm.
              </p>
            </div>
          </div>
        </header>

        <div
          className="chat-scroll flex-1 space-y-3 overflow-y-auto overscroll-contain px-4 py-4 sm:px-6"
          role="log"
          aria-live="polite"
        >
          {entries.length === 0 && (
            <div className="flex h-full flex-col items-center justify-center px-2 text-center">
              <span className="rounded-2xl bg-[#e7f4ec] p-3 text-[#16794c]">
                <Sparkles size={22} />
              </span>
              <p className="mt-3 text-sm font-semibold">Start with a question or a sale</p>
              <p className="mt-1 max-w-sm text-xs text-[#718078]">
                The assistant can look things up instantly, and asks you to confirm
                before recording a sale.
              </p>
              <div className="mt-4 flex flex-wrap justify-center gap-2">
                {suggestions.map((suggestion) => (
                  <button
                    key={suggestion.prompt}
                    type="button"
                    onClick={() => applySuggestion(suggestion.prompt)}
                    className="rounded-full border border-[#d9e0d9] bg-white px-3 py-1.5 text-xs font-semibold text-[#344139] transition hover:bg-[#f2f5f1]"
                  >
                    {suggestion.label}
                  </button>
                ))}
              </div>
            </div>
          )}

          {entries.map((entry, index) => (
            <div
              key={index}
              className={`flex gap-2 ${entry.role === "user" ? "justify-end" : "justify-start"}`}
            >
              {entry.role === "assistant" && (
                <span className="mt-0.5 hidden h-7 w-7 shrink-0 items-center justify-center rounded-full bg-[#e7f4ec] text-[#16794c] sm:flex">
                  <Bot size={14} />
                </span>
              )}

              <div
                className={`max-w-[85%] rounded-2xl px-3.5 py-2.5 text-sm sm:max-w-[75%] ${
                  entry.role === "user"
                    ? "rounded-br-sm bg-[#16794c] text-white"
                    : "rounded-bl-sm border border-[#e5e9e4] bg-[#f7f9f6] text-[#17211b]"
                }`}
              >
                <p className="whitespace-pre-wrap break-words">{entry.text}</p>

                {entry.preview && !entry.resolved && (
                  <div className="mt-3 flex flex-wrap gap-2">
                    <button
                      type="button"
                      onClick={() => handleConfirm(index, entry.preview!)}
                      disabled={loading}
                      className="btn-primary px-3 py-1.5 text-xs"
                    >
                      Confirm
                    </button>
                    <button
                      type="button"
                      onClick={() => handleDismiss(index)}
                      disabled={loading}
                      className="btn-secondary px-3 py-1.5 text-xs"
                    >
                      Cancel
                    </button>
                  </div>
                )}
              </div>

              {entry.role === "user" && (
                <span className="mt-0.5 hidden h-7 w-7 shrink-0 items-center justify-center rounded-full bg-[#eef1ed] text-[#52616a] sm:flex">
                  <User size={14} />
                </span>
              )}
            </div>
          ))}

          {loading && (
            <div className="flex items-center gap-2" role="status" aria-label="Assistant is thinking">
              <span className="hidden h-7 w-7 shrink-0 items-center justify-center rounded-full bg-[#e7f4ec] text-[#16794c] sm:flex">
                <Bot size={14} />
              </span>
              <div className="flex items-center gap-1.5 rounded-2xl rounded-bl-sm border border-[#e5e9e4] bg-[#f7f9f6] px-3.5 py-3 text-sm text-[#718078]">
                <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-[#16794c] [animation-delay:-0.2s]" />
                <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-[#16794c] [animation-delay:-0.1s]" />
                <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-[#16794c]" />
                <span className="ml-1">Thinking</span>
              </div>
            </div>
          )}

          <div ref={endRef} />
        </div>

        <form
          onSubmit={handleSend}
          className="shrink-0 border-t border-[#eef1ed] bg-white p-3 sm:p-4"
        >
          <div className="flex items-center gap-2">
            <input
              ref={inputRef}
              type="text"
              value={input}
              onChange={(e) => setInput(e.target.value)}
              placeholder='e.g. "sold 3 cement"'
              className="field min-w-0 flex-1"
              disabled={loading}
              aria-label="Message the assistant"
              autoComplete="off"
            />
            <button
              type="submit"
              disabled={loading || !input.trim()}
              className="btn-primary shrink-0 px-4"
              aria-label="Send message"
            >
              <Send size={16} />
              <span className="hidden sm:inline">{loading ? "Sending" : "Send"}</span>
            </button>
          </div>
          <p className="mt-1.5 hidden text-[11px] text-[#8a958d] sm:block">
            Nothing is saved until you confirm a sale.
          </p>
        </form>
      </section>
    </div>
  );
}
