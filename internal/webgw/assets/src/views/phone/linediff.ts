// A line diff for the phone's review (MOB-14): the file before and after
// the agent as rows of kept, removed and added lines, cut into hunks with
// a few kept lines around each change. Lines common to the start and the
// end are matched first; the middle is matched by its longest common
// lines while it is small, else shown as removed then added. Pure.

export type RowKind = "ctx" | "add" | "del";

export interface Row {
  kind: RowKind;
  /** old is the line's number before the agent (ctx, del), 1-based. */
  old?: number;
  /** new is the line's number after the agent (ctx, add), 1-based. */
  new?: number;
  text: string;
}

export interface Hunk {
  rows: Row[];
}

/** MAX_CELLS caps the middle's matching table (old × new lines). */
const MAX_CELLS = 2_000_000;

export function lines(text: string): string[] {
  if (text === "") return [];
  const ls = text.split("\n");
  if (ls[ls.length - 1] === "") ls.pop();
  return ls.map((l) => (l.endsWith("\r") ? l.slice(0, -1) : l));
}

/** diffRows is every line of before and after as kept, removed or added. */
export function diffRows(before: string, after: string): Row[] {
  const a = lines(before), b = lines(after);
  let p = 0;
  while (p < a.length && p < b.length && a[p] === b[p]) p++;
  let s = 0;
  while (s < a.length - p && s < b.length - p && a[a.length - 1 - s] === b[b.length - 1 - s]) s++;
  const rows: Row[] = [];
  for (let i = 0; i < p; i++) rows.push({ kind: "ctx", old: i + 1, new: i + 1, text: a[i] });
  const am = a.slice(p, a.length - s), bm = b.slice(p, b.length - s);
  for (const r of middle(am, bm)) {
    rows.push({ kind: r.kind, old: r.old === undefined ? undefined : r.old + p + 1, new: r.new === undefined ? undefined : r.new + p + 1, text: r.text });
  }
  for (let i = 0; i < s; i++) {
    const oi = a.length - s + i, ni = b.length - s + i;
    rows.push({ kind: "ctx", old: oi + 1, new: ni + 1, text: a[oi] });
  }
  return rows;
}

/** middle matches a and b by their longest common lines (0-based numbers). */
function middle(a: string[], b: string[]): Row[] {
  const out: Row[] = [];
  const n = a.length, m = b.length;
  if (n * m > MAX_CELLS || n === 0 || m === 0) {
    a.forEach((text, i) => out.push({ kind: "del", old: i, text }));
    b.forEach((text, j) => out.push({ kind: "add", new: j, text }));
    return out;
  }
  // len[i][j]: longest common lines of a[i:] and b[j:].
  const w = m + 1;
  const len = new Uint32Array((n + 1) * w);
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      len[i * w + j] = a[i] === b[j] ? len[(i + 1) * w + j + 1] + 1 : Math.max(len[(i + 1) * w + j], len[i * w + j + 1]);
    }
  }
  let i = 0, j = 0;
  while (i < n && j < m) {
    if (a[i] === b[j]) {
      out.push({ kind: "ctx", old: i, new: j, text: a[i] });
      i++;
      j++;
    } else if (len[(i + 1) * w + j] >= len[i * w + j + 1]) {
      out.push({ kind: "del", old: i, text: a[i] });
      i++;
    } else {
      out.push({ kind: "add", new: j, text: b[j] });
      j++;
    }
  }
  for (; i < n; i++) out.push({ kind: "del", old: i, text: a[i] });
  for (; j < m; j++) out.push({ kind: "add", new: j, text: b[j] });
  return out;
}

/** hunks keeps the changed rows with ctx kept rows around each; rows
 * between two changes closer than 2×ctx join one hunk. */
export function hunks(rows: Row[], ctx = 3): Hunk[] {
  const keep = new Uint8Array(rows.length);
  rows.forEach((r, i) => {
    if (r.kind === "ctx") return;
    for (let k = Math.max(0, i - ctx); k <= Math.min(rows.length - 1, i + ctx); k++) keep[k] = 1;
  });
  const out: Hunk[] = [];
  let cur: Row[] | null = null;
  rows.forEach((r, i) => {
    if (!keep[i]) {
      cur = null;
      return;
    }
    if (!cur) {
      cur = [];
      out.push({ rows: cur });
    }
    cur.push(r);
  });
  return out;
}

/** noteSide is where a note on row r points: the line after the agent
 * for kept and added lines, before it for removed ones. */
export function noteSide(r: Row): { side: "new" | "old"; line: number } {
  return r.kind === "del" ? { side: "old", line: r.old ?? 0 } : { side: "new", line: r.new ?? 0 };
}
