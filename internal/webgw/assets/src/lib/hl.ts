// Syntax colours of the file view (WEB-38): the spans nvim's tree-sitter
// found (buf.hl) for the rows on screen, painted as classes that map to
// theme tokens (styles/hl.css). The page never parses code. Spans belong
// to one changedtick; an answer for another tick waits for the next one.
// A row keeps its colours while its text is the text they were made for,
// so a shifted row borrows the colours of its own text until the next
// answer, and a changed row is plain for a moment, never wrongly coloured.
import { signal } from "@preact/signals";
import type { HlRes } from "../../../../webproto/proto";
import { link } from "../session";

/** Seg is a run of one row: its text and its colour class ("" plain). */
export interface Seg {
  text: string;
  cls: string;
}

/** MAX_ROW: longer rows stay plain (a minified file, a data line). */
export const MAX_ROW = 4000;
/** DEBOUNCE_MS: edits and scrolls ask once they settle. */
const DEBOUNCE_MS = 200;
/** MAX_ROWS is webproto.MaxHlRows. */
const MAX_ROWS = 2000;

// Capture names (nvim's standard groups) to classes; the first prefix that
// matches wins, so the specific ones come first. Names not listed (a
// variable, a property) stay plain text.
const CLASSES: readonly (readonly [string, string])[] = [
  ["comment", "c"],
  ["markup.heading", "h"],
  ["markup.strong", "b"],
  ["markup.italic", "i"],
  ["markup.strikethrough", "x"],
  ["markup.link", "u"],
  ["markup.raw", "s"],
  ["markup.quote", "c"],
  ["markup.list", "n"],
  ["string.special.url", "u"],
  ["string.escape", "e"],
  ["string.regexp", "e"],
  ["string", "s"],
  ["character", "s"],
  ["number", "n"],
  ["float", "n"],
  ["boolean", "n"],
  ["constant.builtin", "n"],
  ["constant", "n"],
  ["keyword", "k"],
  ["conditional", "k"],
  ["repeat", "k"],
  ["include", "k"],
  ["exception", "k"],
  ["function", "f"],
  ["method", "f"],
  ["constructor", "t"],
  ["type", "t"],
  ["module", "t"],
  ["namespace", "t"],
  ["attribute", "t"],
  ["label", "t"],
  ["tag.attribute", "t"],
  ["tag", "g"],
  ["variable.builtin", "g"],
  ["operator", "o"],
  ["punctuation.special", "o"],
  ["diff.plus", "da"],
  ["diff.minus", "dd"],
  ["diff.delta", "dm"],
  ["error", "r"],
];

/** hlClass is the class of a capture name, or "" (plain). */
export function hlClass(group: string): string {
  for (const [p, c] of CLASSES) if (group === p || group.startsWith(p + ".")) return c;
  return "";
}

/** paint cuts text into runs by spans [c0, c1, class] (UTF-16 columns);
 * a later span paints over an earlier one, as nvim draws them. */
export function paint(text: string, spans: readonly (readonly [number, number, string])[]): Seg[] {
  if (spans.length === 0 || text.length > MAX_ROW) return [{ text, cls: "" }];
  const cls: string[] = new Array<string>(text.length).fill("");
  for (const [a, b, c] of spans) {
    if (!c) continue;
    for (let i = Math.max(0, a); i < Math.min(b, text.length); i++) cls[i] = c;
  }
  const out: Seg[] = [];
  let start = 0;
  for (let i = 1; i <= text.length; i++) {
    if (i === text.length || cls[i] !== cls[start]) {
      out.push({ text: text.slice(start, i), cls: cls[start] });
      start = i;
    }
  }
  return out.length ? out : [{ text, cls: "" }];
}

interface Row {
  text: string;
  segs: Seg[];
}

/** HlState is what the file view paints from. */
export interface HlState {
  buf: number;
  tick: number;
  rows: Map<number, Row>;
  /** byText: the runs of each text of the last answer (a moved row). */
  byText: Map<string, Seg[]>;
  none: boolean;
}

export const hl = signal<HlState | null>(null);

/** rowSegs is the runs of row i with text, or null (plain). */
export function rowSegs(st: HlState | null, buf: number, i: number, text: string): Seg[] | null {
  if (!st || st.buf !== buf || st.none) return null;
  const r = st.rows.get(i);
  if (r && r.text === text) return r.segs;
  return st.byText.get(text) ?? null;
}

/** fold takes an answer for the page's lines at tick into the state. */
export function fold(prev: HlState | null, buf: number, lines: readonly string[], res: HlRes): HlState {
  const keep = prev && prev.buf === buf ? prev.rows : new Map<number, Row>();
  const rows = new Map<number, Row>();
  for (const [i, r] of keep) if (lines[i] === r.text) rows.set(i, r);
  const per = new Map<number, [number, number, string][]>();
  for (const s of res.spans) {
    if (s.length !== 4) continue;
    const [row, a, b, g] = s;
    const c = hlClass(res.groups[g] ?? "");
    if (!c) continue;
    let l = per.get(row);
    if (!l) per.set(row, (l = []));
    l.push([a, b, c]);
  }
  const byText = new Map<string, Seg[]>();
  for (let i = res.first; i < res.last && i < lines.length; i++) {
    const segs = paint(lines[i], per.get(i) ?? []);
    rows.set(i, { text: lines[i], segs });
    if (segs.length > 1 || segs[0]?.cls) byText.set(lines[i], segs);
  }
  return { buf, tick: res.tick, rows, byText, none: res.none === true };
}

// One wish at a time: the newest window and tick the view shows.
let timer: ReturnType<typeof setTimeout> | undefined;
let asked = "";
let busy = false;
let next: { buf: number; tick: number; first: number; last: number; lines: () => readonly string[]; tickNow: () => number } | null = null;

/** wantHl asks the colours of rows [first, last) of buf at tick, once the
 * view settles. lines and tickNow read the page's copy when the answer
 * comes. */
export function wantHl(buf: number, tick: number, first: number, last: number, lines: () => readonly string[], tickNow: () => number): void {
  last = Math.min(last, first + MAX_ROWS);
  const key = `${buf}:${tick}:${first}:${last}`;
  const st = hl.peek();
  if (key === asked || (st && st.buf === buf && st.none && st.tick === tick)) return;
  next = { buf, tick, first, last, lines, tickNow };
  clearTimeout(timer);
  timer = setTimeout(ask, DEBOUNCE_MS);
}

function ask(): void {
  if (busy || !next) return;
  const w = next;
  next = null;
  asked = `${w.buf}:${w.tick}:${w.first}:${w.last}`;
  busy = true;
  link.request("buf.hl", { buf: w.buf, first: w.first, last: w.last }).then(
    (data) => {
      busy = false;
      const res = data as HlRes;
      // Spans of the tick the page shows only; else the next tick asks again.
      if (res.tick === w.tickNow()) hl.value = fold(hl.peek(), w.buf, w.lines(), res);
      else asked = "";
      if (next) ask();
    },
    () => {
      busy = false;
      if (next) ask();
    },
  );
}

/** dropHl forgets the colours (the view closed). */
export function dropHl(): void {
  clearTimeout(timer);
  next = null;
  asked = "";
  hl.value = null;
}
