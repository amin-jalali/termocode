// The Home's chips (MOB-4, mobile plan 2.1). A chip never spends tokens:
// it runs a view on the laptop (view.run, the view registry of MOB-9),
// or, while the laptop has no such view, the page builds the block from
// a topic it reads (build), or the chip fills the box with a request the
// user can edit and send (fill). "Views" opens the Panels drawer at the
// views. MOBUX-3 orders the chips by state and lets the user pin three.
import type { HomeKey } from "./strings";
import type { StringKey } from "../strings";

/** Built names a page-side block builder (Home.tsx buildFor). */
export type Built = "needs" | "board" | "projects" | "tasks" | "git" | "runs";

export interface ChipDef {
  id: string;
  label: HomeKey;
  /** view is the laptop's view id (view.run). */
  view?: string;
  /** build is the page's fallback block; topics are what it reads. */
  build?: Built;
  topics?: readonly string[];
  /** fill is the request the chip puts in the box when there is no block. */
  fill?: HomeKey;
  /** panels: the chip opens the Panels drawer at the views. */
  panels?: boolean;
}

/** vid writes a view id, "<feature>.<name>". */
function vid(feature: string, name: string): string {
  return feature + "." + name;
}

export const CHIPS: readonly ChipDef[] = [
  { id: "needs", label: "chip.needs", view: "inbox", build: "needs", topics: ["inbox", "cards"] },
  { id: "board", label: "chip.board", view: "board", build: "board", topics: ["inbox"] },
  { id: "projects", label: "chip.projects", view: "projects", build: "projects", topics: ["projects"] },
  { id: "views", label: "chip.views", panels: true },
  { id: "tasks", label: "chip.tasks", view: "tasks", build: "tasks", topics: ["tasks"] },
  { id: "runs", label: "chip.runs", view: "runs", build: "runs", topics: ["runs"] },
  { id: "review", label: "chip.review", fill: "fill.review" },
  { id: "research", label: "chip.research", fill: "fill.research" },
  { id: "git", label: "chip.git", view: vid("git", "status"), build: "git", topics: ["git"] },
  { id: "mcp", label: "chip.mcp", view: vid("mcp", "status"), fill: "fill.mcp" },
  { id: "what", label: "chip.what", fill: "fill.what" },
];

/** chipOrder puts "Needs you" first when something waits, and keeps the
 * rest as listed. */
export function chipOrder(chips: readonly ChipDef[], waiting: number): ChipDef[] {
  if (waiting > 0) return [...chips];
  const needs = chips.filter((c) => c.id === "needs");
  return [...chips.filter((c) => c.id !== "needs"), ...needs];
}

/** viewChips are the chips the Views panel lists (they open a block). */
export function viewChips(chips: readonly ChipDef[]): ChipDef[] {
  return chips.filter((c) => c.view !== undefined && c.build !== undefined);
}

// ---- MOBUX-3 chip row (ChipRow.tsx): state order and pins. Its ChipDef
// and CHIPS are named RowChipDef and ROW_CHIPS here, next to the MOB-4
// chips above. Merge the two lists into one later.
// The Home's context chips (MOBUX-3, plan 2.1): one row above the
// composer that scrolls sideways. The order follows the state of the
// laptop, so the chip that matters now is under the thumb:
//   1. "Needs you · N" first while anything waits for an answer
//   2. the chips the user pinned (at most PIN_MAX), in pin order
//   3. chips the state raises (review waiting, agents working, CI red),
//      the bigger count first
//   4. every other chip, in its fixed place
// A chip never spends tokens: tapping one runs its default view on the
// laptop or fills the composer (MOB-4). Pins are a choice of this phone
// only (chip ids, never data), kept in localStorage and read through a
// validator; storage may be missing and the row then has no pins.

/** ChipId names one chip of the row. Saved views pinned to the Home
 * (MOBUX-18) come in as "view:<id>". */
export type ChipId = string;

/** RowChipDef is one chip the row can show. */
export interface RowChipDef {
  id: ChipId;
  /** label is the chip's string key; text is used instead when set
   * (a saved view's own name). */
  label?: StringKey;
  text?: string;
}

/** ChipState is what the laptop says right now (status + inbox topics). */
export interface ChipState {
  /** needsYou counts the cards, asks and runs waiting for an answer. */
  needsYou: number;
  /** working counts the agents and runs busy right now. */
  working: number;
  /** review counts the diffs and pull requests waiting for a review. */
  review: number;
  /** ciRed counts the red CI checks of the open branches. */
  ciRed: number;
}

/** Shape of a chip: color always comes with one of these (MOBUX rule). */
export type ChipMark = "waits" | "review" | "busy" | "bad" | "clear" | "";

