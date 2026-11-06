// Chat drafts, one per chat, kept in memory for the life of the page, so
// a lost link, a tab change or a layout change never loses typed text
// (web UX P5). Nothing here is sent anywhere. The last draft is also kept
// on this device across a reload, unless it looks like a secret (prefs).
import { signal } from "@preact/signals";
import { loadDraft, saveDraft } from "./prefs";

const stored = loadDraft(Date.now());

export const drafts = signal<Readonly<Record<string, string>>>(stored ? { [stored.chat]: stored.text } : {});

export function getDraft(chat: string): string {
  return drafts.value[chat] ?? "";
}

export function setDraft(chat: string, text: string): void {
  const next = { ...drafts.peek() };
  if (text) next[chat] = text;
  else delete next[chat];
  drafts.value = next;
  saveDraft(chat, text, Date.now());
}

/** pendingDraft is text a command put aside for the chat input (Git: Fix
 * CI with AI): the composer takes it as the draft of the first chat it
 * shows that is not notChat (the chat that was open when a new one was
 * asked for). Nothing is sent. */
export const pendingDraft = signal<{ text: string; notChat: string } | null>(null);

/** takePending gives chat the pending text, once. */
export function takePending(chat: string): void {
  const p = pendingDraft.peek();
  if (!p || !chat || chat === p.notChat) return;
  pendingDraft.value = null;
  setDraft(chat, p.text);
}
