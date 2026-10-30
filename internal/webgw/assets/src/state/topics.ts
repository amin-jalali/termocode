// The reducers of the list topics (web design 2.2–2.4): how one delta of
// toasts, chats, chat and cards folds into the topic's data. They mirror
// the hub's apply functions (internal/app/web_*.go) rule for rule, so
// apply(snap₀, deltas…) gives the hub's own snapₙ; TestStoreReplayMatchesGo
// checks that against the Go goldens. A delta that does not fit (an append
// to a message that is not streaming, an index out of range, an unknown
// op) throws, and the store marks the topic broken and asks for a snap.
//
// Pure: no DOM, timers or network; inputs are never changed.
import type {
  Card,
  CardsDelta,
  CardsSnap,
  ChatDelta,
  ChatItem,
  ChatMsg,
  ChatSnap,
  ChatsDelta,
  ChatsSnap,
  Toast,
  ToastsDelta,
  ToastsSnap,
} from "../../../../webproto/proto";
import type { Reducer, Store } from "./store";

/** MAX_CHATS is webproto.MaxChats: the chats topic keeps the newest 200. */
export const MAX_CHATS = 200;

export class DeltaError extends Error {}

function bad(why: string): never {
  throw new DeltaError(why);
}

function obj(v: unknown, what: string): Record<string, unknown> {
  if (typeof v !== "object" || v === null || Array.isArray(v)) bad(what + " is not an object");
  return v as Record<string, unknown>;
}

function arr<T>(v: unknown, what: string): T[] {
  if (v === undefined || v === null) return [];
  if (!Array.isArray(v)) bad(what + " is not a list");
  return v as T[];
}

function str(v: unknown): string {
  return typeof v === "string" ? v : "";
}

function int(v: unknown, what: string): number {
  if (typeof v !== "number" || !Number.isInteger(v)) bad(what + " is not a whole number");
  return v;
}

/** upsert replaces the item with the same id in place, or appends it. */
function upsert<T extends { id: unknown }>(list: readonly T[], item: T): T[] {
  const i = list.findIndex((x) => x.id === item.id);
  if (i < 0) return [...list, item];
  const out = list.slice();
  out[i] = item;
  return out;
}

/** toasts: push adds a toast or updates the one with its id in place; dismiss drops it. */
export const reduceToasts: Reducer = (data, delta) => {
  const s = obj(data, "toasts") as unknown as ToastsSnap;
  const d = obj(delta, "toasts delta") as unknown as ToastsDelta;
  const list = arr<Toast>(s.toasts, "toasts");
  switch (d.op) {
    case "push": {
      const toast = obj(d.toast, "toast") as unknown as Toast;
      return { ...s, toasts: upsert(list, toast) };
    }
    case "dismiss":
      return { ...s, toasts: list.filter((x) => x.id !== d.id) };
    default:
      return bad("toasts: unknown op " + String(d.op));
  }
};

/** sortChats orders chats newest first, then by id (webChatsSort). */
export function sortChats(list: ChatItem[]): ChatItem[] {
  return list.sort((a, b) => {
    const ua = typeof a.updated === "number" ? a.updated : 0;
    const ub = typeof b.updated === "number" ? b.updated : 0;
    if (ua !== ub) return ub - ua;
    return a.id < b.id ? -1 : a.id > b.id ? 1 : 0;
  });
}

/** chats: upsert puts a chat in (or over the one with its id); remove drops one. */
export const reduceChats: Reducer = (data, delta) => {
  const s = obj(data, "chats") as unknown as ChatsSnap;
  const d = obj(delta, "chats delta") as unknown as ChatsDelta;
  let id: string;
  let chat: ChatItem | undefined;
  switch (d.op) {
    case "upsert":
      chat = obj(d.chat, "chat") as unknown as ChatItem;
      id = str(chat.id);
      break;
    case "remove":
      id = str(d.id);
      break;
    default:
      return bad("chats: unknown op " + String(d.op));
  }
  const out = arr<ChatItem>(s.chats, "chats").filter((c) => c.id !== id);
  if (chat) out.push(chat);
  return { ...s, chats: sortChats(out).slice(0, MAX_CHATS) };
};

/** chat: the open chat. append grows a streaming answer; message adds or
 * replaces one; truncate keeps the first n; state sets busy and the
 * title; switched is a whole other chat. */
export const reduceChat: Reducer = (data, delta) => {
  const s = obj(data, "chat") as unknown as ChatSnap;
  const d = obj(delta, "chat delta") as unknown as ChatDelta;
  const msgs = arr<ChatMsg>(s.messages, "messages");
  switch (d.op) {
    case "append": {
      const i = int(d.i, "append index");
      const m = msgs[i];
      if (i < 0 || !m || m.streaming !== true) bad("chat: append to a message that is not streaming");
      const text = str(m.text) + str(d.text);
      const out = msgs.slice();
      out[i] = text ? { ...m, text } : m;
      return { ...s, messages: out };
    }
    case "message": {
      const m = obj(d.msg, "message") as unknown as ChatMsg;
      const i = int(m.i, "message index");
      if (i < 0 || i > msgs.length) bad("chat: message out of range");
      const out = msgs.slice();
      out[i] = m;
      return { ...s, messages: out };
    }
    case "truncate": {
      const n = int(d.n, "truncate count");
      if (n < 0 || n > msgs.length) bad("chat: truncate out of range");
      return { ...s, messages: msgs.slice(0, n) };
    }
    case "state": {
      const out = { ...s };
      if (typeof d.busy === "boolean") out.busy = d.busy;
      if (d.title) out.title = d.title;
      return out;
    }
    case "switched":
      return obj(d.chat, "switched chat");
    default:
      return bad("chat: unknown op " + String(d.op));
  }
};

/** cards: upsert adds a card or replaces the one with its id in place; remove drops one. */
export const reduceCards: Reducer = (data, delta) => {
  const s = obj(data, "cards") as unknown as CardsSnap;
  const d = obj(delta, "cards delta") as unknown as CardsDelta;
  const list = arr<Card>(s.cards, "cards");
  switch (d.op) {
    case "upsert": {
      const card = obj(d.card, "card") as unknown as Card;
      return { ...s, cards: upsert(list, card) };
    }
    case "remove":
      return { ...s, cards: list.filter((c) => c.id !== d.id) };
    default:
      return bad("cards: unknown op " + String(d.op));
  }
};

/** registerTopicReducers gives the store the reducer of every list topic
 * (status keeps the default: each delta is the whole value). */
export function registerTopicReducers(store: Store): void {
  store.reducer("toasts", reduceToasts);
  store.reducer("chats", reduceChats);
  store.reducer("chat", reduceChat);
  store.reducer("cards", reduceCards);
}
