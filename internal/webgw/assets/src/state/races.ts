// The races topic as the phone reads it (MOB-61, mobile plan §3.9): the
// race compare block is one row per lane (state shape, files, lines,
// verify, cost, ★ for the merged lane). Pure: no signals, no DOM.
import type { RaceItem, RaceLane, RacesSnap } from "../../../../webproto/proto";

/** readRaces is the topic's data, with an empty list for a missing one. */
export function readRaces(data: unknown): { state: RacesSnap["state"]; races: RaceItem[] } {
  const d = (typeof data === "object" && data !== null ? data : {}) as Partial<RacesSnap>;
  const state = d.state === "none" || d.state === "error" || d.state === "ready" ? d.state : "loading";
  return { state, races: Array.isArray(d.races) ? d.races : [] };
}

/** Each state has a shape as well as a color (MOBUX rules). */
const laneMarks: Record<RaceLane["state"], string> = {
  starting: "◌",
  working: "◐",
  waiting: "◉",
  done: "✓",
  stopped: "■",
  failed: "✗",
  merged: "★",
  discarded: "○",
};

export function laneMark(state: string): string {
  return laneMarks[state as RaceLane["state"]] ?? "·";
}

const raceMarks: Record<RaceItem["state"], string> = { working: "◐", done: "✓", closed: "●" };

export function raceMark(state: string): string {
  return raceMarks[state as RaceItem["state"]] ?? "·";
}

/** CompareRow is one lane of the race compare block. */
export interface CompareRow {
  id: string;
  mark: string;
  state: RaceLane["state"];
  title: string;
  files: string; // "3", "0" (no changes), "–" (not read yet)
  lines: string; // "+12 −3", "" before the files were read
  verify: "pass" | "fail" | "running" | "";
  exit: number;
  cost: string; // "$0.42", "" unknown
  winner: boolean;
}

/** compareRows is the compare block of race r, in lane order. */
export function compareRows(r: RaceItem): CompareRow[] {
  return (r.lanes ?? []).map((l) => ({
    id: l.id,
    mark: laneMark(l.state),
    state: l.state,
    title: l.title || l.agent,
    files: l.stats ? String(l.files ?? 0) : "–",
    lines: l.stats ? `+${l.add ?? 0} −${l.del ?? 0}` : "",
    verify: l.verify ?? "",
    exit: l.exit ?? 0,
    cost: l.cost ?? "",
    winner: r.winner === l.id,
  }));
}

/** racesBadge counts the races that still work and the lanes that wait
 * for an answer. */
export function racesBadge(data: unknown): { working: number; waiting: number } {
  const out = { working: 0, waiting: 0 };
  for (const r of readRaces(data).races) {
    if (r.state === "working") out.working++;
    for (const l of r.lanes ?? []) if (l.state === "waiting") out.waiting++;
  }
  return out;
}
