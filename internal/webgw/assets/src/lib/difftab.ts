// Diff editor tabs (desktop): a changed file side by side, as the laptop's
// editor opens one from source control or the timeline. The tab's name in
// the editor's tab list says which change it is:
//   "@diff:wt:" + path        the working tree against the index
//   "@diff:idx:" + path       the index against HEAD (the staged change)
//   "@timeline:" + hash + ":" + path   a commit against its parent
import { t } from "../strings";

const WT = "@diff:wt:";
const IDX = "@diff:idx:";
const TIMELINE = "@timeline:";

export type DiffKind = "wt" | "idx" | "commit";

export interface DiffTab {
  kind: DiffKind;
  path: string;
  /** hash: the commit (kind "commit" only). */
  hash: string;
}

/** diffTab names the tab of path's change. */
export function diffTab(kind: DiffKind, path: string, hash = ""): string {
  return kind === "wt" ? WT + path : kind === "idx" ? IDX + path : TIMELINE + hash + ":" + path;
}

/** diffOf reads a diff tab (null: not one, or a broken one). */
export function diffOf(tab: string): DiffTab | null {
  if (tab.startsWith(WT) && tab.length > WT.length) return { kind: "wt", path: tab.slice(WT.length), hash: "" };
  if (tab.startsWith(IDX) && tab.length > IDX.length) return { kind: "idx", path: tab.slice(IDX.length), hash: "" };
  if (!tab.startsWith(TIMELINE)) return null;
  const rest = tab.slice(TIMELINE.length);
  const i = rest.indexOf(":");
  if (i < 4 || !/^[0-9a-f]+$/.test(rest.slice(0, i)) || i === rest.length - 1) return null;
  return { kind: "commit", path: rest.slice(i + 1), hash: rest.slice(0, i) };
}

/** isVirtual: the tab is not a file of its own (a diff, or a broken name). */
export function isVirtual(tab: string): boolean {
  return tab.startsWith("@diff:") || tab.startsWith(TIMELINE);
}

/** fileOfTab is the workspace file a tab is about (null: none). */
export function fileOfTab(tab: string): string | null {
  const d = diffOf(tab);
  if (d) return d.path;
  return isVirtual(tab) ? null : tab;
}

function baseName(path: string): string {
  return path.slice(path.lastIndexOf("/") + 1);
}

/** tabName is an editor tab's name: the file's; a diff says which sides. */
export function tabName(tab: string): string {
  const d = diffOf(tab);
  if (!d) return baseName(tab);
  const name = baseName(d.path);
  if (d.kind === "wt") return t("dv.tab_wt", { name });
  if (d.kind === "idx") return t("dv.tab_idx", { name });
  return t("dv.tab_commit", { name, hash: d.hash });
}

/** revs are the git revisions of a diff's two sides ("" : the live file). */
export function revs(d: DiffTab): { left: string; right: string } {
  if (d.kind === "wt") return { left: "index", right: "" };
  if (d.kind === "idx") return { left: "HEAD", right: "index" };
  return { left: d.hash + "^", right: d.hash };
}
