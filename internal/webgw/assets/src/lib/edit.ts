// The edit sheet's draft (WEB-22, web UX 3.4): a range of whole rows of
// one buffer, the text the user types, and the rows as they were when the
// sheet opened. Send is one buf.set_text with the tick of those rows, so
// a change the laptop made meanwhile is a conflict, never overwritten:
// the sheet keeps the user's text and shows what changed. The draft lives
// here, not in the sheet, so a lost link or a re-render never loses it.
import { signal } from "@preact/signals";
import type { BufSnap, EditRes } from "../../../../webproto/proto";
import { OfflineError, ReqError } from "../net/link";
import { link } from "../session";

/** CONTEXT rows above and below the selection come into the sheet. */
export const CONTEXT = 20;

export interface Draft {
  path: string;
  buf: number;
  /** rows [a, end) of the buffer, as they were (base) at tick. */
  a: number;
  end: number;
  base: string[];
  tick: number;
  /** total is the buffer's line count at tick. */
  total: number;
  text: string;
  sending: boolean;
  /** conflict: the laptop changed these rows since the sheet opened. */
  conflict?: boolean;
  note?: string;
}

export const draft = signal<Draft | null>(null);

/** selection is the rows picked in the file view, [a, b] inclusive. */
export const selection = signal<{ a: number; b: number } | null>(null);

/** saveErr is the editor's own words after a failed save, under the header. */
export const saveErr = signal<string | null>(null);

/** own is the newest tick this page's own edit, undo or save produced:
 * rows that change up to it are not "updated from the laptop". */
export const own = { tick: 0 };

/** tapRow selects a row; a tap on another row stretches the selection,
 * a tap on the one selected row clears it. */
export function tapRow(i: number): void {
  const s = selection.peek();
  if (!s) selection.value = { a: i, b: i };
  else if (s.a === i && s.b === i) selection.value = null;
  else selection.value = { a: Math.min(s.a, i), b: Math.max(s.b, i) };
}

function bytes(s: string): number {
  return new TextEncoder().encode(s).length;
}

/** startEdit opens the sheet on rows [selA, selB] plus CONTEXT around. */
export function startEdit(path: string, buf: BufSnap, selA: number, selB: number): void {
  const n = buf.lines.length;
  const a = Math.max(0, Math.min(selA, n) - CONTEXT);
  const end = Math.min(n, Math.max(selB, selA) + 1 + CONTEXT);
  const base = buf.lines.slice(a, end);
  draft.value = { path, buf: buf.buf, a, end, base, tick: buf.tick, total: n, text: base.join("\n"), sending: false };
}

export function setText(text: string): void {
  const d = draft.peek();
  if (d) draft.value = { ...d, text, note: undefined };
}

export function cancelEdit(): void {
  draft.value = null;
}

function same(a: readonly string[], b: readonly string[]): boolean {
  return a.length === b.length && a.every((x, i) => x === b[i]);
}

/** rebase takes the laptop's newer text as the base: Send again. */
function rebase(d: Draft, buf: BufSnap): Draft {
  const end = Math.min(d.end, buf.lines.length);
  return { ...d, end, base: buf.lines.slice(d.a, end), tick: buf.tick, total: buf.lines.length, conflict: false };
}

export type SendResult = { ok: true; a: number; b: number } | { ok: false };

/** changedRows is the first and last row (buffer rows) the new text changes. */
function changedRows(d: Draft, lines: readonly string[]): { a: number; b: number } {
  let i = 0;
  while (i < lines.length && i < d.base.length && lines[i] === d.base[i]) i++;
  let pe = d.base.length - 1, ne = lines.length - 1;
  while (pe >= i && ne >= i && d.base[pe] === lines[ne]) {
    pe--;
    ne--;
  }
  return { a: d.a + i, b: d.a + Math.max(i, ne) };
}

/** send sends the draft against buf (the page's newest copy). again
 * rebases on the laptop's text first (after the user saw the conflict). */
export function send(buf: BufSnap, again = false): Promise<SendResult> {
  let d = draft.peek();
  if (!d || d.sending) return Promise.resolve({ ok: false });
  if (again) d = rebase(d, buf);
  if (buf.tick !== d.tick) {
    // The laptop changed something: fine only when it was not these rows.
    const end = Math.min(d.end, buf.lines.length);
    if (buf.lines.length !== d.total || !same(buf.lines.slice(d.a, end), d.base)) {
      draft.value = { ...d, conflict: true };
      return Promise.resolve({ ok: false });
    }
    d = { ...d, tick: buf.tick };
  }
  const lines = d.text.split("\n");
  const last = d.end - 1;
  const args =
    d.end > d.a
      ? { buf: d.buf, tick: d.tick, start: { row: d.a, col: 0 }, end: { row: last, col: bytes(buf.lines[last] ?? "") }, lines }
      : { buf: d.buf, tick: d.tick, start: { row: 0, col: 0 }, end: { row: 0, col: 0 }, lines };
  const rows = changedRows(d, lines);
  const sent = d;
  draft.value = { ...d, sending: true, conflict: false, note: undefined };
  return link.request("buf.set_text", args).then(
    (res) => {
      own.tick = Math.max(own.tick, (res as EditRes).tick);
      if (draft.peek()?.buf === sent.buf) draft.value = null;
      selection.value = null;
      return { ok: true as const, ...rows };
    },
    (e: unknown) => {
      const cur = draft.peek();
      if (!cur) return { ok: false as const };
      if (e instanceof ReqError && e.code === "conflict") {
        // The page has the laptop's lines (they came as deltas) or will soon.
        draft.value = { ...cur, sending: false, conflict: true };
        return { ok: false as const };
      }
      draft.value = { ...cur, sending: false, note: e instanceof OfflineError ? undefined : e instanceof Error ? e.message : "" };
      return { ok: false as const };
    },
  );
}

/** save writes the buffer the way :write on the laptop does (its on-save
 * hooks run); a failure keeps the editor's message for the header. */
export function save(buf: number): Promise<boolean> {
  saveErr.value = null;
  return link.request("buf.save", { buf }).then(
    (res) => {
      own.tick = Math.max(own.tick, (res as EditRes).tick);
      return true;
    },
    (e: unknown) => {
      if (!(e instanceof OfflineError)) saveErr.value = e instanceof Error ? e.message : "";
      return false;
    },
  );
}

/** undo undoes one step of the buffer (the laptop's too, if it was last). */
export function undo(buf: number): Promise<string | null> {
  return link.request("buf.undo", { buf }).then(
    (res) => {
      own.tick = Math.max(own.tick, (res as EditRes).tick);
      return null;
    },
    (e: unknown) => (e instanceof OfflineError ? null : e instanceof Error ? e.message : ""),
  );
}
