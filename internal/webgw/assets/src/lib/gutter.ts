// Git change marks in the desktop editor's gutter: the file's unstaged
// diff (git.diff) as the rows that were added or changed and the places
// where rows were deleted, as the laptop's editor marks them. Pure.
import type { DiffRow } from "../../../../webproto/proto";

export type MarkKind = "add" | "mod" | "del";

/** Mark covers rows [from, to] (1-based, of the new text). */
export interface Mark {
  from: number;
  to: number;
  kind: MarkKind;
}

/** gutterMarks reads a diff's rows: a run of deleted rows followed by
 * added ones is a change (the extra added rows are added), added rows
 * alone are added, deleted rows alone mark the row where they were. */
export function gutterMarks(rows: readonly DiffRow[]): Mark[] {
  const out: Mark[] = [];
  let i = 0;
  let next = 1; // the new text's row after the last one seen
  while (i < rows.length) {
    const r = rows[i];
    if (r.k === "ctx") {
      next = (r.new ?? next) + 1;
      i++;
      continue;
    }
    if (r.k !== "add" && r.k !== "del") {
      i++;
      continue;
    }
    let dels = 0;
    const adds: number[] = [];
    while (i < rows.length && (rows[i].k === "add" || rows[i].k === "del")) {
      if (rows[i].k === "del") dels++;
      else adds.push(rows[i].new ?? next + adds.length);
      i++;
    }
    if (adds.length === 0) {
      out.push({ from: Math.max(1, next - 1), to: Math.max(1, next - 1), kind: "del" });
      continue;
    }
    const mod = Math.min(dels, adds.length);
    if (mod > 0) out.push({ from: adds[0], to: adds[mod - 1], kind: "mod" });
    if (adds.length > mod) out.push({ from: adds[mod], to: adds[adds.length - 1], kind: "add" });
    next = adds[adds.length - 1] + 1;
  }
  return out;
}
