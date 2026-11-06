// The git views' workbench commands (Ctrl/Cmd+Shift+P): the commit
// graph, the pull requests, line blame, Commit (Amend) and Clone
// Repository (the last two through source control's own flow); the next
// and previous change or merge conflict of the file, the merge editor's
// Next and Previous Conflict, source control's tree or flat list, and
// staging, unstaging or discarding the selected rows or the change at
// the caret; the tags (check one out) and the stash (apply an entry);
// undo, redo and the reflog (each after the laptop's card).
import type { Command } from "../../lib/commands";
import { blameOn } from "../../lib/blame";
import { GRAPH_TAB, PRS_TAB } from "../../lib/gittab";
import { go } from "../../lib/router";
import { activeDesk } from "../../lib/deskeditor";
import { diffOf } from "../../lib/difftab";
import { discardHunk, discardLines, lineTarget, stageLines } from "../../lib/gitlines";
import { conflictRows, gitAsk, mergeActs, nextRow, scmTree } from "../../lib/gitnav";
import { toast } from "../../lib/toasts";
import { t } from "../../strings";
import { gitFailed } from "./errors";
import type { GitReflog, GitStashList, GitTags, GitUndoPlan } from "../../../../../webproto/proto";
import { age } from "../../state/git";
import { activeFile, cursorAt, focusSlot, quick, setPanes, showFile, type PickItem } from "../../lib/workbench";
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

/** pick opens a list in the quick input (empty: a toast says so). */
function pick(title: string, items: PickItem[], empty: string, run: (key: string) => void): void {
  if (items.length === 0) {
    toast("info", empty);
    return;
  }
  quick.value = { mode: "pick", pick: { title, items, run } };
}

/** tags lists the tags; picking one asks to check it out. */
function tags(): void {
  link.request("git.tags", {}).then((r) => {
    const list = (r as GitTags).tags ?? [];
    pick(t("gt.pick"), list.map((n) => ({ key: n, label: n })), t("gt.none"), (tag) => {
      if (canAct()) gitAsk.value = { op: "git.checkout_tag", args: { tag }, label: t("gt.checkout"), text: t("gt.checkout_q", { tag }) };
    });
  }, gitFailed);
}

/** stashList lists the stash; picking an entry asks to apply it. */
function stashList(): void {
  link.request("git.stash_list", {}).then((r) => {
    const list = (r as GitStashList).entries ?? [];
    pick(t("gs.pick"), list.map((e) => ({ key: e.ref, label: e.message || e.ref, detail: e.ref })), t("gs.none"), (ref) => {
      if (canAct()) gitAsk.value = { op: "git.stash_apply", args: { ref }, label: t("gs.apply"), text: t("gs.apply_q", { ref }) };
    });
  }, gitFailed);
}

/** undoAsk shows a plan of git.undo_plan, then asks the laptop. */
function undoAsk(args: Record<string, unknown>): void {
  link.request("git.undo_plan", args).then((r) => {
    const p = r as GitUndoPlan;
    gitAsk.value = { op: "git.undo", args: p.move ? { move: true } : {}, label: p.action, text: p.text };
  }, gitFailed);
}

/** reflog lists HEAD's moves; picking one plans moving HEAD back there. */
function reflog(): void {
  link.request("git.reflog", {}).then((r) => {
    const list = (r as GitReflog).entries ?? [];
    const now = Date.now();
    pick(t("gu.reflog_pick"), list.map((e, i) => ({ key: String(i), label: e.summary, detail: `${e.hash} · ${age(e.when, now)}` })), t("gu.reflog_none"), (k) => {
      const e = list[Number(k)];
      if (e && canAct()) undoAsk({ reflog: true, index: Number(k), hash: e.hash });
    });
  }, gitFailed);
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
    { id: "stageLines", label: "cmd.stage_lines", when: () => canAct() && lineTarget()?.index === false, run: () => stageLines(true) },
    { id: "unstageLines", label: "cmd.unstage_lines", when: () => canAct() && lineTarget() !== null, run: () => stageLines(false) },
    { id: "discardLines", label: "cmd.discard_lines", when: () => canAct() && lineTarget()?.index === false, run: discardLines },
    { id: "discardHunk", label: "cmd.discard_hunk", when: () => canAct() && lineTarget()?.index === false, run: discardHunk },
    { id: "gitTags", label: "cmd.git_tags", when: repo, run: tags },
    { id: "gitStashList", label: "cmd.git_stash_list", when: repo, run: stashList },
    { id: "gitUndo", label: "cmd.git_undo", when: () => repo() && canAct(), run: () => undoAsk({}) },
    { id: "gitRedo", label: "cmd.git_redo", when: () => repo() && canAct(), run: () => (gitAsk.value = { op: "git.redo", args: {}, label: t("live.redo"), text: t("gu.redo_q") }) },
    { id: "gitReflog", label: "cmd.git_reflog", when: repo, run: reflog },
    { id: "scmViewToggle", label: "cmd.scm_tree", when: repo, run: () => (scmTree.value = !scmTree.peek()) },
  ];
}
