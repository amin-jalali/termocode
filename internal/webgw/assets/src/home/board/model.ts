// The live board across projects (MOB-54, mobile plan 2.5 and 3.8), as
// the phone's Home draws it. Pure: no signals, no DOM, no network.
//
// Sources:
//   - board.projects (the home hub): one row per project whose window runs
//     agents, from the roll-ups; the home project first.
//   - the home project's own runs, worktrees, workflows and cards topics
//     (and, through the door with ws set, MOB-50, those of a running
//     project the user opened): the rows under its header.
// A project the door has no data for yet shows its roll-up agents only.
import type { BoardProject, BoardProjects, Card, RunItem, WorkflowRun, WorktreeItem } from "../../../../../webproto/proto";

/** Tone of a row: drawn with a shape too, never colour alone. */
export type BoardTone = "needs" | "working" | "done" | "failed" | "idle";

/** SHAPE is the shape that goes with each tone. */
export const SHAPE: Record<BoardTone, string> = { needs: "●", working: "◐", done: "✓", failed: "✗", idle: "○" };

/** BoardRow is one line under a project. */
export interface BoardRow {
  key: string;
  tone: BoardTone;
  kind: "card" | "run" | "lane" | "workflow" | "agent";
  title: string;
  detail: string;
  /** what a tap opens: a card id, a run key, a worktree id, a run id */
  ref: string;
}

/** ProjectData is what the page has of one project's topics. */
export interface ProjectData {
  runs?: readonly RunItem[];
  worktrees?: readonly WorktreeItem[];
  workflows?: readonly WorkflowRun[];
  cards?: readonly Card[];
}

/** BoardSection is one project with its rows. */
export interface BoardSection {
  ws: string;
  name: string;
  where: string;
  home: boolean;
  tone: BoardTone;
  needs: number;
  rows: BoardRow[];
  /** detailed: the rows come from the project's topics, not its roll-up */
  detailed: boolean;
  costUSD: number;
}

/** BoardFilter is the chip under the board: all, only needs you, only running. */
export type BoardFilter = "all" | "needs" | "running";

/** BoardView is the whole board. */
export interface BoardView {
  sections: BoardSection[];
  needs: number;
  costUSD: number;
  more: number;
}

const MAX_ROWS = 12; // rows per project; the rest open in the project

const TONE_RANK: Record<BoardTone, number> = { needs: 0, working: 1, failed: 2, done: 3, idle: 4 };

function money(usd: number): string {
  return "$" + usd.toFixed(2);
}

function runRow(r: RunItem): BoardRow {
  const tone: BoardTone = r.needs_you
    ? "needs"
    : r.state === "done"
      ? "done"
      : r.state === "failed" || r.state === "cancelled"
        ? "failed"
        : r.state === "running" || r.state === "stalled" || r.state === "pausing"
          ? "working"
          : "idle";
  const parts: string[] = [];
  if (r.steps > 0 && r.step > 0) parts.push("step " + r.step + "/" + r.steps);
  if (r.cost_usd) parts.push(money(r.cost_usd));
  if (r.why) parts.push(r.why);
  return { key: "run:" + r.key, tone, kind: "run", title: "run " + JSON.stringify(r.title), detail: parts.join(" · "), ref: r.key };
}

function laneRow(w: WorktreeItem): BoardRow | null {
  if (w.kind !== "agent" || w.state === "gone" || w.state === "archived") return null;
  const tone: BoardTone = w.state === "waiting" ? "needs" : w.state === "working" ? "working" : w.state === "done" ? "done" : "idle";
  const parts: string[] = [];
  if (w.agent) parts.push(w.agent);
  if (w.status && (w.ahead || w.dirty)) parts.push("+" + (w.ahead ?? 0) + " ahead · " + (w.dirty ?? 0) + " changed");
  return { key: "lane:" + w.id, tone, kind: "lane", title: "lane " + w.title, detail: parts.join(" · "), ref: w.id };
}

