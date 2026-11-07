// Vim's jump list (WEBP-1), kept by the page so it spans files: each
// editor's frame tells where a jump left from, and leaving a file for
// another one is a jump too. Ctrl+O and Ctrl+I in a frame walk the list;
// a place in another file opens that file. Places are 1-based.
import { effect } from "@preact/signals";
import { activeFile } from "./workbench";

export interface JumpAt {
  path: string;
  line: number;
  col: number;
}

/** JUMP_MAX is how many places the list keeps (Vim's 100). */
const JUMP_MAX = 100;

let list: JumpAt[] = [];
/** idx is where Ctrl+O and Ctrl+I stand (list.length: past the end). */
let idx = 0;
/** carets are the last caret of each file (the place a file is left at). */
const carets = new Map<string, { line: number; col: number }>();
/** travel is the file a Ctrl+O or Ctrl+I is opening (leaving for it is
 * not a new jump). */
let travel: string | null = null;

/** noteJump adds a place a jump leaves; one place per line is kept. */
export function noteJump(path: string, line: number, col: number): void {
  list = list.filter((j) => j.path !== path || j.line !== line);
  list.push({ path, line, col });
  if (list.length > JUMP_MAX) list = list.slice(list.length - JUMP_MAX);
  idx = list.length;
}

/** noteCaret remembers a file's caret. */
export function noteCaret(path: string, line: number, col: number): void {
  carets.set(path, { line, col });
}

/** jumpGo walks n places back (dir -1) or forward (1) from the caret at
 * path; null: the list ends there (nothing moves). */
export function jumpGo(path: string, line: number, col: number, dir: 1 | -1, n: number): JumpAt | null {
  if (dir < 0 && idx >= list.length) {
    // Leaving the list's end keeps the caret, for Ctrl+I to come back.
    noteJump(path, line, col);
    idx = list.length - 1;
  }
  const to = idx + dir * Math.max(1, n);
  if (to < 0 || to >= list.length) return null;
  idx = to;
  const at = list[to];
  if (at.path !== path) travel = at.path;
  return at;
}

// Going to another file leaves a jump at the caret of the file left.
let shown: string | null = null;
effect(() => {
  const f = activeFile.value;
  const was = shown;
  shown = f;
  if (!was || was === f) return;
  const going = travel;
  travel = null;
  if (going === f) return;
  const c = carets.get(was);
  if (c) noteJump(was, c.line, c.col);
});
