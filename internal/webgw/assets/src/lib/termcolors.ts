// Terminal colours (WEB-40), best effort: the hub reads them off the
// laptop's screen for the rows the shell's window shows there
// (term.colors). They are drawn only on rows whose text is still the text
// they were read for, with colours moved until they are readable on this
// page's background. Never escape sequences: styles are data, applied as
// properties.
import { signal } from "@preact/signals";
import type { TermColorsRes, TermStyle } from "../../../../webproto/proto";
import { link } from "../session";
import { contrast, readable } from "../state/theme";

/** TermSeg is a run of a terminal line and its look (none: plain). */
export interface TermSeg {
  text: string;
  look?: Look;
}

/** Look is a style ready for the DOM. */
export type Look = {
  color?: string;
  background?: string;
  fontWeight?: string;
  fontStyle?: string;
  textDecoration?: string;
};

interface Row {
  text: string;
  spans: [number, number, number][];
}

export interface ColorsState {
  buf: number;
  tick: number;
  shown: boolean;
  rows: Map<number, Row>;
  styles: TermStyle[];
}

/** colorsOn: the user's switch (About sheet); on by default. */
export const colorsOn = signal(true);
export const termColors = signal<ColorsState | null>(null);

const DEBOUNCE_MS = 300;
const HEX = /^#[0-9a-f]{6}$/;

let timer: ReturnType<typeof setTimeout> | undefined;
let busy = false;
let next: { buf: number; tick: number; first: number; rows: () => { first: number; rows: readonly string[]; tick: number } | null } | null = null;
let asked = "";

/** wantColors asks the colours of buf at tick once output settles. */
export function wantColors(buf: number, tick: number, first: number, rows: () => { first: number; rows: readonly string[]; tick: number } | null): void {
  const key = `${buf}:${tick}`;
  if (key === asked || !colorsOn.peek()) return;
  next = { buf, tick, first, rows };
  clearTimeout(timer);
  timer = setTimeout(ask, DEBOUNCE_MS);
}

function ask(): void {
  if (busy || !next) return;
  const w = next;
  next = null;
  asked = `${w.buf}:${w.tick}`;
  busy = true;
  link.request("term.colors", { buf: w.buf }).then(
    (data) => {
      busy = false;
      const res = data as TermColorsRes;
      const now = w.rows();
      if (now && res.tick === now.tick) termColors.value = fold(w.buf, now.first, now.rows, res);
      else asked = "";
      if (next) ask();
    },
    () => {
      busy = false;
      if (next) ask();
    },
  );
}

/** fold keeps the spans of rows the page has, with their text. */
export function fold(buf: number, first: number, rows: readonly string[], res: TermColorsRes): ColorsState {
  const out = new Map<number, Row>();
  for (const s of res.spans) {
    if (s.length !== 4) continue;
    const [row, a, b, st] = s;
    const text = rows[row - first];
    if (text === undefined || !res.styles[st]) continue;
    let r = out.get(row);
    if (!r) out.set(row, (r = { text, spans: [] }));
    r.spans.push([a, b, st]);
  }
  return { buf, tick: res.tick, shown: res.shown, rows: out, styles: res.styles };
}

/** dropColors forgets them (the view closed or the switch went off). */
export function dropColors(): void {
  clearTimeout(timer);
  next = null;
  asked = "";
  termColors.value = null;
}

const looks = new Map<string, Look>();

/** look turns a style into DOM properties, readable on bg (the page's). */
export function look(st: TermStyle, bg: string): Look {
  const key = JSON.stringify(st) + bg;
  const hit = looks.get(key);
  if (hit) return hit;
  const out: Look = {};
  const back = st.bg && HEX.test(st.bg) ? st.bg : "";
  if (back) out.background = back;
  if (st.fg && HEX.test(st.fg)) out.color = readable(st.fg, [back || bg], 4.5);
  else if (back) out.color = contrast("#ffffff", back) >= contrast("#111111", back) ? "#ffffff" : "#111111";
  if (st.b) out.fontWeight = "700";
  if (st.i) out.fontStyle = "italic";
  if (st.u || st.s) out.textDecoration = [st.u ? "underline" : "", st.s ? "line-through" : ""].filter(Boolean).join(" ");
  if (looks.size > 500) looks.clear();
  looks.set(key, out);
  return out;
}

/** paintLine cuts a terminal line (buffer rows row.. joined, rowTexts
 * their texts) into runs by the colours of its rows. */
export function paintLine(st: ColorsState | null, buf: number, row: number, rowTexts: readonly string[], bg: string): TermSeg[] | null {
  if (!st || st.buf !== buf || !st.shown) return null;
  const text = rowTexts.join("");
  const marks: (Look | undefined)[] = [];
  let off = 0, any = false;
  rowTexts.forEach((t, k) => {
    const r = st.rows.get(row + k);
    if (r && r.text === t) {
      for (const [a, b, s] of r.spans) {
        const lk = look(st.styles[s], bg);
        for (let i = Math.max(0, a); i < Math.min(b, t.length); i++) marks[off + i] = lk;
        any = true;
      }
    }
    off += t.length;
  });
  if (!any) return null;
  const out: TermSeg[] = [];
  let start = 0;
  for (let i = 1; i <= text.length; i++) {
    if (i === text.length || marks[i] !== marks[start]) {
      out.push({ text: text.slice(start, i), look: marks[start] });
      start = i;
    }
  }
  return out;
}
