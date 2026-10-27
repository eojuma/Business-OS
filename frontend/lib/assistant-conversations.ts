export interface Preview {
  Kind: "sale" | "answer";
  Understood: boolean;
  ProductID: string;
  ProductName: string;
  Quantity: number;
  UnitPrice: number; // cents
  TotalAmount: number; // cents
  Message: string;
}

export interface ChatEntry {
  role: "user" | "assistant";
  text: string;
  preview?: Preview; // only set on assistant entries that are awaiting confirmation
  resolved?: boolean; // true once confirmed or dismissed — hides the buttons
}

export interface Conversation {
  id: string;
  title: string;
  createdAt: number;
  updatedAt: number;
  entries: ChatEntry[];
}

export interface ConversationState {
  conversations: Conversation[];
  activeId: string;
}

export interface StorageLike {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
}

export const STORAGE_VERSION = 1;
export const MAX_CONVERSATIONS = 30;
export const MAX_ENTRIES = 200;
export const NEW_CHAT_TITLE = "New chat";
const TITLE_LIMIT = 42;
const STORAGE_PREFIX = "business-os:assistant:conversations";

function browserStorage(): StorageLike | undefined {
  if (typeof window === "undefined") return undefined;
  try {
    return window.localStorage;
  } catch {
    return undefined;
  }
}

// readStoredValue mirrors the guarded access used by loadConversations so a
// browser that throws on localStorage access degrades to an in-memory session
// instead of taking the page down.
export function readStoredValue(
  key: string,
  store: StorageLike | undefined = browserStorage(),
): string | null {
  if (!store) return null;
  try {
    return store.getItem(key);
  } catch {
    return null;
  }
}

export function newId(): string {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
    return crypto.randomUUID();
  }
  return `conversation-${Date.now()}-${Math.random().toString(36).slice(2)}`;
}

export function createConversation(now = Date.now()): Conversation {
  return { id: newId(), title: NEW_CHAT_TITLE, createdAt: now, updatedAt: now, entries: [] };
}

// Deterministic state for the first render so SSR and hydration match. The
// hook replaces it with the stored state after mount.
export function initialConversationState(): ConversationState {
  const conversation: Conversation = {
    id: "initial",
    title: NEW_CHAT_TITLE,
    createdAt: 0,
    updatedAt: 0,
    entries: [],
  };
  return { conversations: [conversation], activeId: conversation.id };
}

export function emptyState(now = Date.now()): ConversationState {
  const conversation = createConversation(now);
  return { conversations: [conversation], activeId: conversation.id };
}

export function deriveTitle(entries: ChatEntry[]): string {
  const firstUserMessage = entries.find(
    (entry) => entry.role === "user" && entry.text.trim().length > 0,
  );
  if (!firstUserMessage) return NEW_CHAT_TITLE;
  const text = firstUserMessage.text.trim().replace(/\s+/g, " ");
  return text.length > TITLE_LIMIT ? `${text.slice(0, TITLE_LIMIT).trimEnd()}…` : text;
}

export function getActiveConversation(state: ConversationState): Conversation {
  return (
    state.conversations.find((conversation) => conversation.id === state.activeId) ??
    state.conversations[0]
  );
}

export function appendEntry(
  state: ConversationState,
  conversationId: string,
  entry: ChatEntry,
  now = Date.now(),
): ConversationState {
  let changed = false;
  const conversations = state.conversations.map((conversation) => {
    if (conversation.id !== conversationId) return conversation;
    changed = true;
    const combined = [...conversation.entries, entry];
    const entries = combined.slice(-MAX_ENTRIES);
    const title =
      conversation.title === NEW_CHAT_TITLE && entry.role === "user" && entry.text.trim()
        ? deriveTitle(combined)
        : conversation.title;
    return { ...conversation, entries, title, updatedAt: now };
  });
  return changed ? { ...state, conversations } : state;
}

export function updateEntry(
  state: ConversationState,
  conversationId: string,
  index: number,
  patch: Partial<Pick<ChatEntry, "resolved">>,
  now = Date.now(),
): ConversationState {
  let changed = false;
  const conversations = state.conversations.map((conversation) => {
    if (
      conversation.id !== conversationId ||
      index < 0 ||
      index >= conversation.entries.length
    ) {
      return conversation;
    }
    changed = true;
    const entries = conversation.entries.map((entry, entryIndex) =>
      entryIndex === index ? { ...entry, ...patch } : entry,
    );
    return { ...conversation, entries, updatedAt: now };
  });
  return changed ? { ...state, conversations } : state;
}

