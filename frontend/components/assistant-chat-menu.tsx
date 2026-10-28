"use client";

import { useEffect, useRef, useState } from "react";
import { MessageSquare, Pencil, Plus, Sparkles, X } from "lucide-react";
import type { Conversation } from "@/lib/assistant-conversations";

export interface AssistantSuggestion {
  label: string;
  prompt: string;
}

const FOCUSABLE_SELECTOR =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

interface AssistantChatMenuProps {
  open: boolean;
  conversations: Conversation[];
  activeId: string;
  suggestions: AssistantSuggestion[];
  onClose: () => void;
  onSelect: (id: string) => void;
  onNewChat: () => void;
  onRename: (id: string, title: string) => void;
  onDelete: (id: string) => void;
  onSuggestion: (prompt: string) => void;
}

export function AssistantChatMenu({
  open,
  conversations,
  activeId,
  suggestions,
  onClose,
  onSelect,
  onNewChat,
  onRename,
  onDelete,
  onSuggestion,
}: AssistantChatMenuProps) {
  const panelRef = useRef<HTMLElement>(null);
  const renameButtonsRef = useRef(new Map<string, HTMLButtonElement>());
  const restoreFocusRef = useRef<string | null>(null);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [draft, setDraft] = useState("");
  const finishedRef = useRef(false);

  useEffect(() => {
    if (!open) {
      setEditingId(null);
      setDraft("");
      restoreFocusRef.current = null;
      return;
    }
    panelRef.current?.focus();
  }, [open]);

  // Finishing a rename unmounts the focused input; put focus back on the
  // row's rename button so the focus trap does not bounce to the top.
  useEffect(() => {
    if (!open || editingId !== null) return;
    const id = restoreFocusRef.current;
    if (!id) return;
    restoreFocusRef.current = null;
    renameButtonsRef.current.get(id)?.focus();
  }, [open, editingId]);

  useEffect(() => {
    if (!open) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Tab") {
        const panel = panelRef.current;
        if (!panel) return;
        const focusable = Array.from(
          panel.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR),
        ).filter((element) => element.offsetParent !== null);
        if (focusable.length === 0) {
          event.preventDefault();
          panel.focus();
          return;
        }
        const first = focusable[0];
        const last = focusable[focusable.length - 1];
        const active = document.activeElement;
        // The panel itself is the initial focus target (tabIndex -1), so it
        // counts as the boundary: Tab starts at the first control, Shift+Tab
        // wraps to the last one instead of leaving the dialog.
        const inside = active instanceof HTMLElement && active !== panel && panel.contains(active);
        if (event.shiftKey) {
          if (active === first || !inside) {
            event.preventDefault();
            last.focus();
          }
        } else if (active === last || !inside) {
          event.preventDefault();
          first.focus();
        }
        return;
      }

      if (event.key !== "Escape") return;
      event.stopPropagation();
      if (editingId) {
        finishedRef.current = true;
        restoreFocusRef.current = editingId;
        setEditingId(null);
        setDraft("");
        return;
      }
      onClose();
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [open, editingId, onClose]);

  function beginRename(conversation: Conversation) {
    finishedRef.current = false;
    setEditingId(conversation.id);
    setDraft(conversation.title);
  }

  function finishRename(commit: boolean, restoreFocus = false) {
    if (!editingId || finishedRef.current) return;
    finishedRef.current = true;
    if (commit) onRename(editingId, draft);
    if (restoreFocus) restoreFocusRef.current = editingId;
    setEditingId(null);
    setDraft("");
  }

  return (
    <>
      {open && (
        <button
          type="button"
          tabIndex={-1}
          aria-hidden="true"
          aria-label="Close chats"
          onClick={onClose}
          className="absolute inset-0 z-20 cursor-default bg-black/20"
        />
      )}

      <aside
        id="assistant-chat-menu"
        ref={panelRef}
        tabIndex={-1}
        role="dialog"
        aria-modal="true"
        aria-label="Chats"
        aria-hidden={!open}
        className={`absolute inset-y-0 left-0 z-30 flex w-[min(19rem,85%)] flex-col border-r border-[#e5e9e4] bg-white shadow-xl outline-none transition-transform duration-200 ${
          open ? "visible translate-x-0" : "invisible -translate-x-full"
        }`}
      >
        <div className="flex items-center justify-between gap-2 border-b border-[#eef1ed] px-4 py-3">
          <p className="text-sm font-semibold">Chats</p>
          <button
            type="button"
            onClick={onClose}
            aria-label="Close chats"
            className="rounded-lg p-1.5 text-[#4f5c53] transition hover:bg-[#f2f5f1]"
          >
            <X size={16} />
          </button>
        </div>

        <div className="chat-scroll flex-1 overflow-y-auto px-2 py-3">
          <button
            type="button"
            onClick={onNewChat}
            className="btn-secondary mb-4 w-full justify-center text-xs"
          >
            <Plus size={14} />
            New chat
          </button>

          <p className="px-2 pb-1 text-[11px] font-semibold uppercase tracking-wide text-[#8a958d]">
            Quick actions
          </p>
          <div className="mb-4 space-y-0.5">
            {suggestions.map((suggestion) => (
              <button
                key={suggestion.prompt}
                type="button"
                onClick={() => onSuggestion(suggestion.prompt)}
                className="flex w-full items-center gap-2 rounded-lg px-2 py-2 text-left text-xs text-[#526057] transition hover:bg-[#f2f5f1] hover:text-[#17211b]"
              >
                <Sparkles size={14} className="shrink-0 text-[#16794c]" />
                {suggestion.label}
              </button>
            ))}
          </div>

          <p className="px-2 pb-1 text-[11px] font-semibold uppercase tracking-wide text-[#8a958d]">
            Recent
          </p>
          <ul className="space-y-0.5">
            {conversations.map((conversation) => {
              const isActive = conversation.id === activeId;
              const isEditing = editingId === conversation.id;
              return (
                <li key={conversation.id}>
                  <div
                    className={`group flex items-center gap-1 rounded-lg px-1 ${
                      isActive
                        ? "bg-[#e7f4ec] text-[#12633e]"
                        : "text-[#526057] hover:bg-[#f2f5f1]"
                    }`}
                  >
                    <MessageSquare size={14} className="ml-1 shrink-0 opacity-60" />
                    {isEditing ? (
                      <input
                        autoFocus
                        value={draft}
                        onChange={(event) => setDraft(event.target.value)}
                        onKeyDown={(event) => {
                          if (event.key === "Enter") {
                            event.preventDefault();
                            finishRename(true, true);
                          }
                          if (event.key === "Escape") {
                            event.preventDefault();
                            finishRename(false, true);
                          }
                        }}
                        onBlur={() => finishRename(true)}
                        maxLength={42}
                        aria-label="Chat name"
                        className="my-1 min-w-0 flex-1 rounded border border-[#cfd8cf] px-1.5 py-1 text-xs outline-none focus:border-[#16794c]"
                      />
                    ) : (
                      <button
                        type="button"
                        onClick={() => onSelect(conversation.id)}
                        aria-current={isActive ? "page" : undefined}
                        title={conversation.title}
                        className="min-w-0 flex-1 truncate px-1 py-2 text-left text-xs font-medium"
                      >
                        {conversation.title}
                      </button>
                    )}
                    {!isEditing && (
                      <span
                        className={`flex shrink-0 items-center transition ${
                          isActive ? "opacity-100" : "opacity-0 group-hover:opacity-100 focus-within:opacity-100"
                        }`}
                      >
                        <button
                          type="button"
                          ref={(element) => {
                            if (element) {
                              renameButtonsRef.current.set(conversation.id, element);
                            } else {
                              renameButtonsRef.current.delete(conversation.id);
                            }
                          }}
                          onClick={() => beginRename(conversation)}
                          aria-label={`Rename ${conversation.title}`}
                          title="Rename chat"
                          className="rounded p-1 text-[#8a958d] transition hover:bg-[#d9e0d9] hover:text-[#17211b]"
                        >
                          <Pencil size={12} />
                        </button>
                        {conversations.length > 1 && (
                          <button
                            type="button"
                            onClick={() => onDelete(conversation.id)}
                            aria-label={`Close ${conversation.title}`}
                            title="Close chat"
                            className="rounded p-1 text-[#8a958d] transition hover:bg-[#d9e0d9] hover:text-[#17211b]"
                          >
                            <X size={12} />
                          </button>
                        )}
                      </span>
                    )}
                  </div>
                </li>
              );
            })}
          </ul>
        </div>
      </aside>
    </>
  );
}
