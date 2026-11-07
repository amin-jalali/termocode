// The live board's pure parts (MOB-18, MOBUX-9): its columns, the shape
// of each state (color is never alone), the meta line of a tile and
// where a tap goes. No DOM, no network.
import type { Board, BoardItem } from "../../../../../webproto/proto";
import type { Route } from "../../state/route";
import type { StringKey } from "../../strings";

export const COLUMNS = ["needs_you", "working", "review", "done"] as const;
export type Column = (typeof COLUMNS)[number];

/** COLUMN_KEY is each column's title. */
export const COLUMN_KEY: Record<Column, StringKey> = {
  needs_you: "wfv.needs",
  working: "board.col.working",
  review: "pill.review",
  done: "board.col.done",
};

/** SHAPE draws a state with a shape as well as a color. */
export const SHAPE: Record<BoardItem["state"], string> = { wait: "▲", run: "◐", ok: "✓", fail: "✗", idle: "○" };

/** STATE_KEY says a state in words (screen readers). */
export const STATE_KEY: Record<BoardItem["state"], StringKey> = {
  wait: "board.st.wait",
  run: "strip.ai_working",
  ok: "tasks.state.ok",
  fail: "runs.state.failed",
  idle: "runs.state.cancelled",
};

/** KIND_KEY names where a tile comes from. */
export const KIND_KEY: Record<BoardItem["kind"], StringKey> = {
  card: "board.kind.card",
  run: "board.kind.run",
  lane: "board.kind.lane",
  workflow: "board.kind.workflow",
};

/** byColumn splits the board's items by column, in the laptop's order. */
export function byColumn(b: Board): Record<Column, BoardItem[]> {
  const out: Record<Column, BoardItem[]> = { needs_you: [], working: [], review: [], done: [] };
  for (const it of b.items ?? []) out[it.column]?.push(it);
  return out;
}

/** usd writes a cost as $0.80. */
export function usd(v: number): string {
  return "$" + v.toFixed(2);
}

/** openRoute is the view a tap on a tile opens: the card itself, or the
 * view of its source (runs, agent lanes, workflows), where every action
 * keeps its own rules. */
export function openRoute(it: BoardItem): Route {
  switch (it.kind) {
    case "card":
      return { tab: "chat", rest: "", query: {}, card: it.ref };
    case "run":
      return { tab: "more", rest: "runs", query: {} };
    case "lane":
      return { tab: "more", rest: "agents", query: {} };
    default:
      return { tab: "more", rest: "workflows", query: {} };
  }
}