export function startConversation(state: ConversationState, now = Date.now()): ConversationState {
  const active = getActiveConversation(state);
  // Reuse an untouched draft instead of stacking empty "New chat" tabs.
  if (active && active.entries.length === 0) return state;

  const conversation = createConversation(now);
  return {
    conversations: [conversation, ...state.conversations].slice(0, MAX_CONVERSATIONS),
    activeId: conversation.id,
  };
}

export function selectConversation(state: ConversationState, id: string): ConversationState {
  if (
    state.activeId === id ||
    !state.conversations.some((conversation) => conversation.id === id)
  ) {
    return state;
  }
  return { ...state, activeId: id };
}

export function removeConversation(state: ConversationState, id: string): ConversationState {
  if (state.conversations.length <= 1) return state;
  const index = state.conversations.findIndex((conversation) => conversation.id === id);
  if (index === -1) return state;

  const conversations = state.conversations.filter((conversation) => conversation.id !== id);
  if (state.activeId !== id) {
    return { ...state, conversations };
  }
  const next = conversations[Math.min(index, conversations.length - 1)];
  return { conversations, activeId: next.id };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function isChatEntry(value: unknown): value is ChatEntry {
  if (!isRecord(value)) return false;
  if (value.role !== "user" && value.role !== "assistant") return false;
  if (typeof value.text !== "string") return false;
  return value.preview === undefined || isRecord(value.preview);
}

function isConversation(value: unknown): value is Conversation {
  if (!isRecord(value)) return false;
  if (typeof value.id !== "string" || value.id.length === 0) return false;
  if (typeof value.title !== "string") return false;
  if (typeof value.createdAt !== "number" || typeof value.updatedAt !== "number") return false;
  return Array.isArray(value.entries) && value.entries.every(isChatEntry);
}

export function serializeConversationState(state: ConversationState): string {
  return JSON.stringify({
    version: STORAGE_VERSION,
    conversations: state.conversations,
    activeId: state.activeId,
  });
}

export function parseConversationState(raw: string | null): ConversationState | null {
  if (!raw) return null;
  try {
    const parsed: unknown = JSON.parse(raw);
    if (!isRecord(parsed) || parsed.version !== STORAGE_VERSION) return null;
    if (!Array.isArray(parsed.conversations) || typeof parsed.activeId !== "string") {
      return null;
    }

    const conversations = parsed.conversations.filter(isConversation);
    if (conversations.length === 0) return null;

    const activeId = conversations.some((conversation) => conversation.id === parsed.activeId)
      ? parsed.activeId
      : conversations[0].id;
    return { conversations, activeId };
  } catch {
    return null;
  }
}

export function decodeJwtPayload(token: string | null): Record<string, unknown> | null {
  if (!token) return null;
  try {
    const payload = token.split(".")[1];
    if (!payload) return null;
    const base64 = payload.replace(/-/g, "+").replace(/_/g, "/");
    const padded = base64.padEnd(base64.length + ((4 - (base64.length % 4)) % 4), "=");
    const bytes = Uint8Array.from(atob(padded), (character) => character.charCodeAt(0));
    const parsed: unknown = JSON.parse(new TextDecoder().decode(bytes));
    return isRecord(parsed) ? parsed : null;
  } catch {
    return null;
  }
}

export function conversationScopeFromToken(token: string | null): string {
  const payload = decodeJwtPayload(token);
  const candidate = payload?.user_id ?? payload?.business_id;
  return typeof candidate === "string" && candidate.length > 0 ? candidate : "anonymous";
}

export function conversationStorageKey(token: string | null): string {
  return `${STORAGE_PREFIX}:v${STORAGE_VERSION}:${conversationScopeFromToken(token)}`;
}

export function loadConversations(
  key: string,
  store: StorageLike | undefined = browserStorage(),
): ConversationState {
  if (!store) return emptyState();
  try {
    return parseConversationState(store.getItem(key)) ?? emptyState();
  } catch {
    return emptyState();
  }
}

export function saveConversations(
  key: string,
  state: ConversationState,
  store: StorageLike | undefined = browserStorage(),
): void {
  if (!store) return;
  try {
    store.setItem(key, serializeConversationState(state));
  } catch {
    // Storage can be unavailable (private mode) or full; keep the in-memory state.
  }
}
