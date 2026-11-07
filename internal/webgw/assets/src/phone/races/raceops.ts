// Agent races from the phone (MOB-62, mobile plan 2.6): the rules of the
// race block's buttons and the four ops behind them. Start asks the
// laptop (a laptop card, Cancel first); Summarise spends the quick
// model's tokens only when tapped; Verify runs the race's own command
// (set on its start card, never typed here); Stop sends Ctrl+C into the
// working lanes, the work stays. Merge and Discard stay laptop cards.
// The race shapes match the races topic (MOB-61): only the fields used
// here are named, so any newer topic fields pass through.
import type { RaceKey } from "./strings";

/** The race ops (internal/webproto/ops_ai_wt_race.go). */
export const RACE_OPS = {
  start: "ai.race_start",
  summary: "ai.race_summary",
  verify: "ai.race_verify",
  stop: "ai.race_stop",
} as const;

/** Limits, as the laptop checks them. */
export const RACE_TASK_MAX = 2000;
export const RACE_VERIFY_MAX = 300;
export const RACE_MIN_LANES = 2;
export const RACE_MAX_LANES = 4;

export type LaneState = "starting" | "working" | "waiting" | "done" | "stopped" | "failed" | "merged" | "discarded";

export interface RaceLaneLike {
  id: string;
  state: LaneState | string;
  verify?: string;
}

export interface RaceLike {
  id: string;
  /** verify: the race has a verify command (the topic never sends it). */
  verify?: boolean;
  lanes: RaceLaneLike[];
}

/** working: the lane's agent may still work (it has a live tab). */
export function laneWorking(l: RaceLaneLike): boolean {
  return l.state === "starting" || l.state === "working" || l.state === "waiting";
}

/** laneFinished: the agent stopped and its worktree is there. */
export function laneFinished(l: RaceLaneLike): boolean {
  return l.state === "done" || l.state === "stopped";
}

/** canSummarise: two lanes to compare (finished or merged). */
export function canSummarise(r: RaceLike): boolean {
  return r.lanes.filter((l) => laneFinished(l) || l.state === "merged").length >= 2;
}

/** canVerify: a verify command and at least one finished lane. */
export function canVerify(r: RaceLike): boolean {
  return !!r.verify && r.lanes.some(laneFinished);
}

/** working counts the lanes Stop would stop. */
export function working(r: RaceLike): number {
  return r.lanes.filter(laneWorking).length;
}

/** hint says why Summarise or Verify is off ("" when on). */
export function hint(r: RaceLike, what: "summary" | "verify"): RaceKey | "" {
  if (what === "summary") return canSummarise(r) ? "" : "race.hint_summary";
  return r.verify ? "" : "race.hint_verify";
}

export interface StartArgs {
  task: string;
  agents: string[];
  verify?: string;
}

/** startArgs checks the start form as the laptop will and gives the op's
 * args, or the string key of what is wrong. Empty lane picks are left
 * out; the task is one line of words. */
export function startArgs(task: string, agents: readonly string[], verify: string): StartArgs | RaceKey {
  const tk = task.split(/\s+/).filter(Boolean).join(" ");
  if (!tk || tk.length > RACE_TASK_MAX) return "race.need_task";
  const picked = agents.map((a) => a.trim()).filter(Boolean);
  if (picked.length < RACE_MIN_LANES || picked.length > RACE_MAX_LANES) return "race.need_lanes";
  const v = verify.trim();
  if (/[\u0000-\u001f\u007f]/.test(v) || v.length > RACE_VERIFY_MAX) return "race.one_line";
  return v ? { task: tk, agents: picked, verify: v } : { task: tk, agents: picked };
}

/** Requester is what the ops need of the link. */
export interface Requester {
  request(op: string, args?: unknown): Promise<unknown>;
}

export interface RaceSummary {
  race: string;
  lines: string[];
  cost?: string;
}

export interface RaceDone {
  race: string;
  lanes: number;
  message: string;
}

export function summarise(link: Requester, race: string): Promise<RaceSummary> {
  return link.request(RACE_OPS.summary, { race }) as Promise<RaceSummary>;
}

export function verify(link: Requester, race: string): Promise<RaceDone> {
  return link.request(RACE_OPS.verify, { race }) as Promise<RaceDone>;
}

export function stop(link: Requester, race: string): Promise<RaceDone> {
  return link.request(RACE_OPS.stop, { race }) as Promise<RaceDone>;
}

/** start asks the laptop to start a race; it answers with its card. */
export function start(link: Requester, args: StartArgs): Promise<unknown> {
  return link.request(RACE_OPS.start, args);
}
