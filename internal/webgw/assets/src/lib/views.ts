// Views that features add to the desktop workbench without changing its
// layout (ui/Workbench.tsx): the side bar's view of an activity, and the
// bottom panel's tabs besides PROBLEMS and TERMINAL. A feature registers
// them when its module loads.
import type { ComponentChildren } from "preact";
import type { StringKey } from "../strings";

const sides = new Map<string, () => ComponentChildren>();

/** registerSide draws the side bar of activity id. */
export function registerSide(id: string, view: () => ComponentChildren): void {
  sides.set(id, view);
}

/** sideView is the registered side bar of activity id, if there is one. */
export function sideView(id: string): (() => ComponentChildren) | undefined {
  return sides.get(id);
}

/** TERMINAL_ORDER is the TERMINAL tab's place (PROBLEMS is 0). */
export const TERMINAL_ORDER = 50;

/** PanelDef is one tab of the bottom panel; order places it among
 * PROBLEMS (0) and TERMINAL (TERMINAL_ORDER). */
export interface PanelDef {
  id: string;
  label: StringKey;
  order: number;
  view: () => ComponentChildren;
  /** badge is a count shown on the tab (0: none). */
  badge?: () => number;
}

const panelDefs: PanelDef[] = [];

/** registerPanel adds a tab to the bottom panel. */
export function registerPanel(p: PanelDef): void {
  if (panelDefs.some((x) => x.id === p.id)) return;
  panelDefs.push(p);
  panelDefs.sort((a, b) => a.order - b.order);
}

/** panelViews are the registered tabs, in order. */
export function panelViews(): readonly PanelDef[] {
  return panelDefs;
}

/** panelsAround splits the registered tabs: those before TERMINAL and
 * those after it. */
export function panelsAround(): [PanelDef[], PanelDef[]] {
  return [panelDefs.filter((p) => p.order <= TERMINAL_ORDER), panelDefs.filter((p) => p.order > TERMINAL_ORDER)];
}
