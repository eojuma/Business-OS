import { describe, expect, it } from "vitest";
import {
  appendEntry,
  conversationScopeFromToken,
  conversationStorageKey,
  deriveTitle,
  emptyState,
  getActiveConversation,
  initialConversationState,
  loadConversations,
  MAX_CONVERSATIONS,
  MAX_ENTRIES,
  mergeConversationStates,
  NEW_CHAT_TITLE,
  parseConversationState,
  removeConversation,
  saveConversations,
  selectConversation,
  serializeConversationState,
  startConversation,
  updateEntry,
  type ConversationState,
  type StorageLike,
} from "./assistant-conversations";

function fakeToken(payload: Record<string, unknown>): string {
  const header = Buffer.from(JSON.stringify({ alg: "HS256", typ: "JWT" })).toString("base64url");
  const body = Buffer.from(JSON.stringify(payload)).toString("base64url");
  return `${header}.${body}.signature`;
}

function memoryStorage(): StorageLike & { data: Map<string, string> } {
  const data = new Map<string, string>();
  return {
    data,
    getItem: (key) => data.get(key) ?? null,
    setItem: (key, value) => {
      data.set(key, value);
    },
  };
}

function stateWithTwoConversations(): { state: ConversationState; firstId: string; secondId: string } {
  const base = emptyState(1);
  const firstId = base.activeId;
  const withFirst = appendEntry(base, firstId, { role: "user", text: "first chat" }, 2);
  const state = startConversation(withFirst, 3);
  return { state, firstId, secondId: state.activeId };
}

