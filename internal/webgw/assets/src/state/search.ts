// Search hits as the Search view reads them (web design 2.7). Pure: no
// DOM, timers or network.

export interface SearchHit {
  path: string;
  line: number;
  col: number;
  text: string;
  marks: { s: number; e: number }[];
}

export interface SearchSummary {
  files: number;
  matches: number;
  truncated: boolean;
  ms: number;
  stopped: string;
}

type Obj = Record<string, unknown>;

function obj(v: unknown): Obj {
  return typeof v === "object" && v !== null && !Array.isArray(v) ? (v as Obj) : {};
}

function num(v: unknown): number {
  return typeof v === "number" && Number.isFinite(v) && v >= 0 ? Math.floor(v) : 0;
}

function str(v: unknown): string {
  return typeof v === "string" ? v : "";
}

/** readBatch reads a search.batch event: its request id and hits. */
export function readBatch(data: unknown): { id: number; hits: SearchHit[] } {
  const d = obj(data);
  const hits = (Array.isArray(d.results) ? d.results : []).map(obj).map((r) => ({
    path: str(r.path),
    line: num(r.line),
    col: num(r.col),
    text: str(r.text),
    marks: (Array.isArray(r.marks) ? r.marks : []).map(obj).map((m) => ({ s: num(m.s), e: num(m.e) })),
  }));
  return { id: num(d.id), hits: hits.filter((h) => h.path !== "") };
}

/** readSummary reads search.run's answer. */
export function readSummary(data: unknown): SearchSummary {
  const d = obj(data);
  return { files: num(d.files), matches: num(d.matches), truncated: d.truncated === true, ms: num(d.ms), stopped: str(d.stopped) };
}

/** byFile groups hits by file, in the order the files first came. */
export function byFile(hits: SearchHit[]): { path: string; hits: SearchHit[] }[] {
  const out: { path: string; hits: SearchHit[] }[] = [];
  const at = new Map<string, number>();
  for (const h of hits) {
    let i = at.get(h.path);
    if (i === undefined) {
      i = out.length;
      at.set(h.path, i);
      out.push({ path: h.path, hits: [] });
    }
    out[i].hits.push(h);
  }
  return out;
}

/** segments splits text into plain and marked parts (marks in order,
 * clamped to the text, overlaps dropped). */
export function segments(text: string, marks: { s: number; e: number }[]): { text: string; mark: boolean }[] {
  const out: { text: string; mark: boolean }[] = [];
  let at = 0;
  for (const m of [...marks].sort((a, b) => a.s - b.s)) {
    const s = Math.max(m.s, at), e = Math.min(m.e, text.length);
    if (e <= s) continue;
    if (s > at) out.push({ text: text.slice(at, s), mark: false });
    out.push({ text: text.slice(s, e), mark: true });
    at = e;
  }
  if (at < text.length || out.length === 0) out.push({ text: text.slice(at), mark: false });
  return out;
}

/** seconds writes a duration in ms as "0.4 s". */
export function seconds(ms: number): string {
  return `${(ms / 1000).toFixed(1)} s`;
}
