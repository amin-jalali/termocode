// One project as the phone's project page reads it (MOBUX-7): its entry
// of the projects topic, the page's tabs, what "Talk about it" puts in
// the chat box, and the board's four columns over the runs topic. Every
// field is checked here, so a newer or older hub never breaks the page.
// Pure: no DOM, timers or network.
import type { ProjectTask } from "../../../../webproto/proto";
import type { RunView } from "./tasks";

type Obj = Record<string, unknown>;
const obj = (v: unknown): Obj => (typeof v === "object" && v !== null && !Array.isArray(v) ? (v as Obj) : {});
const str = (v: unknown): string => (typeof v === "string" ? v : "");
const list = (v: unknown): unknown[] => (Array.isArray(v) ? v : []);

/** PROJECT_TABS are the page's tabs, in order; the first is the default. */
export const PROJECT_TABS = ["tasks", "runs", "git", "board"] as const;
export type ProjectTab = (typeof PROJECT_TABS)[number];

/** projectTab reads the ?tab= of the route (anything else: Tasks). */
export function projectTab(q: string | undefined): ProjectTab {
  return (PROJECT_TABS as readonly string[]).includes(q ?? "") ? (q as ProjectTab) : "tasks";
}

/** validProjectID: a project id is lower-case hex (never a path). */
export function validProjectID(id: string): boolean {
  return /^[0-9a-f]{8,32}$/.test(id);
}

/** validWS: a workspace key is 16 lower-case hex digits (webproto.ValidWS). */
function validWS(ws: string): boolean {
  return /^[0-9a-f]{16}$/.test(ws);
}

export interface ProjectInfo {
  id: string;
  name: string;
  where: string;
  /** state is the projects topic's: here (this laptop window), running, asleep. */
  state: string;
  /** ws is what a frame names to reach it through the door: "" for the
   * project this page talks to (here) or when the laptop sent no key. */
  ws: string;
  here: boolean;
}

/** readProject finds id in the projects topic; null while it is not there. */
export function readProject(data: unknown, id: string): ProjectInfo | null {
  for (const p of list(obj(data).projects).map(obj)) {
    if (str(p.id) !== id) continue;
    const state = str(p.state);
    const here = state === "here";
    const ws = str(p.ws);
    return { id, name: str(p.name) || id, where: str(p.where), state, here, ws: !here && validWS(ws) ? ws : "" };
  }
  return null;
}

/** reachable: the page can show the project's live topics (Runs, Git,
 * Board): it is the open one, or a running one behind the door. */
export function reachable(p: ProjectInfo): boolean {
  return p.here || (p.ws !== "" && p.state === "running");
}

/** taskRef is what "Talk about it" puts in the chat box: the project and
 * where the task is (file and line, or its number), never more. */
export function taskRef(project: string, t: ProjectTask): string {
  const n = t.number ?? 0;
  const where =
    t.kind === "ptask" ? `${t.path ?? ""}:${t.line ?? 0}` : t.kind === "issue" ? `issue #${n}` : t.kind === "pr" ? `pull request #${n}` : `merge request !${n}`;
  return `[${project} · ${where}] ${t.title}\n`;
}

export const BOARD_COLS = ["needs", "working", "review", "done"] as const;
export type BoardCol = (typeof BOARD_COLS)[number];

/** boardCol puts a run in a board column: one that waits for the user
 * (or is paused) needs you; a running, pausing or stalled one is
 * working; a finished one is done (failed and cancelled too: the run
 * card says how it ended). Review holds lanes that wait for a review,
 * which the runs topic does not carry; the live board (MOB-18) fills it. */
export function boardCol(r: RunView): BoardCol {
  if (r.needsYou) return "needs";
  if (r.state === "running" || r.state === "pausing" || r.state === "stalled") return "working";
  if (r.state === "paused" || r.state === "resumable") return "needs";
  return "done";
}

/** boardCounts sorts runs into the board's columns. */
export function boardCounts(runs: readonly RunView[]): Record<BoardCol, RunView[]> {
  const out: Record<BoardCol, RunView[]> = { needs: [], working: [], review: [], done: [] };
  for (const r of runs) out[boardCol(r)].push(r);
  return out;
}