describe("assistant conversation model", () => {
  it("creates a deterministic initial state and one empty conversation", () => {
    const initial = initialConversationState();
    expect(initial.conversations).toHaveLength(1);
    expect(initial.conversations[0].id).toBe("initial");
    expect(initial.conversations[0].title).toBe("New chat");
    expect(initial.activeId).toBe("initial");

    const empty = emptyState(1000);
    expect(empty.conversations).toHaveLength(1);
    expect(empty.conversations[0].title).toBe("New chat");
    expect(empty.conversations[0].createdAt).toBe(1000);
    expect(empty.activeId).toBe(empty.conversations[0].id);
  });

  it("derives a truncated title from the first user message", () => {
    expect(deriveTitle([])).toBe("New chat");
    expect(deriveTitle([{ role: "assistant", text: "hello" }])).toBe("New chat");
    expect(deriveTitle([{ role: "user", text: "  sold   3 cement  " }])).toBe("sold 3 cement");

    const longMessage = "a".repeat(60);
    const title = deriveTitle([{ role: "user", text: longMessage }]);
    expect(title).toHaveLength(43);
    expect(title.endsWith("…")).toBe(true);
  });

  it("appends to the target conversation and keeps the first title", () => {
    const base = emptyState(1);
    const id = base.activeId;

    const withUser = appendEntry(base, id, { role: "user", text: "sold 2 hammers" }, 2);
    expect(withUser.conversations[0].entries).toHaveLength(1);
    expect(withUser.conversations[0].title).toBe("sold 2 hammers");
    expect(withUser.conversations[0].updatedAt).toBe(2);

    const withAssistant = appendEntry(withUser, id, { role: "assistant", text: "Confirm?" }, 3);
    expect(withAssistant.conversations[0].entries).toHaveLength(2);
    expect(withAssistant.conversations[0].title).toBe("sold 2 hammers");
  });

  it("only updates the conversation matching the id", () => {
    const { state, firstId, secondId } = stateWithTwoConversations();
    const updated = appendEntry(state, firstId, { role: "user", text: "another message" }, 4);

    expect(updated.conversations.find((c) => c.id === firstId)?.entries).toHaveLength(2);
    expect(updated.conversations.find((c) => c.id === secondId)?.entries).toHaveLength(0);
    expect(updated.activeId).toBe(secondId);
  });

  it("marks an entry resolved only when the index is valid", () => {
    const base = emptyState(1);
    const withEntry = appendEntry(base, base.activeId, { role: "assistant", text: "preview" }, 2);
    const resolved = updateEntry(withEntry, base.activeId, 0, { resolved: true }, 3);
    expect(resolved.conversations[0].entries[0].resolved).toBe(true);
    expect(updateEntry(resolved, base.activeId, 5, { resolved: false })).toBe(resolved);
  });

  it("starts a new conversation when the active one has messages", () => {
    const base = emptyState(1);
    const withMessage = appendEntry(base, base.activeId, { role: "user", text: "hello" }, 2);
    const next = startConversation(withMessage, 3);

    expect(next.conversations).toHaveLength(2);
    expect(next.conversations[0].id).toBe(next.activeId);
    expect(next.conversations[1].id).toBe(base.activeId);
  });

  it("reuses an untouched draft instead of stacking empty chats", () => {
    const base = emptyState(1);
    expect(startConversation(base, 2)).toBe(base);
  });

  it("caps the number of stored conversations", () => {
    let state = emptyState(0);
    for (let i = 1; i <= MAX_CONVERSATIONS; i += 1) {
      state = appendEntry(state, state.activeId, { role: "user", text: `chat ${i}` }, i);
      state = startConversation(state, i);
    }
    expect(state.conversations).toHaveLength(MAX_CONVERSATIONS);
    expect(state.conversations[0].id).toBe(state.activeId);
  });

  it("evicts the least recently updated chat, not the one on screen", () => {
    let state = emptyState(0);
    for (let i = 1; i <= MAX_CONVERSATIONS; i += 1) {
      state = appendEntry(state, state.activeId, { role: "user", text: `chat ${i}` }, i);
      state = startConversation(state, i);
    }

    // Open the oldest chat, then start a new one.
    const oldest = state.conversations[state.conversations.length - 1];
    const secondOldest = state.conversations[state.conversations.length - 2];
    state = selectConversation(state, oldest.id);
    const next = startConversation(state, MAX_CONVERSATIONS + 1);

    expect(next.conversations).toHaveLength(MAX_CONVERSATIONS);
    expect(next.conversations.some((conversation) => conversation.id === oldest.id)).toBe(true);
    expect(next.conversations.some((conversation) => conversation.id === secondOldest.id)).toBe(false);
    expect(next.conversations.some((conversation) => conversation.id === next.activeId)).toBe(true);
  });

  it("merges another tab's snapshot without losing either side's chats", () => {
    const localBase = emptyState(1);
    const local = appendEntry(localBase, localBase.activeId, { role: "user", text: "local chat" }, 2);

    const remoteBase = emptyState(10);
    const remote = appendEntry(remoteBase, remoteBase.activeId, { role: "user", text: "remote chat" }, 11);

    const merged = mergeConversationStates(local, remote);
    expect(merged.conversations.map((conversation) => conversation.id).sort()).toEqual(
      [localBase.activeId, remoteBase.activeId].sort(),
    );
    expect(merged.activeId).toBe(localBase.activeId);
  });

  it("unions a shared chat and keeps the latest updatedAt", () => {
    const base = emptyState(1);
    const id = base.activeId;
    const older = appendEntry(base, id, { role: "user", text: "old" }, 2);
    const newer = appendEntry(base, id, { role: "user", text: "newer" }, 5);

    const merged = mergeConversationStates(older, newer);
    const conversation = merged.conversations.find((item) => item.id === id);
    expect(conversation?.entries.map((entry) => entry.text)).toEqual(["old", "newer"]);
    expect(conversation?.updatedAt).toBe(5);
  });

  it("does not resurrect a chat closed in another tab", () => {
    const { state, firstId } = stateWithTwoConversations();
    const closed = removeConversation(state, firstId, 10);

    const merged = mergeConversationStates(state, closed);
    expect(merged.conversations.some((conversation) => conversation.id === firstId)).toBe(false);
    expect(merged.removed?.[firstId]).toBe(10);
  });

  it("falls back to a fresh chat when both tabs removed every conversation", () => {
    const { state, firstId, secondId } = stateWithTwoConversations();
    const closedFirst = removeConversation(state, firstId, 10);
    const closedSecond = removeConversation(state, secondId, 11);

    const merged = mergeConversationStates(closedFirst, closedSecond);
    expect(merged.conversations).toHaveLength(1);
    expect(merged.conversations[0].entries).toHaveLength(0);
    expect(merged.activeId).toBe(merged.conversations[0].id);
  });

  it("does not tombstone its own recovered chat", () => {
    const recovered = {
      id: "recovered",
      title: NEW_CHAT_TITLE,
      createdAt: 0,
      updatedAt: 0,
      entries: [],
    };
    const other = { ...recovered, id: "other-chat", createdAt: 5, updatedAt: 5 };
    const local: ConversationState = {
      conversations: [recovered],
      activeId: recovered.id,
      removed: { [other.id]: 10 },
    };
    const incoming: ConversationState = {
      conversations: [other],
      activeId: other.id,
      removed: { [recovered.id]: 11 },
    };

    const merged = mergeConversationStates(local, incoming);
    expect(merged.conversations).toHaveLength(1);
    expect(merged.conversations[0].id).toBe("recovered");
    expect(merged.removed?.["recovered"]).toBeUndefined();

    // The state must remain loadable from storage.
    const parsed = parseConversationState(serializeConversationState(merged, Date.now()));
    expect(parsed?.conversations[0].id).toBe("recovered");
  });

  it("keeps messages written concurrently in the same chat", () => {
    const base = emptyState(1);
    const id = base.activeId;
    const tabA = appendEntry(base, id, { role: "user", text: "from A" }, 10);
    const tabB = appendEntry(base, id, { role: "user", text: "from B" }, 11);

    const merged = mergeConversationStates(tabA, tabB);
    const conversation = merged.conversations.find((item) => item.id === id);
    expect(conversation?.entries.map((entry) => entry.text)).toEqual(["from A", "from B"]);
  });

  it("merges a resolved flag from the other tab's copy", () => {
    const base = emptyState(1);
    const id = base.activeId;
    const withPreview = appendEntry(base, id, { role: "assistant", text: "preview" }, 10);
    const resolved = updateEntry(withPreview, id, 0, { resolved: true }, 11);

    const merged = mergeConversationStates(withPreview, resolved);
    const conversation = merged.conversations.find((item) => item.id === id);
    expect(conversation?.entries[0].resolved).toBe(true);
  });

  it("keeps tombstones when starting a new chat", () => {
    const { state, firstId } = stateWithTwoConversations();
    const closed = removeConversation(state, firstId, 10);
    const withMessage = appendEntry(closed, closed.activeId, { role: "user", text: "hello" }, 11);

    const next = startConversation(withMessage, 12);
    expect(next.removed?.[firstId]).toBe(10);
  });

  it("persists removal tombstones through storage", () => {
    const { state, firstId } = stateWithTwoConversations();
    const now = Date.now();
    const closed = removeConversation(state, firstId, now);

    const parsed = parseConversationState(serializeConversationState(closed, now));
    expect(parsed?.removed?.[firstId]).toBe(now);
    expect(parsed?.conversations.some((conversation) => conversation.id === firstId)).toBe(false);
  });

  it("writes identical payloads regardless of local conversation order", () => {
    const { state } = stateWithTwoConversations();
    const now = Date.now();
    const reversed: ConversationState = {
      ...state,
      conversations: [...state.conversations].reverse(),
    };

    expect(serializeConversationState(state, now)).toBe(serializeConversationState(reversed, now));
  });

  it("trims old entries but keeps the derived title", () => {
    let state = emptyState(0);
    const id = state.activeId;
    for (let i = 0; i < MAX_ENTRIES; i += 1) {
      state = appendEntry(state, id, { role: "user", text: i === 0 ? "original title" : `message ${i}` }, i);
    }
    state = appendEntry(state, id, { role: "assistant", text: "latest" }, MAX_ENTRIES);

    expect(state.conversations[0].entries).toHaveLength(MAX_ENTRIES);
    expect(state.conversations[0].entries[0].text).toBe("message 1");
    expect(state.conversations[0].title).toBe("original title");
  });

  it("selects and closes conversations without losing the active one", () => {
    const { state, firstId, secondId } = stateWithTwoConversations();

    const selected = selectConversation(state, firstId);
    expect(selected.activeId).toBe(firstId);
    expect(selectConversation(selected, "missing")).toBe(selected);

    const removedActive = removeConversation(selected, firstId);
    expect(removedActive.conversations).toHaveLength(1);
    expect(removedActive.activeId).toBe(secondId);

    const removedInactive = removeConversation(state, firstId);
    expect(removedInactive.conversations).toHaveLength(1);
    expect(removedInactive.activeId).toBe(secondId);
  });

  it("never removes the final conversation", () => {
    const base = emptyState(1);
    expect(removeConversation(base, base.activeId)).toBe(base);
  });

  it("falls back to the first conversation when the active id is missing", () => {
    const state = { ...emptyState(1), activeId: "missing" };
    expect(getActiveConversation(state).id).toBe(state.conversations[0].id);
  });
});

