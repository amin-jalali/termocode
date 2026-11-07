// The phone's saved views (MOBUX-18) as plain data: reading the laptop's
// list (view.list, ev views_saved) defensively, the pinned ones in their
// order, the name checks the sheet makes before it asks the laptop, and
// which calm line a refusal gets. No Preact and no session here: the
// Home's views (saved.tsx) hold the signals the page gives them.
import type { SavedView, SavedViewList, ViewSpec } from "../../../../webproto/proto";
import type { SVKey } from "./savedstrings";

export const MAX_VIEW_NAME = 60;

const isObj = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null && !Array.isArray(v);

/** readSavedList reads a view.list answer or a views_saved event; an
 * entry without an id, a name or a spec is left out. */
export function readSavedList(data: unknown): SavedViewList {
  const out: SavedViewList = { items: [], max: 32, max_pinned: 8 };
  if (!isObj(data)) return out;
  if (typeof data.max === "number") out.max = data.max;
  if (typeof data.max_pinned === "number") out.max_pinned = data.max_pinned;
  const items = Array.isArray(data.items) ? data.items : [];
  for (const v of items) {
    if (!isObj(v) || typeof v.id !== "string" || typeof v.name !== "string" || !isObj(v.spec) || typeof v.spec.view !== "string") continue;
    out.items.push({
      id: v.id,
      name: v.name,
      spec: v.spec as unknown as ViewSpec,
      pinned: v.pinned === true,
      order: typeof v.order === "number" ? v.order : 0,
      saved: typeof v.saved === "number" ? v.saved : 0,
      by: typeof v.by === "string" ? v.by : undefined,
    });
  }
  return out;
}

/** pinnedViews are the views on Home, first to last. */
export function pinnedViews(l: SavedViewList): SavedView[] {
  return l.items.filter((v) => v.pinned).sort((a, b) => (a.order ?? 0) - (b.order ?? 0));
}

/** sameName finds the saved view of that name (any case), except one id. */
export function sameName(l: SavedViewList, name: string, except = ""): SavedView | undefined {
  const n = name.trim().toLowerCase();
  return l.items.find((v) => v.id !== except && v.name.toLowerCase() === n);
}

/** nameOK says whether the laptop takes this as a view name. */
export function nameOK(name: string): boolean {
  const n = name.trim();
  return n !== "" && [...n].length <= MAX_VIEW_NAME && !/[\u0000-\u001f\u007f]/.test(n);
}

/** canPin says whether one more view fits on Home (a view already
 * there always fits). */
export function canPin(l: SavedViewList, id = ""): boolean {
  const pins = pinnedViews(l);
  return pins.some((v) => v.id === id) || pins.length < l.max_pinned;
}

/** refusal is the line for a refused change, by error code ("" when the
 * page's own line for the error fits better). */
export function refusal(code: string, op: "save" | "pin" | "rename" | "delete"): SVKey | "" {
  switch (code) {
    case "limit":
      return op === "save" ? "err.full" : "err.pins";
    case "conflict":
      return "err.name";
    case "not_found":
      return "err.gone";
  }
  return "";
}

/** defaultName is the name the Save sheet starts with: the spec's own
 * title, the block's title, or the view's id. */
export function defaultName(spec: ViewSpec, title?: string): string {
  const n = (spec.title || title || spec.view).trim();
  return [...n].slice(0, MAX_VIEW_NAME).join("");
}
