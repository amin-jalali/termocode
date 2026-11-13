// The one change between two texts (WEB-39): the common start and end are
// kept, the middle is replaced. Used by the editor (laptop text in) and
// the live sync (user text out). A surrogate pair is never cut in two.
// Pure.

export interface Edit {
  from: number;
  to: number;
  insert: string;
}

function isHigh(c: number): boolean {
  return c >= 0xd800 && c < 0xdc00;
}

function isLow(c: number): boolean {
  return c >= 0xdc00 && c < 0xe000;
}

/** diff is the edit that turns a into b, or null when they are equal. */
export function diff(a: string, b: string): Edit | null {
  if (a === b) return null;
  let p = 0;
  const n = Math.min(a.length, b.length);
  while (p < n && a.charCodeAt(p) === b.charCodeAt(p)) p++;
  if (p > 0 && isHigh(a.charCodeAt(p - 1))) p--;
  let s = 0;
  while (s < n - p && a.charCodeAt(a.length - 1 - s) === b.charCodeAt(b.length - 1 - s)) s++;
  if (s > 0 && isLow(a.charCodeAt(a.length - s))) s--;
  return { from: p, to: a.length - s, insert: b.slice(p, b.length - s) };
}

/** utf8Len is the UTF-8 byte length of s (nvim counts columns in bytes). */
export function utf8Len(s: string): number {
  let n = 0;
  for (let i = 0; i < s.length; i++) {
    const c = s.charCodeAt(i);
    if (c < 0x80) n += 1;
    else if (c < 0x800) n += 2;
    else if (isHigh(c) && i + 1 < s.length && isLow(s.charCodeAt(i + 1))) {
      n += 4;
      i++;
    } else n += 3;
  }
  return n;
}

/** pos is offset at in text as nvim's (row, byte column). */
export function pos(text: string, at: number): { row: number; col: number } {
  let row = 0, start = 0;
  for (let i = text.indexOf("\n"); i >= 0 && i < at; i = text.indexOf("\n", i + 1)) {
    row++;
    start = i + 1;
  }
  return { row, col: utf8Len(text.slice(start, at)) };
}
