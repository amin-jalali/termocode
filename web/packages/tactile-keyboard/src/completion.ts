// The suggestion engine: language-server items (when the host has them)
// merged with identifiers already in the buffer, fuzzy-filtered against
// the word being typed and ranked, so suggestions work offline too.

export interface Suggestion {
  label: string;
  insertText: string;
  detail: string;
  /** For a snippet: where the caret lands inside insertText. */
  caretOffset?: number;
}

const IDENT = /[A-Za-z_]\w*/g; // \w* so one-letter names (i, x, _) count too

const isWord = (c: number): boolean => (c >= 48 && c <= 57) || (c >= 65 && c <= 90) || (c >= 97 && c <= 122) || c === 95;

const clampTo = (o: number, n: number): number => Math.max(0, Math.min(n, o));

/** currentWord is the identifier immediately before offset. */
export function currentWord(text: string, offset: number): string {
  const o = clampTo(offset, text.length);
  let s = o;
  while (s > 0 && isWord(text.charCodeAt(s - 1))) s--;
  return text.slice(s, o);
}

const afterDot = (text: string, offset: number): boolean => {
  const o = clampTo(offset, text.length);
  return o > 0 && text[o - 1] === ".";
};

/** hasTrigger: a word is being typed, or the caret is right after a dot. */
export function hasTrigger(text: string, offset: number): boolean {
  return currentWord(text, offset) !== "" || afterDot(text, offset);
}

/** suggestLocal: instant suggestions from the buffer alone. */
export function suggestLocal(text: string, offset: number, limit = 12): Suggestion[] {
  const word = currentWord(text, offset);
  if (!word) return []; // a bare dot needs the language server
  return rank(text, word, [], limit);
}

/** suggest: language-server items (lspFetch) merged with buffer words. */
export async function suggest(text: string, offset: number, lspFetch?: () => Promise<Suggestion[]>, limit = 12): Promise<Suggestion[]> {
  const word = currentWord(text, offset);
  if (!word && !afterDot(text, offset)) return [];
  const lsp = lspFetch ? await lspFetch() : [];
  return rank(text, word, lsp, limit);
}

function rank(text: string, word: string, lsp: Suggestion[], limit: number): Suggestion[] {
  const freq = new Map<string, number>();
  for (const m of text.matchAll(IDENT)) {
    const w = m[0];
    if (w === word) continue; // never suggest the word being typed
    freq.set(w, (freq.get(w) ?? 0) + 1);
  }
  const lspLabels = new Set(lsp.map((s) => s.label));
  const seen = new Set(lspLabels);
  const buffer: Suggestion[] = [];
  for (const w of freq.keys()) {
    if (seen.has(w)) continue;
    seen.add(w);
    buffer.push({ label: w, insertText: w, detail: "" });
  }
  const lower = word.toLowerCase();
  const scored: [Suggestion, number][] = [];
  for (const s of [...lsp, ...buffer]) {
    const sc = score(s.label, lower, lspLabels.has(s.label), freq.get(s.label) ?? 0);
    if (sc !== null) scored.push([s, sc]);
  }
  scored.sort((a, b) => b[1] - a[1]);
  return scored.slice(0, limit).map((x) => x[0]);
}

/** score: higher is better, null drops it. LSP origin, exact / prefix /
 * fuzzy match, brevity and buffer frequency. */
function score(label: string, lowerWord: string, isLsp: boolean, freq: number): number | null {
  if (!lowerWord) return isLsp ? 1000 : 100 + freq; // dot trigger
  const lower = label.toLowerCase();
  let s = 0;
  if (lower === lowerWord) s += 600;
  else if (lower.startsWith(lowerWord)) s += 400;
  else if (!fuzzy(lower, lowerWord)) return null;
  else s += 150;
  if (isLsp) s += 300;
  s += Math.max(0, Math.min(20, 20 - label.length));
  s += Math.max(0, Math.min(30, freq));
  return s;
}

/** fuzzy: is needle an in-order subsequence of haystack? */
function fuzzy(haystack: string, needle: string): boolean {
  let i = 0;
  for (let j = 0; j < haystack.length && i < needle.length; j++) if (haystack[j] === needle[i]) i++;
  return i === needle.length;
}
