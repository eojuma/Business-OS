"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  appendEntry as appendEntryToState,
  conversationStorageKey,
  getActiveConversation,
  initialConversationState,
  loadConversations,
  mergeConversationStates,
  parseConversationState,
  readStoredValue,
  serializeConversationState,
  removeConversation,
  saveConversations,
  selectConversation as selectConversationInState,
  startConversation,
  updateEntry as updateEntryInState,
  type ChatEntry,
  type Conversation,
  type ConversationState,
} from "@/lib/assistant-conversations";

export interface AssistantConversations {
  conversations: Conversation[];
  activeId: string;
  activeConversation: Conversation;
  loaded: boolean;
  appendEntry: (conversationId: string, entry: ChatEntry) => void;
  updateEntry: (
    conversationId: string,
    index: number,
    patch: Partial<Pick<ChatEntry, "resolved">>,
  ) => void;
  newConversation: () => void;
  selectConversation: (id: string) => void;
  closeConversation: (id: string) => void;
}

export function useAssistantConversations(): AssistantConversations {
  const [state, setState] = useState<ConversationState>(initialConversationState);
  const [loaded, setLoaded] = useState(false);
  const storageKeyRef = useRef<string | null>(null);
  // True while the next state change comes from adopting another tab's write.
  const adoptedRef = useRef(false);

  useEffect(() => {
    // Storage access can throw (disabled cookies, enterprise policy,
    // sandboxed iframes); the guarded read returns null so the page falls
    // back to an in-memory session and still leaves the loading state.
    const token = readStoredValue("token");
    const key = conversationStorageKey(token);
    storageKeyRef.current = key;
    setState(loadConversations(key));
    setLoaded(true);
  }, []);

  useEffect(() => {
    if (!loaded || !storageKeyRef.current) return;
    const payload = serializeConversationState(state);
    if (adoptedRef.current) {
      adoptedRef.current = false;
      const stored = parseConversationState(readStoredValue(storageKeyRef.current));
      // Comparing content (activeId ignored) tells us whether this merge added
      // anything. Identical content would be a pointless write and, worse,
      // re-broadcast the other tab's snapshot for ever; a merge that added
      // chats must be persisted so a reload cannot lose them.
      if (stored) {
        const comparable = serializeConversationState({ ...stored, activeId: state.activeId });
        if (comparable === payload) return;
      }
    }
    saveConversations(storageKeyRef.current, state);
  }, [loaded, state]);

  // Another tab writing the same key fires a storage event here. Merge its
  // snapshot (newest copy per conversation) instead of overwriting its chats
  // with this tab's stale state.
  useEffect(() => {
    if (typeof window === "undefined") return;
    const onStorage = (event: StorageEvent) => {
      if (!storageKeyRef.current || event.key !== storageKeyRef.current || !event.newValue) return;
      const incoming = parseConversationState(event.newValue);
      if (!incoming) return;
      adoptedRef.current = true;
      setState((previous) => mergeConversationStates(previous, incoming));
    };
    window.addEventListener("storage", onStorage);
    return () => window.removeEventListener("storage", onStorage);
  }, []);

  const activeConversation = useMemo(() => getActiveConversation(state), [state]);

  const appendEntry = useCallback((conversationId: string, entry: ChatEntry) => {
    adoptedRef.current = false;
    setState((previous) => appendEntryToState(previous, conversationId, entry));
  }, []);

  const updateEntry = useCallback(
    (conversationId: string, index: number, patch: Partial<Pick<ChatEntry, "resolved">>) => {
      adoptedRef.current = false;
      setState((previous) => updateEntryInState(previous, conversationId, index, patch));
    },
    [],
  );

  const newConversation = useCallback(() => {
    adoptedRef.current = false;
    setState((previous) => startConversation(previous));
  }, []);

  const selectConversation = useCallback((id: string) => {
    adoptedRef.current = false;
    setState((previous) => selectConversationInState(previous, id));
  }, []);

  const closeConversation = useCallback((id: string) => {
    adoptedRef.current = false;
    setState((previous) => removeConversation(previous, id));
  }, []);

  return {
    conversations: state.conversations,
    activeId: state.activeId,
    activeConversation,
    loaded,
    appendEntry,
    updateEntry,
    newConversation,
    selectConversation,
    closeConversation,
  };
}