/** ChipItem is one chip as drawn, in order. */
export interface ChipItem {
  def: RowChipDef;
  pinned: boolean;
  /** count is the number the chip shows (0: none). */
  count: number;
  mark: ChipMark;
}

export const PIN_MAX = 3;
export const NEEDS: ChipId = "needs";

/** ROW_CHIPS is the fixed row of the Home, in its resting order. */
export const ROW_CHIPS: readonly RowChipDef[] = [
  { id: NEEDS },
  { id: "board", label: "chips.board" },
  { id: "projects", label: "ph.switch_title" },
  { id: "views", label: "tabs.label" },
  { id: "tasks", label: "more.tasks" },
  { id: "review", label: "pill.review" },
  { id: "research", label: "chips.research" },
  { id: "git", label: "tab.git" },
  { id: "mcp", label: "chips.mcp" },
  { id: "whatcan", label: "chips.whatcan" },
];

const ID_RE = /^(?:[a-z]{2,16}|view:[a-zA-Z0-9_-]{1,64})$/;

/** validPins keeps the known-looking ids, once each, at most PIN_MAX. */
export function validPins(v: unknown): ChipId[] {
  if (!Array.isArray(v)) return [];
  const out: ChipId[] = [];
  for (const x of v) {
    if (typeof x === "string" && ID_RE.test(x) && x !== NEEDS && !out.includes(x)) out.push(x);
    if (out.length === PIN_MAX) break;
  }
  return out;
}

/** stateOf gives the count and shape a chip takes from the state. */
function stateOf(id: ChipId, s: ChipState): { count: number; mark: ChipMark } {
  switch (id) {
    case NEEDS:
      return s.needsYou > 0 ? { count: s.needsYou, mark: "waits" } : { count: 0, mark: "clear" };
    case "review":
      return s.review > 0 ? { count: s.review, mark: "review" } : { count: 0, mark: "" };
    case "board":
      return s.working > 0 ? { count: s.working, mark: "busy" } : { count: 0, mark: "" };
    case "git":
      return s.ciRed > 0 ? { count: s.ciRed, mark: "bad" } : { count: 0, mark: "" };
  }
  return { count: 0, mark: "" };
}

/** orderChips puts the chips in the order of the rules at the top.
 * Pins that name no chip are dropped; the sort is stable. */
export function orderChips(defs: readonly RowChipDef[], s: ChipState, pins: readonly ChipId[]): ChipItem[] {
  const byId = new Map(defs.map((d) => [d.id, d] as const));
  const item = (d: RowChipDef): ChipItem => ({ def: d, pinned: pins.includes(d.id), ...stateOf(d.id, s) });
  const out: ChipItem[] = [];
  const needs = byId.get(NEEDS);
  const waits = needs !== undefined && s.needsYou > 0;
  if (waits) out.push(item(needs));
  for (const id of validPins(pins)) {
    const d = byId.get(id);
    if (d) out.push(item(d));
  }
  const rest = defs.filter((d) => !out.some((o) => o.def.id === d.id) && !(d.id === NEEDS && !waits)).map(item);
  const raised = rest.filter((c) => c.count > 0).sort((a, b) => b.count - a.count);
  out.push(...raised, ...rest.filter((c) => c.count === 0));
  // Nothing waits: the calm "Nothing waits" chip rests at the end.
  if (needs && !waits) out.push(item(needs));
  return out;
}

/** PinResult is togglePin's answer; full says the pin was refused. */
export interface PinResult {
  pins: ChipId[];
  full: boolean;
}

/** togglePin pins id at the end of the pins or unpins it. A fourth pin
 * is refused (full), never dropping an older one by surprise. The
 * Needs you chip is never pinned: the state already puts it first. */
export function togglePin(pins: readonly ChipId[], id: ChipId): PinResult {
  const cur = validPins(pins);
  if (cur.includes(id)) return { pins: cur.filter((p) => p !== id), full: false };
  if (id === NEEDS || !ID_RE.test(id)) return { pins: cur, full: false };
  if (cur.length >= PIN_MAX) return { pins: cur, full: true };
  return { pins: [...cur, id], full: false };
}

const PINS_KEY = "termocode.chips.v1";

/** loadPins reads this phone's pins ([] when none or unreadable). */
export function loadPins(): ChipId[] {
  try {
    return validPins(JSON.parse(localStorage.getItem(PINS_KEY) ?? "[]") as unknown);
  } catch {
    return [];
  }
}

/** savePins stores this phone's pins; a blocked storage keeps them for this page. */
export function savePins(pins: readonly ChipId[]): void {
  try {
    localStorage.setItem(PINS_KEY, JSON.stringify(validPins(pins)));
  } catch {
    // Storage is full or blocked: the pins last for this page.
  }
}
