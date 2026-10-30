// Terminal rows as the terminal view reads them (web design 2.5). The
// laptop's terminal wraps long output at its own width, so one long line
// arrives as several full rows. reflow joins them back into one line,
// which the page then wraps at the phone's width. Pure.
import type { TermItem, TermSnap } from "../../../../webproto/proto";

export interface Line {
  /** row is the buffer row of the line's first row. */
  row: number;
  text: string;
}

/** cells counts the columns a row takes (one per character; wide
 * characters count one, so such a row may not join: it then just wraps). */
function cells(s: string): number {
  let n = 0;
  for (const _ of s) n++;
  return n;
}

/** reflow joins each row that fills the laptop's width (cols) with the
 * row after it. first is the buffer row of rows[0]. */
export function reflow(rows: readonly string[], first: number, cols: number): Line[] {
  const out: Line[] = [];
  let cur: Line | null = null;
  for (let i = 0; i < rows.length; i++) {
    const r = rows[i];
    if (cur) cur.text += r;
    else cur = { row: first + i, text: r };
    if (!(cols > 0 && cells(r) >= cols)) {
      out.push(cur);
      cur = null;
    }
  }
  if (cur) out.push(cur);
  return out;
}

/** readTerm returns a terminal snapshot, or null when it is not one. */
export function readTerm(data: unknown): TermSnap | null {
  if (typeof data !== "object" || data === null) return null;
  const d = data as TermSnap;
  if (!Array.isArray(d.rows) || typeof d.first !== "number") return null;
  return d;
}

/** readTerms returns the shells of the terms topic, tab by tab. */
export function readTerms(data: unknown): TermItem[] {
  const v = typeof data === "object" && data !== null ? (data as Record<string, unknown>).terms : undefined;
  if (!Array.isArray(v)) return [];
  return v.filter((x): x is TermItem => typeof x === "object" && x !== null && typeof (x as TermItem).buf === "number");
}
