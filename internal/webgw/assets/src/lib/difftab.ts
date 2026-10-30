// Diff editor tabs (desktop): a changed file side by side, as the laptop's
// editor opens one from source control or the timeline. The tab's name in
// the editor's tab list says which change it is:
//   "@diff:wt:" + path        the working tree against the index
//   "@diff:idx:" + path       the index against HEAD (the staged change)
//   "@timeline:" + hash + ":" + path   a commit against its parent
//   "@review:" + card + ":" + path     the AI's change a review card asks about
//   "@ai:" + n + ":" + path    the AI's suggestion n for a file (lib/ai.ts)
//   "@agent:" + id + ":" + path   a file an agent changed in its worktree
import { t } from "../strings";
import { vtabName, vtabOf } from "./vtabs";
import { gitTabName, gitTabOf } from "./gittab";

const WT = "@diff:wt:";
const IDX = "@diff:idx:";
const TIMELINE = "@timeline:";
const REVIEW = "@review:";
const AI = "@ai:";
const AGENT = "@agent:";

export type DiffKind = "wt" | "idx" | "commit" | "review" | "ai" | "agent";

export interface DiffTab {
  kind: DiffKind;
  path: string;
  /** hash: the commit (kind "commit"), the review card's id, the
   * suggestion's number (kind "ai") or the worktree's id (kind "agent"). */
  hash: string;
}

/** aiTab names the tab of the AI's suggestion n for path. */
export function aiTab(n: number, path: string): string {
  return AI + n + ":" + path;
}

/** agentTab names the tab of a file agent worktree id changed. */
export function agentTab(id: string, path: string): string {
  return AGENT + id + ":" + path;
}

/** keyed reads "<prefix><key>:<path>" (null: not one). */
function keyed(tab: string, prefix: string, key: RegExp): { key: string; path: string } | null {
  if (!tab.startsWith(prefix)) return null;
  const rest = tab.slice(prefix.length);
  const i = rest.indexOf(":");
  if (i < 1 || i === rest.length - 1 || !key.test(rest.slice(0, i))) return null;
  return { key: rest.slice(0, i), path: rest.slice(i + 1) };
}

/** diffTab names the tab of path's change. */
export function diffTab(kind: DiffKind, path: string, hash = ""): string {
  if (kind === "wt") return WT + path;
  if (kind === "idx") return IDX + path;
  return (kind === "review" ? REVIEW : TIMELINE) + hash + ":" + path;
}

/** diffOf reads a diff tab (null: not one, or a broken one). */
export function diffOf(tab: string): DiffTab | null {
  if (tab.startsWith(WT) && tab.length > WT.length) return { kind: "wt", path: tab.slice(WT.length), hash: "" };
  if (tab.startsWith(IDX) && tab.length > IDX.length) return { kind: "idx", path: tab.slice(IDX.length), hash: "" };
  if (tab.startsWith(REVIEW)) {
    const rest = tab.slice(REVIEW.length);
    const i = rest.indexOf(":");
    if (i < 1 || i === rest.length - 1) return null;
    return { kind: "review", path: rest.slice(i + 1), hash: rest.slice(0, i) };
  }
  const ai = keyed(tab, AI, /^[0-9]+$/);
  if (ai) return { kind: "ai", path: ai.path, hash: ai.key };
  const ag = keyed(tab, AGENT, /^[A-Za-z0-9][A-Za-z0-9._-]*$/);
  if (ag) return { kind: "agent", path: ag.path, hash: ag.key };
  if (!tab.startsWith(TIMELINE)) return null;
  const rest = tab.slice(TIMELINE.length);
  const i = rest.indexOf(":");
  if (i < 4 || !/^[0-9a-f]+$/.test(rest.slice(0, i)) || i === rest.length - 1) return null;
  return { kind: "commit", path: rest.slice(i + 1), hash: rest.slice(0, i) };
}

/** isVirtual: the tab is not a file of its own (a diff, a table, a
 * query, details: every name that starts with "@"). */
export function isVirtual(tab: string): boolean {
  return tab.startsWith("@");
}

/** fileOfTab is the workspace file a tab is about (null: none). */
export function fileOfTab(tab: string): string | null {
  const d = diffOf(tab);
  if (d) return d.kind === "agent" ? null : d.path; // an agent's file is not the workspace's
  return isVirtual(tab) ? null : tab;
}

function baseName(path: string): string {
  return path.slice(path.lastIndexOf("/") + 1);
}

/** tabName is an editor tab's name: the file's; a diff says which sides. */
export function tabName(tab: string): string {
  const v = vtabOf(tab);
  if (v) return vtabName(v);
  const g = gitTabOf(tab);
  if (g) return gitTabName(g);
  const d = diffOf(tab);
  if (!d) return baseName(tab);
  const name = baseName(d.path);
  if (d.kind === "wt") return t("dv.tab_wt", { name });
  if (d.kind === "idx") return t("dv.tab_idx", { name });
  if (d.kind === "review") return t("dv.tab_review", { name });
  if (d.kind === "ai") return t("ai.tab", { name });
  if (d.kind === "agent") return t("ai.tab_agent", { name });
  return t("dv.tab_commit", { name, hash: d.hash });
}

/** revs are the git revisions of a diff's two sides ("" : the live file). */
export function revs(d: DiffTab): { left: string; right: string } {
  if (d.kind === "ai" || d.kind === "agent") return { left: "", right: "" }; // not git's: views/AIDiff.tsx
  if (d.kind === "wt") return { left: "index", right: "" };
  if (d.kind === "idx") return { left: "HEAD", right: "index" };
  if (d.kind === "review") return { left: "", right: "" };
  return { left: d.hash + "^", right: d.hash };
}

/** Row is a diff row as a review card has it. */
export interface Row {
  k: string;
  text: string;
  old?: number;
  new?: number;
}

/** applyRows is the text after a review card's change: the rows' added
 * lines go in, the removed ones go out, every other line stays. null:
 * the rows do not fit the text (it changed since). */
export function applyRows(old: readonly string[], rows: readonly Row[]): string[] | null {
  const out: string[] = [];
  let o = 1; // the next old line to copy (1-based)
  const copyTo = (k: number) => {
    while (o < k && o <= old.length) out.push(old[o++ - 1]);
  };
  for (const r of rows) {
    if (r.k === "ctx" || r.k === "del") {
      const k = r.old ?? 0;
      if (k < o || k > old.length) return null;
      copyTo(k);
      if (r.k === "ctx") out.push(old[k - 1]);
      o = k + 1;
    } else if (r.k === "add") {
      if (r.new) while (out.length < r.new - 1 && o <= old.length) out.push(old[o++ - 1]);
      out.push(r.text);
    }
  }
  copyTo(old.length + 1);
  return out;
}
