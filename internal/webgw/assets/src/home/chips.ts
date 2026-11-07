// The Home's chips (MOB-4, mobile plan 2.1). A chip never spends tokens:
// it runs a view on the laptop (view.run, the view registry of MOB-9),
// or, while the laptop has no such view, the page builds the block from
// a topic it reads (build), or the chip fills the box with a request the
// user can edit and send (fill). "Views" opens the Panels drawer at the
// views. MOBUX-3 orders the chips by state and lets the user pin three.
import type { HomeKey } from "./strings";

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
