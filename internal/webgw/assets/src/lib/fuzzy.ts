// Fuzzy matching for Go to file and the command list (workbench): the
// query's letters in order, case-insensitive. A match scores higher when
// its letters are close together, start words or path parts, and sit in
// the file's name rather than its folders. Pure.

export interface Hit {
  /** score: higher is better. */
  score: number;
  /** at: the matched positions in the text (UTF-16 indexes). */
  at: number[];
}

function isStart(text: string, i: number): boolean {
  if (i === 0) return true;
  const p = text[i - 1], c = text[i];
  return p === "/" || p === "_" || p === "-" || p === "." || p === " " || (p >= "a" && p <= "z" && c >= "A" && c <= "Z");
}

/** match finds query in text, or returns null. An empty query matches
 * everything with score 0. Spaces in the query are ignored. */
export function match(query: string, text: string): Hit | null {
  const q = query.replace(/\s+/g, "").toLowerCase();
  if (!q) return { score: 0, at: [] };
  const low = text.toLowerCase();
  const nameAt = text.lastIndexOf("/") + 1;
  // Prefer the file's own name: try to match the query's tail there first
  // (greedy from the end), then the rest before it.
  const at: number[] = [];
  let i = low.length - 1;
  for (let k = q.length - 1; k >= 0; k--) {
    while (i >= 0 && low[i] !== q[k]) i--;
    if (i < 0) return null;
    at.push(i);
    i--;
  }
  at.reverse();
  // Pull each letter forward to the latest place that still keeps order,
  // which joins runs: "fil" in "filter.go" is 0,1,2, not scattered.
  for (let k = at.length - 2; k >= 0; k--) {
    const want = at[k + 1] - 1;
    if (want > at[k] && low[want] === q[k]) at[k] = want;
  }
  let score = 0;
  for (let k = 0; k < at.length; k++) {
    const p = at[k];
    score += 1;
    if (k > 0 && at[k - 1] === p - 1) score += 5;
    if (isStart(text, p)) score += 3;
    if (p >= nameAt) score += 2;
    if (text[p] === query.replace(/\s+/g, "")[k]) score += 0.5;
  }
  if (low.slice(nameAt).startsWith(q)) score += 10;
  score -= text.length * 0.01;
  return { score, at };
}

/** rank orders the items that match query, best first, at most max. */
export function rank<T>(query: string, items: readonly T[], text: (x: T) => string, max: number): { item: T; hit: Hit }[] {
  const out: { item: T; hit: Hit }[] = [];
  for (const item of items) {
    const hit = match(query, text(item));
    if (hit) out.push({ item, hit });
  }
  out.sort((a, b) => b.hit.score - a.hit.score);
  return out.slice(0, max);
}
