// The phone's task action sheet (MOBUX-8, mobile plan 2.4) as data: what
// a task offers, in order, and the rough cost and lane count shown before
// anything starts. Pure: no signals, no DOM, no network.
//
// Action kinds: ask fills the chat box (nothing runs until Send), card
// is a laptop card (Cancel first), op starts at once from the sheet's own
// confirm. A viewer sees only Talk about it.
import type { RunItem } from "../../../../webproto/proto";
import type { StringKey } from "../strings";

/** The race limits of ai.race_start (MOB-62). */
export const RACE_MIN_LANES = 2;
export const RACE_MAX_LANES = 4;

export type TaskActionID = "talk" | "workflow" | "agents" | "models" | "start";

export interface TaskAction {
  id: TaskActionID;
  kind: "ask" | "card" | "op";
  label: StringKey;
  viewer?: boolean;
}

// phone_tasksheet_test.go reads the rows below, so keep one row per line.
export const TASK_ACTIONS: readonly TaskAction[] = [
  { id: "talk", kind: "ask", label: "pp.talk", viewer: true },
  { id: "workflow", kind: "card", label: "ph.task.workflow" },
  { id: "agents", kind: "card", label: "ph.task.agents" },
  { id: "models", kind: "ask", label: "ph.task.models" },
  { id: "start", kind: "op", label: "ph.task.start" },
];

/** HINTS is the one line under each action. */
export const HINTS: Readonly<Record<TaskActionID, StringKey>> = {
  talk: "ph.task.talk_hint",
  workflow: "ph.task.workflow_hint",
  agents: "ph.task.agents_hint",
  models: "ph.task.models_hint",
  start: "ph.task.start_hint",
};

/** actionsFor is what a role may see: a viewer only refers to the task. */
export function actionsFor(viewer: boolean): readonly TaskAction[] {
  return viewer ? TASK_ACTIONS.filter((a) => a.viewer) : TASK_ACTIONS;
}

/** TaskLike is the part of a project task (MOBUX-7, project.tasks of
 * MOB-52) the sheet needs: never a file's contents. */
export interface TaskLike {
  id: string;
  kind: string; // ptask | issue | pr | mr
  title: string;
  number?: number;
  path?: string;
  line?: number;
}

/** RefWords are the strings taskRef uses (passed in, so this file stays pure). */
export interface RefWords {
  issue(n: number): string;
  pr(n: number): string;
  mr(n: number): string;
}

const MAX_REF_TITLE = 120;

/** taskRef is the short reference the chat gets: where the task lives
 * and its title, one line, quoted. */
export function taskRef(task: TaskLike, w: RefWords): string {
  const raw = task.title.replace(/\s+/g, " ").trim();
  const title = raw.length > MAX_REF_TITLE ? raw.slice(0, MAX_REF_TITLE - 1) + "…" : raw;
  const n = task.number ?? 0;
  let where: string;
  switch (task.kind) {
    case "issue":
      where = w.issue(n);
      break;
    case "pr":
      where = w.pr(n);
      break;
    case "mr":
      where = w.mr(n);
      break;
    default:
      where = task.path ? (task.line ? `${task.path}:${task.line}` : task.path) : task.id;
  }
  return title ? `${where} “${title}”` : where;
}

const RECENT_RUNS = 20;

/** typicalRunUSD is the median cost of the newest finished runs with a
 * known cost (null: none yet). One AI run is one lane. */
export function typicalRunUSD(runs: readonly RunItem[]): number | null {
  const costs = runs
    .filter((r) => r.state === "done" && typeof r.cost_usd === "number" && r.cost_usd > 0)
    .slice(0, RECENT_RUNS)
    .map((r) => r.cost_usd as number)
    .sort((a, b) => a - b);
  if (costs.length === 0) return null;
  const mid = Math.floor(costs.length / 2);
  return costs.length % 2 ? costs[mid] : (costs[mid - 1] + costs[mid]) / 2;
}

export interface Estimate {
  lanes: number;
  /** usd is the rough cost of all lanes; null: not known yet; 0: free (nothing runs). */
  usd: number | null;
}

/** estimate is the rough cost of starting lanes lanes of a run like the
 * recent ones. Talk about it runs nothing until Send: it is free. */
export function estimate(action: TaskActionID, lanes: number, runs: readonly RunItem[]): Estimate {
  if (action === "talk") return { lanes: 0, usd: 0 };
  const n = action === "agents" || action === "models" ? clampLanes(lanes) : 1;
  const one = typicalRunUSD(runs);
  return { lanes: n, usd: one === null ? null : one * n };
}

export function clampLanes(n: number): number {
  return Math.min(RACE_MAX_LANES, Math.max(RACE_MIN_LANES, Math.round(n) || RACE_MIN_LANES));
}

/** CostWords are the strings costLine uses. */
export interface CostWords {
  free: string;
  lanes(n: number): string;
  known(usd: string, lanes: string): string;
  unknown(lanes: string): string;
}

/** money shows a rough amount: cents under $10, whole dollars above. */
export function money(usd: number): string {
  return usd < 10 ? `$${usd.toFixed(2)}` : `$${Math.round(usd)}`;
}

/** costLine is the line shown before a start: "About $0.80 · 3 lanes". */
export function costLine(e: Estimate, w: CostWords): string {
  if (e.usd === 0 && e.lanes === 0) return w.free;
  const lanes = w.lanes(e.lanes);
  return e.usd === null ? w.unknown(lanes) : w.known(money(e.usd), lanes);
}

/** defaultLanes picks one agent per lane, in the laptop's order, the
 * first agent again when there are fewer agents than lanes. */
export function defaultLanes(agentIDs: readonly string[], lanes: number): string[] {
  if (agentIDs.length === 0) return [];
  return Array.from({ length: clampLanes(lanes) }, (_, i) => agentIDs[i % agentIDs.length]);
}

/** resizeLanes keeps the agents already picked and fills new lanes. */
export function resizeLanes(picked: readonly string[], agentIDs: readonly string[], lanes: number): string[] {
  const n = clampLanes(lanes);
  const fill = defaultLanes(agentIDs, RACE_MAX_LANES);
  return Array.from({ length: n }, (_, i) => picked[i] ?? fill[i] ?? "").filter((id) => id !== "");
}

/** toggleProfile adds or removes name, keeping at most RACE_MAX_LANES. */
export function toggleProfile(picked: readonly string[], name: string): string[] {
  if (picked.includes(name)) return picked.filter((p) => p !== name);
  return picked.length >= RACE_MAX_LANES ? [...picked] : [...picked, name];
}