describe("assistant conversation persistence", () => {
  it("round-trips a conversation state", () => {
    const { state } = stateWithTwoConversations();
    expect(parseConversationState(serializeConversationState(state))).toEqual(state);
  });

  it("rejects invalid or outdated payloads", () => {
    expect(parseConversationState(null)).toBeNull();
    expect(parseConversationState("not json")).toBeNull();
    expect(parseConversationState(JSON.stringify({ version: 999, conversations: [], activeId: "" }))).toBeNull();
    expect(parseConversationState(JSON.stringify({ version: 1, conversations: [], activeId: "" }))).toBeNull();
  });

  it("drops invalid conversations and repairs a missing active id", () => {
    const valid = emptyState(1).conversations[0];
    const payload = JSON.stringify({
      version: 1,
      conversations: [valid, { id: "", title: "bad", createdAt: 0, updatedAt: 0, entries: [] }],
      activeId: "missing",
    });
    const parsed = parseConversationState(payload);
    expect(parsed?.conversations).toHaveLength(1);
    expect(parsed?.activeId).toBe(valid.id);
  });

  it("loads and saves through a storage adapter", () => {
    const store = memoryStorage();
    const key = "test-key";
    const { state } = stateWithTwoConversations();

    saveConversations(key, state, store);
    expect(loadConversations(key, store)).toEqual(state);

    expect(loadConversations("missing", store).conversations).toHaveLength(1);
  });

  it("falls back to an empty state when stored data is corrupt", () => {
    const store = memoryStorage();
    store.setItem("corrupt", "{not-json");
    expect(loadConversations("corrupt", store).conversations).toHaveLength(1);
  });

  it("scopes the storage key by user, then business, then anonymous", () => {
    const userToken = fakeToken({ user_id: "user-1", business_id: "business-1" });
    const businessToken = fakeToken({ business_id: "business-1" });
    const invalidToken = "not-a-jwt";

    expect(conversationScopeFromToken(userToken)).toBe("user-1");
    expect(conversationScopeFromToken(businessToken)).toBe("business-1");
    expect(conversationScopeFromToken(invalidToken)).toBe("anonymous");
    expect(conversationScopeFromToken(null)).toBe("anonymous");
    expect(conversationStorageKey(userToken)).toContain("v1:user-1");
  });
});
