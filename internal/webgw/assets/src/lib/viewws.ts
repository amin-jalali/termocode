// Views across projects (MOB-57), the page's half. A view spec carries
// its project's workspace key (ws, "" = home). The page puts that key on
// the view's frames, so the door (MOB-50) routes them to that project's
// hub; it never sends a path. A pinned view of a project that does not
// run shows "asleep — Open" instead of data; the Go twin of
// viewWSState is webproto.ViewWSState (same rules, same words).
// English only (owner 2026-11-07).
import type { ProjectItem } from "../../../../webproto/proto";

export type ViewWSState = "live" | "asleep" | "unknown";

const WS_KEY = /^[0-9a-f]{16}$/;

/** A spec's ws is none or a 16-hex workspace key; anything else is dropped. */
export function specWS(ws: string | undefined): string {
  return ws && WS_KEY.test(ws) ? ws : "";
}

/** The ws to put on a view's frame: "" for the home project (no field). */
export function frameWS(ws: string | undefined, home: string): string {
  const k = specWS(ws);
  return k === home ? "" : k;
}

/** Whether a view of project ws can run now. */
export function viewWSState(projects: readonly ProjectItem[], home: string, ws: string | undefined): ViewWSState {
  const k = specWS(ws);
  if (k === "" || k === home) return "live";
  const p = projects.find((x) => x.ws === k);
  if (!p) return "unknown";
  return p.state === "here" || p.state === "running" ? "live" : "asleep";
}

/** The project's name for a pinned view's header ("" for home). */
export function viewWSName(projects: readonly ProjectItem[], home: string, ws: string | undefined): string {
  const k = specWS(ws);
  if (k === "" || k === home) return "";
  return projects.find((x) => x.ws === k)?.name ?? "";
}

/** The words a pinned view shows instead of data. Safe action first. */
export const VIEW_WS_TEXT = {
  asleep: "asleep",
  open: "Open",
  asleepLine: (name: string) => (name ? `${name} is asleep — Open` : "asleep — Open"),
  unknown: "Project gone. Remove this view or pick a project.",
  gone: "The project stopped. Showing the last data.",
} as const;

/** Projects for the Views menu's project picker: home first, then running, then asleep, by name. */
export function pickerProjects(projects: readonly ProjectItem[]): ProjectItem[] {
  const rank = (p: ProjectItem) => (p.state === "here" ? 0 : p.state === "running" ? 1 : 2);
  return [...projects].sort((a, b) => rank(a) - rank(b) || a.name.localeCompare(b.name));
}
