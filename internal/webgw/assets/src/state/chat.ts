// The chat and chats topics as the chat view reads them (web design 2.2).
// Tolerant: a field of the wrong type reads as missing, so an older or
// newer hub never breaks the view. Pure.
import type { ChatItem, ChatMsg, ChatSnap } from "../../../../webproto/proto";

function isObj(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

/** readMsg returns one message, or null when it is not one. */
export function readMsg(v: unknown): ChatMsg | null {
  if (!isObj(v) || typeof v.i !== "number" || typeof v.role !== "string") return null;
  return v as unknown as ChatMsg;
}

/** readChat returns the open chat, or null before there is one. */
export function readChat(data: unknown): ChatSnap | null {
  if (!isObj(data)) return null;
  const msgs = Array.isArray(data.messages) ? data.messages : [];
  return {
    id: typeof data.id === "string" ? data.id : "",
    title: typeof data.title === "string" ? data.title : "",
    profile: typeof data.profile === "string" ? data.profile : undefined,
    model: typeof data.model === "string" ? data.model : undefined,
    busy: data.busy === true,
    messages: msgs.map(readMsg).filter((m): m is ChatMsg => m !== null),
  };
}

/** readChats returns the chat list, newest first. */
export function readChats(data: unknown): ChatItem[] {
  if (!isObj(data) || !Array.isArray(data.chats)) return [];
  return data.chats.filter((c): c is ChatItem => isObj(c) && typeof c.id === "string");
}

/** isStub: an older message sent without its text (fetch it with chat.get_message). */
export function isStub(m: ChatMsg): boolean {
  return m.truncated === true && !m.text && !m.md && !(m.steps && m.steps.length);
}

/** safeHref returns href when it is an http or https URL, else null: any
 * other link shows as its text only (never-do 20). */
export function safeHref(href: unknown): string | null {
  if (typeof href !== "string") return null;
  const h = href.trim();
  return /^https?:\/\/[^\s"'<>]+$/i.test(h) ? h : null;
}
