// Live editing together (G-COL), pure: two changes made on one base
// joined the way the hub does it (internal/coedit), and nvim's byte
// columns as the editor's UTF-16 units. Part of coedit.js.
import { diff } from "../lib/textdiff";

function isHigh(c: number): boolean {
  return c >= 0xd800 && c < 0xdc00;
}

function isLow(c: number): boolean {
  return c >= 0xdc00 && c < 0xe000;
}

/** colUtf16 turns a byte column of line (nvim) into UTF-16 units (the
 * editor's), at most the line's length. */
export function colUtf16(line: string, bytes: number): number {
  let n = 0, i = 0;
  while (i < line.length && n < bytes) {
    const c = line.charCodeAt(i);
    const pair = isHigh(c) && i + 1 < line.length && isLow(line.charCodeAt(i + 1));
    n += c < 0x80 ? 1 : c < 0x800 ? 2 : pair ? 4 : 3;
    i += pair ? 2 : 1;
  }
  return i;
}

/** merge3 joins two changes made on one base: mine (the page's) and
 * theirs (the laptop's). When they do not touch the same text both land,
 * with the hub's edge rules (internal/coedit): two inserts at one point
 * put theirs first. An overlap is a real conflict: null. */
export function merge3(base: string, mine: string, theirs: string): string | null {
  const m = diff(base, mine);
  const t = diff(base, theirs);
  if (!m) return theirs;
  if (!t) return mine;
  if (t.to <= m.from) return base.slice(0, t.from) + t.insert + base.slice(t.to, m.from) + m.insert + base.slice(m.to);
  if (m.to <= t.from) return base.slice(0, m.from) + m.insert + base.slice(m.to, t.from) + t.insert + base.slice(t.to);
  return null;
}
