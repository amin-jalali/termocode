// Saved and pinned views on the phone (MOB-71): the laptop keeps them
// (views.json for the views, shared by every paired phone; this device's
// pins on its devices.json entry), the page only asks. view.list on each
// connect; save, delete and pin answer with what changed. Specs only,
// never data: a pinned view re-runs live through view.run (MOB-9).
import { signal } from "@preact/signals";
import type { SavedView, ViewList } from "../../../../webproto/proto";
import { link } from "../session";

/** saved is the laptop's list (null: not read yet). */
export const saved = signal<ViewList | null>(null);

/** loadSaved reads the saved views and this device's pins. */
export function loadSaved(): Promise<ViewList> {
  return link.request("view.list").then((d) => {
    const l = d as ViewList;
    saved.value = { views: l.views ?? [], pins: l.pins ?? [] };
    return saved.value;
  });
}

/** pinnedViews are this device's pinned views, in Home order. */
export function pinnedViews(l: ViewList | null): SavedView[] {
  if (!l) return [];
  const byId = new Map(l.views.map((v) => [v.id, v] as const));
  const out: SavedView[] = [];
  for (const id of l.pins) {
    const v = byId.get(id);
    if (v) out.push(v);
  }
  return out;
}

/** saveView keeps spec as name (an existing name is replaced); pin also
 * pins it to this phone's Home. */
export function saveView(name: string, spec: unknown, pin: boolean, id?: string): Promise<SavedView> {
  return link.request("view.save", { ...(id ? { id } : {}), name, spec, ...(pin ? { pin } : {}) }).then((d) => {
    void loadSaved();
    return d as SavedView;
  });
}

/** deleteView forgets a saved view on the laptop (every phone's pins of
 * it go too). The caller asks first, Cancel first. */
export function deleteView(id: string): Promise<void> {
  return link.request("view.delete", { id }).then(() => {
    void loadSaved();
  });
}

/** pinView pins or unpins a saved view on this phone's Home. */
export function pinView(id: string, on: boolean, order?: number): Promise<void> {
  return link.request("view.pin", { id, on, ...(order ? { order } : {}) }).then((d) => {
    const l = d as ViewList;
    saved.value = { views: l.views ?? [], pins: l.pins ?? [] };
  });
}
