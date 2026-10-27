"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  appendEntry as appendEntryToState,
  conversationStorageKey,
  getActiveConversation,
  initialConversationState,
  loadConversations,
  readStoredValue,
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
    saveConversations(storageKeyRef.current, state);
  }, [loaded, state]);

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
