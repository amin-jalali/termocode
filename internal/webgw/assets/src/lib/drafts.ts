// Chat drafts, one per chat, kept in memory for the life of the page, so
// a lost link, a tab change or a layout change never loses typed text
// (web UX P5). Nothing here is sent anywhere.
import { signal } from "@preact/signals";

export const drafts = signal<Readonly<Record<string, string>>>({});

export function getDraft(chat: string): string {
  return drafts.value[chat] ?? "";
}

export function setDraft(chat: string, text: string): void {
  const next = { ...drafts.peek() };
  if (text) next[chat] = text;
  else delete next[chat];
  drafts.value = next;
}
