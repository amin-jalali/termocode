// The git views' workbench commands (Ctrl/Cmd+Shift+P): the commit
// graph, the pull requests, line blame, Commit (Amend) and Clone
// Repository (the last two through source control's own flow).
import type { Command } from "../../lib/commands";
import { blameOn } from "../../lib/blame";
import { GRAPH_TAB, PRS_TAB } from "../../lib/gittab";
import { go } from "../../lib/router";
import { setPanes, showFile } from "../../lib/workbench";
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

export function gitCommands(): Command[] {
  return [
    { id: "gitGraph", label: "cmd.git_graph", when: repo, run: () => showFile(GRAPH_TAB) },
    { id: "gitPRs", label: "cmd.git_prs", when: repo, run: () => showFile(PRS_TAB) },
    { id: "gitBlame", label: "cmd.git_blame", when: repo, run: () => (blameOn.value = !blameOn.peek()) },
    { id: "gitAmend", label: "cmd.git_amend", when: () => repo() && canAct(), run: () => start("amend") },
    { id: "gitClone", label: "cmd.git_clone", when: canAct, run: () => start("clone") },
  ];
}
