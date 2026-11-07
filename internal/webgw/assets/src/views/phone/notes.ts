// The review notes the user wrote on the phone (MOB-14), per agent
// worktree, kept on this page until they are sent (ai.worktree_notes)
// or the worktree is merged or discarded. Notes are the user's own text
// and never leave the page before Send.
import { signal } from "@preact/signals";
import type { ReviewNote } from "../../../../../webproto/proto";

/** MAX_NOTES is webproto.MaxReviewNotes: the most one send carries. */
export const MAX_NOTES = 30;

/** notes holds each worktree's notes by its id. */
export const notes = signal<ReadonlyMap<string, readonly ReviewNote[]>>(new Map());

export function notesOf(wt: string): readonly ReviewNote[] {
  return notes.value.get(wt) ?? [];
}

function put(wt: string, list: readonly ReviewNote[]): void {
  const m = new Map(notes.peek());
  if (list.length === 0) m.delete(wt);
  else m.set(wt, list);
  notes.value = m;
}

/** addNote adds n (a note on the same line replaces the old one); false
 * when the worktree already has MAX_NOTES notes. */
export function addNote(wt: string, n: ReviewNote): boolean {
  const old = notes.peek().get(wt) ?? [];
  const rest = old.filter((o) => !(o.path === n.path && o.line === n.line && o.side === n.side));
  if (rest.length >= MAX_NOTES) return false;
  put(wt, [...rest, n].sort((a, b) => (a.path === b.path ? a.line - b.line : a.path < b.path ? -1 : 1)));
  return true;
}

export function removeNote(wt: string, i: number): void {
  const old = notes.peek().get(wt) ?? [];
  put(wt, old.filter((_, k) => k !== i));
}

export function clearNotes(wt: string): void {
  put(wt, []);
}
