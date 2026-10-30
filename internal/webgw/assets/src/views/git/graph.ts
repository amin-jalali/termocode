// Drawing the commit graph (desktop): the hub lays the lanes out
// (git.graph gives each row its node column and the lines of its two
// halves); this only turns them into SVG paths. A lane is LANE px wide;
// a row is ROW px high, its node in the middle.
import type { GitGraphEdge, GitGraphRow } from "../../../../../webproto/proto";

export const LANE = 16;
export const ROW = 24;
/** COLORS is how many lane colours the stylesheet has (--gg-0 …). */
export const COLORS = 8;

/** laneX is the middle of a column. */
export const laneX = (col: number): number => col * LANE + LANE / 2;

/** edgePath is one line from column e.f at y0 to column e.t at y1: a
 * straight line in its lane, or a soft curve to the other lane. */
export function edgePath(e: GitGraphEdge, y0: number, y1: number): string {
  const x0 = laneX(e.f), x1 = laneX(e.t);
  if (x0 === x1) return `M${x0} ${y0}V${y1}`;
  const m = (y0 + y1) / 2;
  return `M${x0} ${y0}C${x0} ${m} ${x1} ${m} ${x1} ${y1}`;
}

/** lanes is how many columns the rows need. */
export function lanes(rows: readonly GitGraphRow[]): number {
  let n = 1;
  for (const r of rows) {
    n = Math.max(n, r.col + 1);
    for (const e of [...r.up, ...r.down]) n = Math.max(n, e.f + 1, e.t + 1);
  }
  return n;
}

/** colorClass is the class of a lane colour. */
export const colorClass = (c: number): string => "gg-c" + (((c % COLORS) + COLORS) % COLORS);

/** readGraph checks a git.graph answer (anything odd: no rows). */
export function readGraph(r: unknown): { rows: GitGraphRow[]; more: boolean } {
  const g = (r ?? {}) as { rows?: unknown; more?: unknown };
  const rows = Array.isArray(g.rows) ? (g.rows as GitGraphRow[]).filter((x) => typeof x?.hash === "string" && Array.isArray(x.up) && Array.isArray(x.down)) : [];
  return { rows, more: g.more === true };
}
