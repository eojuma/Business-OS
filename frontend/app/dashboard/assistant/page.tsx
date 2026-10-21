"use client";

import { useEffect, useRef, useState } from "react";
import { Bot, Plus, Send, Sparkles, User, X } from "lucide-react";
import { api } from "@/lib/api";
import { useAssistantConversations } from "@/hooks/use-assistant-conversations";
import type { Preview } from "@/lib/assistant-conversations";

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
  const {
    conversations,
    activeId,
    activeConversation,
    loaded,
    appendEntry,
    updateEntry,
    newConversation,
    selectConversation,
    closeConversation,
  } = useAssistantConversations();

  const entries = activeConversation.entries;
  const [input, setInput] = useState("");
  const [pendingId, setPendingId] = useState<string | null>(null);
  const loading = pendingId !== null;

  const inputRef = useRef<HTMLInputElement>(null);
  const scrollRef = useRef<HTMLDivElement>(null);
  const focusInputRef = useRef(false);
  const scrollPositions = useRef<Record<string, number>>({});
  const prevActiveRef = useRef(activeId);

  useEffect(() => {
    if (!loaded) return;
    const element = scrollRef.current;
    if (!element) return;

    const switched = prevActiveRef.current !== activeId;
    prevActiveRef.current = activeId;

    if (switched) {
      const saved = scrollPositions.current[activeId];
      element.scrollTop = saved ?? element.scrollHeight;
      return;
    }
    element.scrollTo({ top: element.scrollHeight, behavior: "smooth" });
  }, [activeId, loaded, entries.length, pendingId]);

  useEffect(() => {
    if (!loading && focusInputRef.current) {
      focusInputRef.current = false;
      inputRef.current?.focus();
    }
  }, [loading]);

  function handleScroll(e: React.UIEvent<HTMLDivElement>) {
    scrollPositions.current[activeId] = e.currentTarget.scrollTop;
  }

  async function handleSend(e: React.FormEvent) {
    e.preventDefault();
    const text = input.trim();
    if (!text || pendingId) return;

    const conversationId = activeId;
    const history = entries
      .slice(-6)
      .map((entry) => ({ role: entry.role, text: entry.text }));

    setInput("");
    focusInputRef.current = true;
    appendEntry(conversationId, { role: "user", text });
    setPendingId(conversationId);

    try {
      const res = await api.post("/assistant/interpret", { text, history });
      const preview: Preview = res.data.data;

      appendEntry(conversationId, {
        role: "assistant",
        text: preview.Message,
        preview: preview.Kind === "sale" && preview.Understood ? preview : undefined,
      });
    } catch (err: any) {
      appendEntry(conversationId, {
        role: "assistant",
        text: err.response?.data?.error || "Something went wrong understanding that.",
      });
    } finally {
      setPendingId((current) => (current === conversationId ? null : current));
    }
  }

  async function handleConfirm(index: number, preview: Preview) {
    const conversationId = activeId;
    focusInputRef.current = true;
    setPendingId(conversationId);

    try {
      const res = await api.post("/assistant/confirm", {
        product_id: preview.ProductID,
        quantity: preview.Quantity,
      });
      const sale = res.data.data;

      updateEntry(conversationId, index, { resolved: true });
      appendEntry(conversationId, {
        role: "assistant",
        text: `Sale recorded — total KSh ${formatMoney(sale.TotalAmount)}.`,
      });
    } catch (err: any) {
      appendEntry(conversationId, {
        role: "assistant",
        text: err.response?.data?.error || "Failed to record the sale.",
      });
    } finally {
      setPendingId((current) => (current === conversationId ? null : current));
    }
  }

  function handleDismiss(index: number) {
    updateEntry(activeId, index, { resolved: true });
  }

  function handleNewConversation() {
    newConversation();
    inputRef.current?.focus();
  }

  function applySuggestion(prompt: string) {
    setInput(prompt);
    inputRef.current?.focus();
  }

  const thinking = pendingId === activeId;

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

        <div className="flex shrink-0 items-center gap-2 border-b border-[#eef1ed] px-3 py-2">
          <div
            className="chat-scroll flex min-w-0 flex-1 items-center gap-1 overflow-x-auto"
            aria-label="Conversations"
          >
            {conversations.map((conversation) => {
              const isActive = conversation.id === activeId;
              return (
                <div
                  key={conversation.id}
                  className={`group flex shrink-0 items-center gap-1 rounded-lg border px-2.5 py-1.5 text-xs transition ${
                    isActive
                      ? "border-[#16794c] bg-[#e7f4ec] text-[#12633e]"
                      : "border-transparent text-[#647168] hover:bg-[#f2f5f1]"
                  }`}
                >
                  <button
                    type="button"
                    onClick={() => selectConversation(conversation.id)}
                    aria-current={isActive ? "page" : undefined}
                    className="max-w-[150px] truncate font-semibold"
                    title={conversation.title}
                  >
                    {conversation.title}
                  </button>
                  {conversations.length > 1 && (
                    <button
                      type="button"
                      onClick={() => closeConversation(conversation.id)}
                      aria-label={`Close ${conversation.title}`}
                      className="rounded p-0.5 text-[#8a958d] transition hover:bg-[#d9e0d9] hover:text-[#17211b]"
                    >
                      <X size={12} />
                    </button>
                  )}
                </div>
              );
            })}
          </div>
          <button
            type="button"
            onClick={handleNewConversation}
            className="btn-secondary shrink-0 px-2.5 py-1.5 text-xs"
            aria-label="New chat"
          >
            <Plus size={14} />
            <span className="hidden sm:inline">New chat</span>
          </button>
        </div>

        <div
          ref={scrollRef}
          onScroll={handleScroll}
          className="chat-scroll flex-1 space-y-3 overflow-y-auto overscroll-contain px-4 py-4 sm:px-6"
          role="log"
          aria-live="polite"
          aria-busy={loading}
        >
          {!loaded && (
            <div className="flex h-full items-center justify-center text-sm text-[#8a958d]">
              Loading chats…
            </div>
          )}

          {loaded && entries.length === 0 && (
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

          {loaded &&
            entries.map((entry, index) => (
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

          {thinking && (
            <div
              className="flex items-center gap-2"
              role="status"
              aria-label="Assistant is thinking"
            >
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
