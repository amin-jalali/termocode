// Line and hunk staging in the desktop editor (the laptop's Git: Stage
// Line, Unstage Line, Discard Line (or Selected Lines) and Discard Hunk
// (at cursor)): the rows the selection covers, of the working file (a
// file tab or a working tree diff) or of the index (a staged diff).
// Staging asks nothing; discarding asks here, then the laptop.
import type { DiffRow, GitDiff } from "../../../../webproto/proto";
import { link } from "../session";
import { t } from "../strings";
import { gitFailed } from "../views/git/errors";
import { activeDesk } from "./deskeditor";
import { diffOf, isVirtual } from "./difftab";
import { gitAsk } from "./gitnav";
import { toast } from "./toasts";
import { activeFile } from "./workbench";

/** LineTarget is the file and rows a line command acts on; index: the
 * rows are the index's (a staged diff). */
export interface LineTarget {
  path: string;
  index: boolean;
  from: number;
  to: number;
}

/** lineTarget reads the focused editor (null: not a file or its change). */
export function lineTarget(): LineTarget | null {
  const tab = activeFile.peek();
  const d = activeDesk.peek();
  if (!tab || !d) return null;
  const r = d.rows();
  if (r.from < 1) return null;
  const diff = diffOf(tab);
  if (diff && diff.kind !== "wt" && diff.kind !== "idx") return null;
  if (!diff && isVirtual(tab)) return null;
  return { path: diff ? diff.path : tab, index: diff?.kind === "idx", from: r.from, to: r.to };
}

/** rowsName: "line 4", "lines 4-6". */
export function rowsName(from: number, to: number): string {
  return from === to ? t("gl.row", { n: from }) : t("gl.rows", { a: from, b: to });
}

const nameOf = (p: string) => p.slice(p.lastIndexOf("/") + 1);

/** stageLines stages (on) or unstages the selected rows. */
export function stageLines(on: boolean): void {
  const g = lineTarget();
  if (!g || (on && g.index)) return;
  const args: Record<string, unknown> = { path: g.path, from: g.from, to: g.to };
  if (g.index) args.index = true;
  link.request(on ? "git.stage_lines" : "git.unstage_lines", args).then(
    () => toast("ok", t(on ? "gl.staged" : "gl.unstaged", { rows: rowsName(g.from, g.to), name: nameOf(g.path) })),
    gitFailed,
  );
}

/** discardLines asks to discard the selected rows' changes. */
export function discardLines(): void {
  const g = lineTarget();
  if (!g || g.index) return;
  gitAsk.value = { op: "git.discard_lines", args: { path: g.path, from: g.from, to: g.to }, label: t("gl.discard"), text: t("gl.discard_q", { rows: rowsName(g.from, g.to), path: g.path }) };
}

/** hunkAt is the header of the hunk of rows that covers row n of the
 * new text (a deletion: the rows beside it). */
export function hunkAt(rows: readonly DiffRow[], n: number): string {
  let head = "", first = 0, last = 0;
  const hit = () => head !== "" && n >= first && n <= Math.max(last, first);
  for (const r of rows) {
    if (r.k === "hunk") {
      if (hit()) return head;
      head = r.text;
      first = Math.max(1, r.new ?? 1);
      last = first - 1;
    } else if ((r.k === "ctx" || r.k === "add") && r.new) last = Math.max(last, r.new);
  }
  return hit() ? head : "";
}

/** discardHunk asks to discard the change at the caret. */
export function discardHunk(): void {
  const g = lineTarget();
  if (!g || g.index) return;
  const n = activeDesk.peek()?.rows().from ?? 0;
  link.request("git.diff", { path: g.path }).then((r) => {
    const head = hunkAt((r as GitDiff).rows ?? [], n);
    if (!head) return toast("info", t("gl.no_change"));
    gitAsk.value = { op: "git.discard_hunk", args: { path: g.path, hunk: head }, label: t("gl.discard_hunk"), text: t("gl.hunk_q", { n, path: g.path }) };
  }, gitFailed);
}
