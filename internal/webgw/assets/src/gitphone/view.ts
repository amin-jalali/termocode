// The changes view as a view spec (git.status, internal/views): the
// laptop's saved views (view.list) may hold one the AI or the user made
// for this page; the page draws the files through it here: its filter,
// its group (state, the default, or folder), its sort and its limit.
// Pure: no imports of the page.
import type { GitFile, Side } from "../state/git";
import type { ViewSpec } from "../home/viewspec";

// The view id, joined so it does not read as a string key.
export const STATUS_VIEW = ["git", "status"].join(".");

/** PageView is a saved view the page uses. */
export interface PageView {
  id: string;
  name: string;
  by: string;
  saved: number;
  spec: ViewSpec;
}

type Obj = Record<string, unknown>;
const isObj = (v: unknown): v is Obj => typeof v === "object" && v !== null && !Array.isArray(v);

/** pickView is the newest saved view of viewID in a view.list answer (or
 * a views_saved event), null when there is none. */
export function pickView(data: unknown, viewID: string): PageView | null {
  const items = isObj(data) && Array.isArray(data.items) ? data.items : [];
  let best: PageView | null = null;
  for (const v of items) {
    if (!isObj(v) || typeof v.id !== "string" || typeof v.name !== "string" || !isObj(v.spec) || v.spec.view !== viewID) continue;
    const saved = typeof v.saved === "number" ? v.saved : 0;
    if (!best || saved > best.saved) best = { id: v.id, name: v.name, by: typeof v.by === "string" ? v.by : "", saved, spec: v.spec as unknown as ViewSpec };
  }
  return best;
}

/** stateOf is a file's state field (webproto views_v1 git.status). */
export function stateOf(f: GitFile): string {
  return f.conflict ? "conflict" : f.untracked ? "new" : f.staged && !f.unstaged ? "staged" : "changed";
}

function cell(f: GitFile, field: string): string {
  switch (field) {
    case "path":
      return f.path;
    case "state":
      return stateOf(f);
    case "staged":
      return String(f.staged);
    case "x":
      return f.x;
    case "y":
      return f.y;
  }
  return "";
}

function holds(f: GitFile, c: NonNullable<ViewSpec["filter"]>[number]): boolean {
  const v = cell(f, c.field), want = c.value ?? "";
  switch (c.op) {
    case "eq":
      return v === want;
    case "ne":
      return v !== want;
    case "contains":
      return v.toLowerCase().includes(want.toLowerCase());
    case "starts":
      return v.startsWith(want);
    case "in":
      return (c.in ?? []).includes(v);
    case "empty":
      return v === "";
    case "not_empty":
      return v !== "";
    default:
      return true; // an order on text: the laptop would refuse it; show all
  }
}

/** Section is one group of rows: a side of the index, or a folder. */
export interface Section {
  key: string;
  title: string;
  /** side: the git side the rows are on (a folder's rows each say theirs). */
  side?: Side;
  rows: { f: GitFile; side: Side }[];
}

/** sideOf lists the sides a file shows on (staged and changed: both). */
export function sidesOf(f: GitFile): Side[] {
  if (f.conflict) return ["conflicts"];
  if (f.untracked) return ["untracked"];
  const out: Side[] = [];
  if (f.staged) out.push("staged");
  if (f.unstaged) out.push("changes");
  return out.length ? out : ["changes"];
}

export const SIDES: Side[] = ["conflicts", "staged", "changes", "untracked"];

function dirOf(p: string): string {
  const t = p.endsWith("/") ? p.slice(0, -1) : p;
  const i = t.lastIndexOf("/");
  return i < 0 ? "" : t.slice(0, i);
}

/** sections draws files through spec: the filtered rows in groups, and
 * how many the limit hides. A group of "state" (or none) keeps the
 * index's sides in their order; "path" groups by folder. */
export function sections(files: GitFile[], spec: ViewSpec | null, titles: Record<Side, string>, root: string): { list: Section[]; hidden: number } {
  let fs = files.filter((f) => (spec?.filter ?? []).every((c) => holds(f, c)));
  const sort = spec?.sort?.[0];
  if (sort) {
    const k = sort.field;
    fs = [...fs].sort((a, b) => cell(a, k).localeCompare(cell(b, k)) * (sort.desc ? -1 : 1));
  }
  let hidden = 0;
  if (spec?.limit && spec.limit > 0 && fs.length > spec.limit) {
    hidden = fs.length - spec.limit;
    fs = fs.slice(0, spec.limit);
  }
  if (spec?.group === "path") {
    const by = new Map<string, Section>();
    for (const f of fs) {
      const d = dirOf(f.path);
      let sec = by.get(d);
      if (!sec) by.set(d, (sec = { key: "d:" + d, title: d || root, rows: [] }));
      for (const side of sidesOf(f)) sec.rows.push({ f, side });
    }
    return { list: [...by.values()], hidden };
  }
  const list: Section[] = SIDES.map((side) => ({ key: side, title: titles[side], side, rows: [] as Section["rows"] }));
  for (const f of fs) for (const side of sidesOf(f)) list[SIDES.indexOf(side)].rows.push({ f, side });
  return { list: list.filter((x) => x.rows.length > 0), hidden };
}

/** describe is a short line of what a spec does, for the view's banner. */
export function describe(spec: ViewSpec): string[] {
  const out: string[] = [];
  for (const c of spec.filter ?? []) {
    if (c.field === "path" && (c.op === "contains" || c.op === "starts") && c.value) out.push(c.op === "starts" ? c.value + "…" : "only *" + c.value + (c.value.startsWith(".") ? "" : "*"));
    else if (c.op === "eq" || c.op === "ne") out.push(`${c.field} ${c.op === "eq" ? "=" : "≠"} ${c.value ?? ""}`);
    else if (c.op === "in") out.push(`${c.field}: ${(c.in ?? []).join(", ")}`);
  }
  if (spec.group === "path") out.push("by folder");
  if (spec.limit) out.push(`max ${spec.limit}`);
  return out;
}
