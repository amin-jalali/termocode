// The projects list of the phone (MOBUX-6): one card per project the
// laptop knows (the projects topic, MOB-8; other workspaces reach the
// phone through the door, MOB-50). A card shows the name, the state as a
// colour and a shape (● needs you, ◐ working, ✓ quiet, ◌ asleep), the
// open task count and the CI result when the laptop sends them.
// Pure: no signals, no DOM; the topic data is read defensively, so a
// field the laptop does not send yet is simply not drawn.
import type { Status } from "./status";

/** CI is a project's last CI result, drawn as a shape and a word. */
export type CI = "passed" | "failed" | "running";

export interface ProjectCard {
  id: string;
  name: string;
  /** where is the folder as the laptop sends it (~-relative, never absolute). */
  where: string;
  status: Status;
  /** here: the project this page is attached to. */
  here: boolean;
  working: number;
  waiting: number;
  /** tasks is the open task count; undefined when the laptop does not know it. */
  tasks?: number;
  ci?: CI;
}

const num = (v: unknown): number => (typeof v === "number" && Number.isFinite(v) && v > 0 ? Math.floor(v) : 0);
const text = (v: unknown): string => (typeof v === "string" ? v : "");

/** ciOf maps the laptop's CI words onto the three the card draws. */
export function ciOf(v: unknown): CI | undefined {
  switch (v) {
    case "pass":
    case "passed":
    case "ok":
    case "success":
      return "passed";
    case "fail":
    case "failed":
    case "failure":
    case "error":
      return "failed";
    case "running":
    case "pending":
    case "queued":
      return "running";
    default:
      return undefined;
  }
}

/** statusOf is the card's state: what needs the user first, then work,
 * then a quiet open project (✓), then one that is not running (◌). */
export function statusOf(state: string, working: number, waiting: number): Status {
  if (waiting > 0) return "needs";
  if (working > 0) return "working";
  if (state === "asleep" || state === "") return "asleep";
  return "done";
}

/** RANK orders the cards: needs you, working, quiet, asleep. */
const RANK: Partial<Record<Status, number>> = { needs: 0, working: 1, done: 2, asleep: 3 };

/** absolute says a path would show the laptop's folder layout; the card
 * then shows the name only (the laptop sends ~-relative paths). */
function absolute(p: string): boolean {
  return p.startsWith("/") || /^[A-Za-z]:[\\/]/.test(p) || p.startsWith("\\\\");
}

/** projectCards turns the projects topic into the list's cards, sorted
 * by state, then by name; an item without an id or a name is dropped. */
export function projectCards(data: unknown): ProjectCard[] {
  const d = (typeof data === "object" && data !== null ? data : {}) as { projects?: unknown };
  const list = Array.isArray(d.projects) ? (d.projects as unknown[]) : [];
  const out: ProjectCard[] = [];
  for (const raw of list) {
    if (typeof raw !== "object" || raw === null) continue;
    const p = raw as Record<string, unknown>;
    const id = text(p.id), name = text(p.name);
    if (!id || !name) continue;
    const working = num(p.working), waiting = num(p.waiting);
    const where = text(p.where);
    const card: ProjectCard = {
      id,
      name,
      where: absolute(where) ? "" : where,
      status: statusOf(text(p.state), working, waiting),
      here: p.state === "here" || p.attached === true,
      working,
      waiting,
    };
    if (typeof p.tasks === "number") card.tasks = num(p.tasks);
    const ci = ciOf(p.ci);
    if (ci) card.ci = ci;
    out.push(card);
  }
  return out.sort((a, b) => {
    const r = (RANK[a.status] ?? 9) - (RANK[b.status] ?? 9);
    if (r !== 0) return r;
    const n = a.name.localeCompare(b.name);
    return n !== 0 ? n : a.id < b.id ? -1 : a.id > b.id ? 1 : 0;
  });
}

/** CI_LOOK is the CI result's shape and colour (never colour alone). */
export const CI_LOOK: Readonly<Record<CI, Status>> = { passed: "done", failed: "failed", running: "working" };
