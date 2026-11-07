// The phone's live board (MOBUX-9, mobile plan §2.5 and §3.8): what the
// board.get op answers (webproto Board, MOB-18), sorted into its four
// columns and turned into tiles. Pure: no DOM, timers or network.
//
// The types mirror internal/webproto/data_board.go (MOB-18). `project`
// and `ci` are optional: board v1 is the home project only (the page
// fills the project name), and a lane's CI state comes when the hub adds
// it to BoardItem.
import type { Route } from "../state/route";
import type { Status } from "./status";

/** OP_BOARD_GET is the op that answers the board now. */
export const OP_BOARD_GET = "board.get";

/** BOARD_REFRESH_MS: the page asks again at most twice a second when a
 * topic the board is built from changes (plan §3.8, coalesced ≤ 2 / s). */
export const BOARD_REFRESH_MS = 500;

/** BOARD_TOPICS are the topics board.get reads; a change in one of them
 * means the board may have changed. */
export const BOARD_TOPICS = ["cards", "runs", "worktrees", "workflows"] as const;

export type BoardColumn = "needs_you" | "working" | "review" | "done";
export type BoardKind = "card" | "run" | "lane" | "workflow";
export type BoardState = "wait" | "run" | "ok" | "fail" | "idle";

/** BOARD_COLUMNS is the drawing order: what needs the user comes first. */
export const BOARD_COLUMNS: readonly BoardColumn[] = ["needs_you", "working", "review", "done"];

export interface BoardItem {
  id: string;
  kind: BoardKind;
  column: BoardColumn;
  state: BoardState;
  title: string;
  /** sub is the agent, model profile, branch or workflow. */
  sub?: string;
  /** why says why it waits. */
  why?: string;
  step?: number;
  steps?: number;
  cost_usd?: number;
  ahead?: number;
  dirty?: number;
  since?: number;
  /** ref is the item's id in its own topic. */
  ref: string;
  /** project is the project's name (board v2, MOB-54). */
  project?: string;
  /** ci is the state of the lane's checks, when known. */
  ci?: "run" | "ok" | "fail";
}

export interface BoardCounts {
  needs_you: number;
  working: number;
  review: number;
  done: number;
}

export interface Board {
  items: BoardItem[];
  counts: BoardCounts;
  today_usd?: number;
  more?: number;
  built: number;
}

const isObj = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null && !Array.isArray(v);

/** readBoard checks the shape of a board.get answer (null: odd). */
export function readBoard(v: unknown): Board | null {
  if (!isObj(v) || !Array.isArray(v.items) || !isObj(v.counts)) return null;
  const items = (v.items as unknown[]).filter(
    (x): x is BoardItem =>
      isObj(x) &&
      typeof x.id === "string" &&
      typeof x.title === "string" &&
      typeof x.ref === "string" &&
      (BOARD_COLUMNS as readonly unknown[]).includes(x.column),
  );
  return { ...(v as unknown as Board), items };
}

/** columnsOf groups the items by column, every column present, the
 * board's own order kept inside each. */
export function columnsOf(b: Board | null): Record<BoardColumn, BoardItem[]> {
  const out: Record<BoardColumn, BoardItem[]> = { needs_you: [], working: [], review: [], done: [] };
  for (const it of b?.items ?? []) out[it.column].push(it);
  return out;
}

/** countOf is a column's count: the hub's count (it counts past the
 * item cap), else the items drawn. */
export function countOf(b: Board | null, col: BoardColumn): number {
  const n = b?.counts?.[col];
  return typeof n === "number" ? n : columnsOf(b)[col].length;
}

/** firstColumn is where the board opens: the first column with work,
 * needs you first; an empty board opens on needs you. */
export function firstColumn(b: Board | null): BoardColumn {
  return BOARD_COLUMNS.find((c) => countOf(b, c) > 0) ?? "needs_you";
}

/** statusOf is the phone status (colour + shape) of an item. */
export function statusOf(it: Pick<BoardItem, "column" | "state">): Status {
  switch (it.state) {
    case "wait":
      return "needs";
    case "run":
      return "working";
    case "fail":
      return "failed";
    case "idle":
      return "idle";
    case "ok":
      return it.column === "review" ? "review" : "done";
    default:
      return "idle";
  }
}

/** ciStatus is the status of a CI state. */
export function ciStatus(ci: BoardItem["ci"]): Status | undefined {
  if (ci === "ok") return "done";
  if (ci === "fail") return "failed";
  if (ci === "run") return "working";
  return undefined;
}

/** stepText writes the progress, like "3/5" ("" without steps). */
export function stepText(step?: number, steps?: number): string {
  if (!steps || steps < 1 || step === undefined || step < 0) return "";
  return `${step}/${steps}`;
}

/** costText writes a cost in dollars, like "$0.80" ("" when none). */
export function costText(usd?: number): string {
  if (typeof usd !== "number" || !isFinite(usd) || usd <= 0) return "";
  return usd < 0.01 ? "<$0.01" : `$${usd.toFixed(2)}`;
}

/** BoardTileData is what BoardTile draws for one item. */
export interface BoardTileData {
  status: Status;
  title: string;
  project?: string;
  agent?: string;
  step?: string;
  cost?: string;
  ci?: Status;
  live: boolean;
}

/** tileOf turns an item into a tile: project · agent/model · step · cost
 * · CI. home is the home project's name, used when the item has none. */
export function tileOf(it: BoardItem, home?: string): BoardTileData {
  const status = statusOf(it);
  return {
    status,
    title: it.title || it.ref,
    project: it.project || home || undefined,
    agent: it.sub || undefined,
    step: stepText(it.step, it.steps) || undefined,
    cost: costText(it.cost_usd) || undefined,
    ci: ciStatus(it.ci),
    live: status === "working",
  };
}

/** DrillKind is where a tap on a tile can go. */
export type DrillKind = "card" | "logs" | "diff" | "chat";

export interface Drill {
  kind: DrillKind;
  route: Route;
}

/** drillsOf lists where an item opens, most useful first: a card its
 * answer sheet; a run its logs and chat; a lane its diff, logs and chat;
 * a workflow run its logs (steps). The views read the id from the query. */
export function drillsOf(it: BoardItem): Drill[] {
  const id = it.ref;
  switch (it.kind) {
    case "card":
      return [{ kind: "card", route: { tab: "chat", rest: "", query: {}, card: id } }];
    case "run":
      return [
        { kind: "logs", route: { tab: "more", rest: "runs", query: { run: id } } },
        { kind: "chat", route: { tab: "chat", rest: "", query: { run: id } } },
      ];
    case "lane":
      return [
        { kind: "diff", route: { tab: "more", rest: "worktrees", query: { lane: id, show: "diff" } } },
        { kind: "logs", route: { tab: "more", rest: "worktrees", query: { lane: id, show: "logs" } } },
        { kind: "chat", route: { tab: "chat", rest: "", query: { lane: id } } },
      ];
    case "workflow":
      return [{ kind: "logs", route: { tab: "more", rest: "workflows", query: { run: id } } }];
    default:
      return [];
  }
}

/** columnAt is the column shown at a horizontal scroll offset of a strip
 * of equal columns width px wide (the one most in view). */
export function columnAt(scrollLeft: number, width: number): BoardColumn {
  if (!(width > 0)) return BOARD_COLUMNS[0];
  const i = Math.round(Math.abs(scrollLeft) / width);
  return BOARD_COLUMNS[Math.max(0, Math.min(BOARD_COLUMNS.length - 1, i))];
}
