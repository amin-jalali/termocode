// Race compare data (MOBUX-10): the `races` topic (MOB-61) read into the
// phone's small table and lane cards. A race is one task raced by 2–4
// lanes, each an agent in its own worktree (internal/agentrace). Pure: no
// Preact, no session, no DOM.
//
// The shapes below follow internal/agentrace's JSON. They are local until
// the races topic lands in proto.d.ts (MOB-61); every field is read with
// care, so a missing or new field never breaks the view.
import type { Status } from "./status";

export interface RaceFile {
  status: string; // A, M or D
  path: string;
  add?: number;
  del?: number;
  binary?: boolean;
}

export interface RaceLane {
  id: string; // also the agent worktree id
  agent: string;
  title: string;
  branch?: string;
  state: string; // starting|working|waiting|done|stopped|failed|merged|discarded
  files?: RaceFile[];
  stats?: boolean; // files were read (an empty list is "no changes")
  verify?: { state?: string; exit?: number };
  cost?: number; // dollars; missing when the agent CLI does not say
  note?: string;
}

export interface Race {
  id: string;
  number: number;
  name: string;
  prompt: string;
  verify?: string;
  started_ms?: number;
  lanes: RaceLane[];
  summary?: string[];
  summary_cost?: string;
  winner?: string; // the merged lane
  /** judge is the lane the AI judge picked (MOB-62), when it says. */
  judge?: string;
}

const obj = (v: unknown): Record<string, unknown> => (typeof v === "object" && v !== null ? (v as Record<string, unknown>) : {});

function readLane(v: unknown): RaceLane | null {
  const l = obj(v);
  if (typeof l.id !== "string" || !l.id) return null;
  const files = Array.isArray(l.files) ? (l.files.filter((f) => typeof obj(f).path === "string") as RaceFile[]) : [];
  return {
    id: l.id,
    agent: typeof l.agent === "string" ? l.agent : "",
    title: typeof l.title === "string" && l.title ? l.title : l.id,
    branch: typeof l.branch === "string" ? l.branch : undefined,
    state: typeof l.state === "string" ? l.state : "",
    files,
    stats: l.stats === true,
    verify: obj(l.verify) as RaceLane["verify"],
    cost: typeof l.cost === "number" && isFinite(l.cost) ? l.cost : undefined,
    note: typeof l.note === "string" ? l.note : undefined,
  };
}

/** readRaces is the topic's races, newest first; a bad race is dropped. */
export function readRaces(data: unknown): Race[] {
  const d = obj(data);
  const list = Array.isArray(d.races) ? d.races : [];
  const out: Race[] = [];
  for (const v of list) {
    const r = obj(v);
    if (typeof r.id !== "string" || !Array.isArray(r.lanes)) continue;
    const lanes = r.lanes.map(readLane).filter((l): l is RaceLane => l !== null);
    out.push({
      id: r.id,
      number: typeof r.number === "number" ? r.number : 0,
      name: typeof r.name === "string" ? r.name : r.id,
      prompt: typeof r.prompt === "string" ? r.prompt : "",
      verify: typeof r.verify === "string" ? r.verify : undefined,
      started_ms: typeof r.started_ms === "number" ? r.started_ms : undefined,
      lanes,
      summary: Array.isArray(r.summary) ? r.summary.filter((s): s is string => typeof s === "string") : undefined,
      summary_cost: typeof r.summary_cost === "string" ? r.summary_cost : undefined,
      winner: typeof r.winner === "string" && r.winner ? r.winner : undefined,
      judge: typeof r.judge === "string" && r.judge ? r.judge : undefined,
    });
  }
  return out.sort((a, b) => b.number - a.number);
}

/** laneStatus is a lane's state as a phone status (colour + shape). */
export function laneStatus(l: RaceLane): Status {
  switch (l.state) {
    case "waiting":
      return "needs";
    case "starting":
    case "working":
      return "working";
    case "done":
    case "stopped":
      return l.verify?.state === "fail" ? "failed" : "review";
    case "merged":
      return "done";
    case "failed":
      return "failed";
  }
  return "idle"; // discarded, or a word this page does not know yet
}

/** testsStatus is the verify result of a lane: pass ✓, fail ✗, running ◐,
 * none ○ (no verify command, or not run yet). */
export function testsStatus(l: RaceLane): Status {
  switch (l.verify?.state) {
    case "pass":
      return "done";
    case "fail":
      return "failed";
    case "running":
      return "working";
  }
  return "idle";
}

/** costText is a lane's cost: "$0.42", or "—" when the agent does not say. */
export function costText(cost: number | undefined): string {
  if (cost === undefined) return "—";
  return "$" + (cost < 0.01 && cost > 0 ? "<0.01" : cost.toFixed(2));
}

/** canMerge: the lane is finished (done or stopped) with its worktree
 * still there, it changed something, and no lane of the race was merged.
 * The merge itself runs on the laptop after its card. */
export function canMerge(r: Race, l: RaceLane): boolean {
  if (r.winner || r.lanes.some((x) => x.state === "merged")) return false;
  const finished = l.state === "done" || l.state === "stopped";
  return finished && (!l.stats || (l.files?.length ?? 0) > 0);
}

/** judgeLane is the lane the judge picked: the race's judge field when the
 * laptop sends it, else the lane the AI summary names as the most complete
 * (the summary prompt asks it to say so). "" when there is none. */
export function judgeLane(r: Race): string {
  if (r.judge && r.lanes.some((l) => l.id === r.judge)) return r.judge;
  const pick = /\b(most complete|best|strongest|winner|recommend)/i;
  for (const line of r.summary ?? []) {
    if (!pick.test(line)) continue;
    const low = line.toLowerCase();
    // The longest title first, so "fix 2" wins over "fix".
    const named = [...r.lanes].sort((a, b) => b.title.length - a.title.length).find((l) => low.includes(l.title.toLowerCase()));
    if (named) return named.id;
  }
  return "";
}

/** RaceRow is one line of the table on top of the compare view. */
export interface RaceRow {
  id: string;
  title: string;
  agent: string;
  status: Status;
  files: number;
  add: number;
  del: number;
  /** filesKnown: the lane's files were read (else the cell is "—"). */
  filesKnown: boolean;
  tests: Status;
  cost: string;
  judge: boolean;
  merge: boolean;
}

/** raceRows is the table: one row per lane, in lane order. */
export function raceRows(r: Race): RaceRow[] {
  const pick = judgeLane(r);
  return r.lanes.map((l) => {
    let add = 0,
      del = 0;
    for (const f of l.files ?? []) {
      add += f.add ?? 0;
      del += f.del ?? 0;
    }
    return {
      id: l.id,
      title: l.title,
      agent: l.agent,
      status: laneStatus(l),
      files: l.files?.length ?? 0,
      add,
      del,
      filesKnown: l.stats === true || (l.files?.length ?? 0) > 0,
      tests: testsStatus(l),
      cost: costText(l.cost),
      judge: pick === l.id,
      merge: canMerge(r, l),
    };
  });
}

/** raceTotal is the cost of all lanes and the summary, "—" when no lane says. */
export function raceTotal(r: Race): string {
  const known = r.lanes.filter((l) => l.cost !== undefined);
  if (known.length === 0) return "—";
  return costText(known.reduce((s, l) => s + (l.cost ?? 0), 0));
}
