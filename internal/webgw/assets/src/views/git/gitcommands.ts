// The git views' workbench commands (Ctrl/Cmd+Shift+P): the commit
// graph, the pull requests, line blame, Commit (Amend) and Clone
// Repository (the last two through source control's own flow); the next
// and previous change or merge conflict of the file, the merge editor's
// Next and Previous Conflict, and source control's tree or flat list.
import type { Command } from "../../lib/commands";
import { blameOn } from "../../lib/blame";
import { GRAPH_TAB, PRS_TAB } from "../../lib/gittab";
import { go } from "../../lib/router";
import { activeDesk } from "../../lib/deskeditor";
import { diffOf } from "../../lib/difftab";
import { conflictRows, mergeActs, nextRow, scmTree } from "../../lib/gitnav";
import { activeFile, cursorAt, focusSlot, setPanes, showFile } from "../../lib/workbench";
import { link, store } from "../../session";
import { readGit } from "../../state/git";
import { gitAction, type GitAction } from "./GitMore";

const repo = () => readGit(store.topic("git").peek()?.data).repo;
const canAct = () => link.meta.peek()?.role !== "viewer";

function start(a: GitAction): void {
  setPanes({ side: true });
  go({ tab: "git", rest: "", query: {} });
  gitAction.value = a;
}

const desk = () => activeDesk.peek() !== null;
const merge = () => mergeActs.peek().get(focusSlot.peek()) ?? null;

/** change goes to the next (1) or previous (-1) git change: a diff
 * editor's own, else the file's gutter marks. */
function change(dir: 1 | -1): void {
  const d = activeDesk.peek();
  if (!d) return;
  const tab = activeFile.peek();
  if (tab && diffOf(tab)) return d.diffGo(dir);
  const n = nextRow(d.marks().map((m) => m.from), cursorAt.peek()?.line ?? 0, dir);
  if (n > 0) d.reveal(n);
}

/** conflict goes to the next (1) or previous (-1) "<<<<<<<" row. */
function conflict(dir: 1 | -1): void {
  const d = activeDesk.peek();
  if (!d) return;
  const n = nextRow(conflictRows(d.text()), cursorAt.peek()?.line ?? 0, dir);
  if (n > 0) d.reveal(n);
}

export function gitCommands(): Command[] {
  return [
    { id: "gitGraph", label: "cmd.git_graph", when: repo, run: () => showFile(GRAPH_TAB) },
    { id: "gitPRs", label: "cmd.git_prs", when: repo, run: () => showFile(PRS_TAB) },
    { id: "gitBlame", label: "cmd.git_blame", when: repo, run: () => (blameOn.value = !blameOn.peek()) },
    { id: "gitAmend", label: "cmd.git_amend", when: () => repo() && canAct(), run: () => start("amend") },
    { id: "gitClone", label: "cmd.git_clone", when: canAct, run: () => start("clone") },
    { id: "nextChange", label: "cmd.next_change", when: desk, run: () => change(1) },
    { id: "prevChange", label: "cmd.prev_change", when: desk, run: () => change(-1) },
    { id: "nextConflict", label: "cmd.next_conflict", when: () => desk() && !merge(), run: () => conflict(1) },
    { id: "prevConflict", label: "cmd.prev_conflict", when: () => desk() && !merge(), run: () => conflict(-1) },
    { id: "mergeNext", label: "cmd.merge_next", when: () => merge() !== null, run: () => merge()?.go(1) },
    { id: "mergeTakeBase", label: "cmd.merge_base", when: () => merge() !== null && canAct(), run: () => merge()?.base() },
    { id: "mergePrev", label: "cmd.merge_prev", when: () => merge() !== null, run: () => merge()?.go(-1) },
    { id: "scmViewToggle", label: "cmd.scm_tree", when: repo, run: () => (scmTree.value = !scmTree.peek()) },
  ];
}
