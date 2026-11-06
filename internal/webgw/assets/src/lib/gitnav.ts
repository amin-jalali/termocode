// Git moves in the desktop editor (the laptop's ]c / [c and ]x / [x):
// the next or previous git change of the file (its gutter marks) and
// the next or previous merge conflict ("<<<<<<<" rows); the merge
// editor's own Next and Previous Conflict and Accept Base, by editor
// group; and source control's tree or flat list. Pure, but for the
// signals the views share.
import { signal } from "@preact/signals";

/** nextRow is the row of rows after line (dir 1) or before it (-1),
 * going round at the end; 0: there is none. rows are 1-based. */
export function nextRow(rows: readonly number[], line: number, dir: 1 | -1): number {
  const s = [...new Set(rows)].filter((r) => r > 0).sort((a, b) => a - b);
  if (s.length === 0) return 0;
  if (dir === 1) return s.find((r) => r > line) ?? s[0];
  for (let i = s.length - 1; i >= 0; i--) if (s[i] < line) return s[i];
  return s[s.length - 1];
}

/** conflictRows are the 1-based rows of text that start a conflict. */
export function conflictRows(text: string): number[] {
  const out: number[] = [];
  text.split("\n").forEach((l, i) => {
    if (/^<{7}( |$)/.test(l.replace(/\r$/, ""))) out.push(i + 1);
  });
  return out;
}

/** MergeActs are a merge editor's moves, for the commands. */
export interface MergeActs {
  go(dir: 1 | -1): void;
  /** base takes the common ancestor's text (false: the file has none). */
  base(): boolean;
}

/** mergeActs are the merge editors on screen, by editor group slot. */
export const mergeActs = signal<ReadonlyMap<string, MergeActs>>(new Map());

/** setMergeActs puts (or with null, takes) the merge editor of a slot. */
export function setMergeActs(slot: string, a: MergeActs | null): void {
  const next = new Map(mergeActs.peek());
  if (a) next.set(slot, a);
  else next.delete(slot);
  mergeActs.value = next;
}

/** scmTree: source control groups the changed files under their folders. */
export const scmTree = signal(false);