function workflowRow(r: WorkflowRun): BoardRow | null {
  if (r.state !== "running" && r.state !== "paused" && !r.waiting) return null;
  const tone: BoardTone = r.waiting ? "needs" : "working";
  const detail = r.waiting ? "waits: " + r.waiting : r.items ? r.items + " items" : "";
  return { key: "wf:" + r.id, tone, kind: "workflow", title: "workflow " + r.workflow, detail, ref: r.id };
}

function cardRow(c: Card): BoardRow {
  return { key: "card:" + c.id, tone: "needs", kind: "card", title: c.title, detail: c.read_only ? "answer on the laptop" : "", ref: c.id };
}

/** rowsOf builds a project's rows from its topics, needs-you first. */
export function rowsOf(d: ProjectData): BoardRow[] {
  const rows: BoardRow[] = [];
  for (const c of d.cards ?? []) rows.push(cardRow(c));
  for (const r of d.runs ?? []) {
    const row = runRow(r);
    if (row.tone !== "idle") rows.push(row);
  }
  for (const w of d.worktrees ?? []) {
    const row = laneRow(w);
    if (row) rows.push(row);
  }
  for (const r of d.workflows ?? []) {
    const row = workflowRow(r);
    if (row) rows.push(row);
  }
  return rows.sort((a, b) => TONE_RANK[a.tone] - TONE_RANK[b.tone]);
}

/** agentRows are a project's roll-up agents, when its topics are not here. */
function agentRows(p: BoardProject): BoardRow[] {
  const rows: BoardRow[] = (p.agents ?? []).map((a, i) => ({
    key: "agent:" + i,
    tone: a.state === "waiting" ? "needs" : "working",
    kind: "agent",
    title: a.name,
    detail: a.state === "waiting" ? "waits for you" : "working",
    ref: "",
  }));
  if (p.more) rows.push({ key: "agent:more", tone: "idle", kind: "agent", title: "+" + p.more + " more", detail: "", ref: "" });
  return rows;
}

function costOf(d: ProjectData | undefined): number {
  let usd = 0;
  for (const r of d?.runs ?? []) usd += r.cost_usd ?? 0;
  return usd;
}

/** buildBoard merges the project rows with the topics the page has, by
 * ws (data[ws]); the home project's topics are under list.home. */
export function buildBoard(list: BoardProjects | null, data: Readonly<Record<string, ProjectData>>, filter: BoardFilter): BoardView {
  const projects = list?.projects ?? [];
  const sections: BoardSection[] = [];
  let needs = 0;
  let cost = 0;
  for (const p of projects) {
    const d = data[p.ws];
    const detailed = d !== undefined;
    let rows = detailed ? rowsOf(d) : agentRows(p);
    const n = rows.filter((r) => r.tone === "needs").length || (detailed ? 0 : p.waiting ?? 0);
    const usd = costOf(d);
    needs += n;
    cost += usd;
    const tone: BoardTone = n > 0 ? "needs" : rows.some((r) => r.tone === "working") || (!detailed && p.state === "working") ? "working" : "idle";
    if (filter === "needs") rows = rows.filter((r) => r.tone === "needs");
    if (filter === "running") rows = rows.filter((r) => r.tone === "working");
    if (filter !== "all" && rows.length === 0 && !p.home) continue;
    sections.push({ ws: p.ws, name: p.name, where: p.where, home: !!p.home, tone, needs: n, rows: rows.slice(0, MAX_ROWS), detailed, costUSD: usd });
  }
  // home first, then needs you, then working, then by name (the laptop's order)
  sections.sort((a, b) => {
    if (a.home !== b.home) return a.home ? -1 : 1;
    if (TONE_RANK[a.tone] !== TONE_RANK[b.tone]) return TONE_RANK[a.tone] - TONE_RANK[b.tone];
    return a.name < b.name ? -1 : a.name > b.name ? 1 : 0;
  });
  return { sections, needs, costUSD: cost, more: list?.more ?? 0 };
}

/** sectionLine is a project header's short status: "● 1 needs you". */
export function sectionLine(s: BoardSection): string {
  if (s.needs > 0) return SHAPE.needs + " " + s.needs + " needs you";
  const working = s.rows.filter((r) => r.tone === "working").length;
  if (working > 0) return SHAPE.working + " " + working + " working";
  return SHAPE.idle + " idle";
}

export { money as boardMoney };
