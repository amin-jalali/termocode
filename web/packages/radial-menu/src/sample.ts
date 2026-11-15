// The design's own sample data (mobocode-radial-v6.html CATS,
// radial_menu.dart _sampleCats): items without run show "▸ run <label>".
// The demo page uses it; a real page passes its own categories.
import type { Category } from "./menu";

const it = (id: string, label: string, icon: string, detail: string) => ({ id, label, icon, detail });

/** SAMPLE_FAVORITES are the design's SAVED ring. */
export const SAMPLE_FAVORITES = ["ai.ask", "edit.find", "git.commit", "edit.format"];

export const SAMPLE: Category[] = [
  {
    id: "edit",
    label: "EDIT",
    icon: "open",
    items: [
      it("edit.open", "open", "open", "open a file"),
      it("edit.find", "find", "find", "search across the project"),
      it("edit.diff", "diff", "diff", "show uncommitted changes"),
      it("edit.format", "format", "format", "reformat the current file"),
      it("edit.comment", "comment", "format", "toggle line comments"),
      it("edit.rename", "rename", "open", "rename the symbol"),
    ],
  },
  {
    id: "ai",
    label: "AI",
    icon: "ask",
    items: [
      it("ai.ask", "ask ai", "ask", "ask the assistant anything"),
      it("ai.explain", "explain", "ask", "explain the selected code"),
      it("ai.refactor", "refactor", "format", "restructure, same behavior"),
      it("ai.tests", "tests", "check", "generate unit tests"),
      it("ai.fix", "fix", "bolt", "find & fix a bug"),
      it("ai.docs", "docs", "open", "write documentation"),
      it("ai.review", "review", "diff", "review the current diff"),
    ],
  },
  {
    id: "git",
    label: "GIT",
    icon: "branch",
    items: [
      it("git.commit", "commit", "check", "stage & commit changes"),
      it("git.push", "push", "up", "push to the remote"),
      it("git.pull", "pull", "down", "pull from the remote"),
      it("git.diff", "diff", "diff", "show uncommitted changes"),
      it("git.stash", "stash", "shell", "stash working changes"),
      it("git.log", "log", "format", "view commit history"),
    ],
  },
  {
    id: "run",
    label: "RUN",
    icon: "shell",
    items: [
      it("run.build", "build", "shell", "compile the project"),
      it("run.test", "test", "check", "run the test suite"),
      it("run.lint", "lint", "format", "run the linter"),
      it("run.tasks", "tasks", "set", "run a saved task"),
    ],
  },
];
