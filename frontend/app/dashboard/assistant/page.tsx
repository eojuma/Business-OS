"use client";

import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { Bot, Menu, Send, Sparkles, User } from "lucide-react";
import { api } from "@/lib/api";
import { AssistantChatMenu } from "@/components/assistant-chat-menu";
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
    renameConversation,
  } = useAssistantConversations();

  const entries = activeConversation.entries;
  const [input, setInput] = useState("");
  // Pending requests are tracked per conversation so one chat thinking does
  // not block composing or confirming in another.
  const [pendingIds, setPendingIds] = useState<Record<string, boolean>>({});
  const loading = Boolean(pendingIds[activeId]);
  const setConversationPending = useCallback((conversationId: string, value: boolean) => {
    setPendingIds((previous) => {
      if (value) {
        return previous[conversationId] ? previous : { ...previous, [conversationId]: true };
      }
      if (!previous[conversationId]) return previous;
      const next = { ...previous };
      delete next[conversationId];
      return next;
    });
  }, []);

  const [menuOpen, setMenuOpen] = useState(false);
  const menuButtonRef = useRef<HTMLButtonElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const scrollRef = useRef<HTMLDivElement>(null);
  const focusInputRef = useRef(false);
  const scrollPositions = useRef<Record<string, number>>({});
  const prevActiveRef = useRef(activeId);
  const switchedRef = useRef(false);
  const prevEntriesLenRef = useRef(entries.length);
  const prevPendingActiveRef = useRef(false);

  // Restore the destination offset before paint when switching conversations.
  // Without this, the browser clamps scrollTop, fires a scroll event, and
  // handleScroll overwrites the saved offset before it can be applied.
  useLayoutEffect(() => {
    if (!loaded) return;
    const element = scrollRef.current;
    if (!element) return;

    const switched = prevActiveRef.current !== activeId;
    prevActiveRef.current = activeId;

    if (!switched) return;
    switchedRef.current = true;
    const saved = scrollPositions.current[activeId];
    element.scrollTop = saved ?? element.scrollHeight;
  }, [activeId, loaded]);

  useEffect(() => {
    if (!loaded) return;

    const switched = switchedRef.current;
    switchedRef.current = false;

    const entriesChanged = entries.length !== prevEntriesLenRef.current;
    prevEntriesLenRef.current = entries.length;

    const pendingActive = Boolean(pendingIds[activeId]);
    const pendingChanged = pendingActive !== prevPendingActiveRef.current;
    prevPendingActiveRef.current = pendingActive;

    // A switch is restored by the layout effect above; appends are handled
    // below. Requests belonging to another chat must not move this one.
    if (switched) return;
    if (!entriesChanged && !pendingChanged) return;

    const element = scrollRef.current;
    if (!element) return;
    element.scrollTo({ top: element.scrollHeight, behavior: "smooth" });
  }, [activeId, loaded, entries.length, pendingIds]);

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
    if (!text || loading) return;

    const conversationId = activeId;
    const history = entries
      .slice(-6)
      .map((entry) => ({ role: entry.role, text: entry.text }));

    setInput("");
    focusInputRef.current = true;
    appendEntry(conversationId, { role: "user", text });
    setConversationPending(conversationId, true);

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
      setConversationPending(conversationId, false);
    }
  }

  async function handleConfirm(index: number, preview: Preview) {
    const conversationId = activeId;
    focusInputRef.current = true;
    setConversationPending(conversationId, true);

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
      setConversationPending(conversationId, false);
    }
  }

  function handleDismiss(index: number) {
    updateEntry(activeId, index, { resolved: true });
  }

  function closeMenu() {
    setMenuOpen(false);
    menuButtonRef.current?.focus();
  }

  function handleNewConversation() {
    newConversation();
    setMenuOpen(false);
    inputRef.current?.focus();
  }

  function handleSelectConversation(id: string) {
    selectConversation(id);
    setMenuOpen(false);
    inputRef.current?.focus();
  }

  function handleSuggestion(prompt: string) {
    if (entries.length > 0) newConversation();
    setInput(prompt);
    setMenuOpen(false);
    inputRef.current?.focus();
  }

  function applySuggestion(prompt: string) {
    setInput(prompt);
    inputRef.current?.focus();
  }

  const thinking = loading;

  return (
    <div className="mx-auto flex h-[calc(100dvh-6rem)] w-full max-w-3xl flex-col sm:h-[calc(100dvh-7rem)] md:h-[calc(100dvh-3rem)] lg:h-[calc(100dvh-4rem)]">
      <section className="panel relative flex min-h-0 flex-1 flex-col overflow-hidden">
        <AssistantChatMenu
          open={menuOpen}
          conversations={conversations}
          activeId={activeId}
          suggestions={suggestions}
          onClose={closeMenu}
          onSelect={handleSelectConversation}
          onNewChat={handleNewConversation}
          onRename={renameConversation}
          onDelete={closeConversation}
          onSuggestion={handleSuggestion}
        />
        <header className="shrink-0 border-b border-[#eef1ed] px-4 py-3 sm:px-5">
          <div className="flex items-center gap-2">
            <button
              ref={menuButtonRef}
              type="button"
              onClick={() => setMenuOpen((current) => !current)}
              aria-label="Chats"
              aria-expanded={menuOpen}
              aria-controls="assistant-chat-menu"
              className="rounded-lg p-2 text-[#4f5c53] transition hover:bg-[#f2f5f1]"
            >
              <Menu size={18} />
            </button>
            <span className="rounded-xl bg-[#e7f4ec] p-2 text-[#16794c]">
              <Bot size={18} />
            </span>
            <div className="min-w-0 flex-1">
              <p className="eyebrow">AI workspace</p>
              <h1 className="truncate text-lg font-bold tracking-tight sm:text-xl">Assistant</h1>
            </div>
          </div>
          <p className="mt-2 truncate text-sm text-[#718078]">
            {entries.length > 0
              ? activeConversation.title
              : "Record a sale or ask about stock, profit and balances. Nothing is saved until you confirm."}
          </p>
        </header>

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
