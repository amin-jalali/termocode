// Git editor tabs (desktop): views of the repository that open in the
// editor area beside the files, as the laptop's editor opens them. The
// tab's name in the editor's tab list says which one:
//   "@git:graph"             the commit graph of every branch
//   "@git:merge:" + path     a conflicted file's merge editor
//   "@git:rebase:" + hash    an interactive rebase from that commit
//   "@git:prs"               the pull requests and the branch's checks
//   "@git:pr:" + number      one pull request
import { t } from "../strings";

const PRE = "@git:";
export const GRAPH_TAB = PRE + "graph";
export const PRS_TAB = PRE + "prs";
const MERGE = PRE + "merge:";
const REBASE = PRE + "rebase:";
const PR = PRE + "pr:";

export type GitTab =
  | { kind: "graph" }
  | { kind: "prs" }
  | { kind: "merge"; path: string }
  | { kind: "rebase"; hash: string }
  | { kind: "pr"; n: number };

export const mergeTab = (path: string): string => MERGE + path;
export const rebaseTab = (hash: string): string => REBASE + hash;
export const prTab = (n: number): string => PR + n;

/** isGitTab: the tab is a git view (maybe a broken one). */
export function isGitTab(tab: string): boolean {
  return tab.startsWith(PRE);
}

/** gitTabOf reads a git tab (null: not one, or a broken one). */
export function gitTabOf(tab: string): GitTab | null {
  if (tab === GRAPH_TAB) return { kind: "graph" };
  if (tab === PRS_TAB) return { kind: "prs" };
  if (tab.startsWith(MERGE) && tab.length > MERGE.length) return { kind: "merge", path: tab.slice(MERGE.length) };
  if (tab.startsWith(REBASE)) {
    const hash = tab.slice(REBASE.length);
    return /^[0-9a-f]{4,64}$/.test(hash) ? { kind: "rebase", hash } : null;
  }
  if (tab.startsWith(PR)) {
    const n = Number(tab.slice(PR.length));
    return Number.isInteger(n) && n > 0 && String(n) === tab.slice(PR.length) ? { kind: "pr", n } : null;
  }
  return null;
}

/** gitTabName is a git tab's name in the tab list. */
export function gitTabName(g: GitTab): string {
  switch (g.kind) {
    case "graph":
      return t("gg.tab");
    case "prs":
      return t("pr.tab");
    case "merge":
      return t("gm.tab", { name: g.path.slice(g.path.lastIndexOf("/") + 1) });
    case "rebase":
      return t("gr.tab", { hash: g.hash.slice(0, 7) });
    case "pr":
      return t("pr.tab_one", { n: g.n });
  }
}

/** gitTabIcon is the codicon a git tab shows before its name. */
export function gitTabIcon(g: GitTab): string {
  switch (g.kind) {
    case "graph":
      return "git-commit";
    case "merge":
      return "git-merge";
    case "rebase":
      return "list-ordered";
    default:
      return "git-pull-request";
  }
}
