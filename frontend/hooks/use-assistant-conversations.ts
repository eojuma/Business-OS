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
  // Payload adopted from another tab; the save effect skips exactly that
  // snapshot so it is not re-broadcast, while genuine local edits still save.
  const adoptedSnapshotRef = useRef<string | null>(null);

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
    if (adoptedSnapshotRef.current === payload) {
      // Adopting another tab's write is not a local change; saving it again
      // would make the tabs rewrite each other indefinitely.
      adoptedSnapshotRef.current = null;
      return;
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
      setState((previous) => {
        const merged = mergeConversationStates(previous, incoming);
        adoptedSnapshotRef.current = serializeConversationState(merged);
        return merged;
      });
    };
    window.addEventListener("storage", onStorage);
    return () => window.removeEventListener("storage", onStorage);
  }, []);

  const activeConversation = useMemo(() => getActiveConversation(state), [state]);

  const appendEntry = useCallback((conversationId: string, entry: ChatEntry) => {
    setState((previous) => appendEntryToState(previous, conversationId, entry));
  }, []);

  const updateEntry = useCallback(
    (conversationId: string, index: number, patch: Partial<Pick<ChatEntry, "resolved">>) => {
      setState((previous) => updateEntryInState(previous, conversationId, index, patch));
    },
    [],
  );

  const newConversation = useCallback(() => {
    setState((previous) => startConversation(previous));
  }, []);

  const selectConversation = useCallback((id: string) => {
    setState((previous) => selectConversationInState(previous, id));
  }, []);

  const closeConversation = useCallback((id: string) => {
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
