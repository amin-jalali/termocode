// One project as the phone's project page reads it (MOBUX-7): its entry
// of the projects topic (MOB-8), its project.tasks sections (MOB-52) and
// the board's four columns over the runs topic. Every field is checked
// here, so a newer or older hub never breaks the page. Pure: no DOM,
// timers or network.
import type { RunView } from "./tasks";

type Obj = Record<string, unknown>;
const obj = (v: unknown): Obj => (typeof v === "object" && v !== null && !Array.isArray(v) ? (v as Obj) : {});
const num = (v: unknown): number => (typeof v === "number" && Number.isFinite(v) ? v : 0);
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

export interface ProjectInfo {
  id: string;
  name: string;
  where: string;
  /** state is the projects topic's: here (this laptop window), running, asleep … */
  state: string;
  /** here: the laptop window this page talks to has it open, so its topics are live. */
  here: boolean;
  working: number;
  waiting: number;
}

/** readProject finds id in the projects topic; null while it is not there. */
export function readProject(data: unknown, id: string): ProjectInfo | null {
  for (const p of list(obj(data).projects).map(obj)) {
    if (str(p.id) !== id) continue;
    const state = str(p.state);
    return {
      id,
      name: str(p.name) || id,
      where: str(p.where),
      state,
      here: state === "here",
      working: num(p.working),
      waiting: num(p.waiting),
    };
  }
  return null;
}

export type TaskKind = "ptask" | "issue" | "pr" | "mr";
export type TaskState = "open" | "done" | "draft";
export type Checks = "" | "none" | "pending" | "pass" | "fail";

export interface ProjectTaskView {
  id: string;
  kind: TaskKind;
  title: string;
  state: TaskState;
  number: number;
  path: string;
  line: number;
  group: string;
  depth: number;
  author: string;
  labels: string[];
  checks: Checks;
  branch: string;
}

export interface ProjectSectionView {
  kind: string;
  title: string;
  items: ProjectTaskView[];
  total: number;
  open: number;
  note: string;
}

const KINDS: ReadonlySet<string> = new Set(["ptask", "issue", "pr", "mr"]);
const STATES: ReadonlySet<string> = new Set(["open", "done", "draft"]);
const CHECKS: ReadonlySet<string> = new Set(["none", "pending", "pass", "fail"]);
const SECTIONS: ReadonlySet<string> = new Set(["checklist", "issues", "prs", "mrs", "mcp"]);

/** readProjectTasks reads a project.tasks result: its sections in the
 * hub's order (checklists first), each item checked field by field. */
export function readProjectTasks(data: unknown): ProjectSectionView[] {
  return list(obj(data).sections)
    .map(obj)
    .filter((s) => SECTIONS.has(str(s.kind)))
    .map((s) => {
      const items = list(s.items)
        .map(obj)
        .filter((x) => str(x.id) !== "" && str(x.title) !== "")
        .map((x): ProjectTaskView => {
          const kind = str(x.kind), state = str(x.state), checks = str(x.checks);
          return {
            id: str(x.id),
            kind: KINDS.has(kind) ? (kind as TaskKind) : "ptask",
            title: str(x.title),
            state: STATES.has(state) ? (state as TaskState) : "open",
            number: num(x.number),
            path: str(x.path),
            line: num(x.line),
            group: str(x.group),
            depth: Math.min(4, num(x.depth)),
            author: str(x.author),
            labels: list(x.labels).map(str).filter(Boolean).slice(0, 6),
            checks: CHECKS.has(checks) ? (checks as Checks) : "",
            branch: str(x.branch),
          };
        });
      return {
        kind: str(s.kind),
        title: str(s.title) || str(s.kind),
        items,
        total: Math.max(num(s.total), items.length),
        open: num(s.open),
        note: str(s.note),
      };
    });
}

/** taskMark is a task's state as a shape (status is never colour alone). */
export function taskMark(t: ProjectTaskView): "✓" | "◐" | "○" {
  return t.state === "done" ? "✓" : t.state === "draft" ? "◐" : "○";
}

/** checksMark is a pull request's checks as a shape. */
export function checksMark(c: Checks): string {
  return c === "pass" ? "✓" : c === "fail" ? "✗" : c === "pending" ? "◐" : "";
}

/** taskRef is what "Talk about it" puts in the chat box: the project and
 * where the task is (file and line, or its number), never more. */
export function taskRef(project: string, t: ProjectTaskView): string {
  const where =
    t.kind === "ptask" ? `${t.path}:${t.line}` : t.kind === "issue" ? `issue #${t.number}` : t.kind === "pr" ? `pull request #${t.number}` : `merge request !${t.number}`;
  return `[${project} · ${where}] ${t.title}\n`;
}

export const BOARD_COLS = ["needs", "working", "review", "done"] as const;
export type BoardCol = (typeof BOARD_COLS)[number];

/** boardCol puts a run in a board column: one that waits for the user
 * needs you; a running, pausing or stalled one is working; a finished one
 * is done. Review holds the lanes that wait for a review (the board's
 * lanes, MOB-18); the runs topic has none. Failed and cancelled runs are
 * done too: the run card says how they ended. */
export function boardCol(r: RunView): BoardCol {
  if (r.needsYou) return "needs";
  if (r.state === "running" || r.state === "pausing" || r.state === "stalled") return "working";
  if (r.state === "paused" || r.state === "resumable") return "needs";
  return "done";
}

/** boardCounts counts runs by column. */
export function boardCounts(runs: readonly RunView[]): Record<BoardCol, RunView[]> {
  const out: Record<BoardCol, RunView[]> = { needs: [], working: [], review: [], done: [] };
  for (const r of runs) out[boardCol(r)].push(r);
  return out;
}
