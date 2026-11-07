// Opening and closing a project from the phone (MOB-51). The phone names
// a project only by its ws from the projects topic (never a path).
// project.open goes to the home hub and waits for a laptop card; a
// project that runs already answers "conflict" and needs nothing more:
// the door joins it. project.close goes to the project's own hub through
// the door and works only for a project the phone opened.
// Phone only: the projects list (MOBUX-6) imports it into the lazy phone
// chunk; the desktop app.js does not carry it.
import type { ProjectItem } from "../../../../webproto/proto";
import type { Door } from "../../net/door";
import { ReqError } from "../../net/link";
import { link } from "../../session";

/** canOpen: the row offers "Open" (nothing runs there). */
export function canOpen(p: ProjectItem): boolean {
  return p.state === "asleep" && !!p.ws;
}

/** openProject asks the laptop to open p; it answers with the laptop
 * card's id, or "" when p runs already (no card). */
export async function openProject(ws: string): Promise<string> {
  try {
    const r = (await link.request("project.open", { ws })) as { card?: unknown } | null;
    return String(r?.card ?? "");
  } catch (e) {
    if (e instanceof ReqError && e.code === "conflict") return "";
    throw e;
  }
}

/** closeProject stops a project the phone opened, on its own hub. */
export function closeProject(door: Door, ws: string): Promise<unknown> {
  return door.request(ws, "project.close", { ws });
}

/** Phone text (English only). */
export const projectText = {
  open: "Open",
  close: "Close",
  opening: "Opening…",
  asleep: "asleep",
  openLabel: (name: string) => `Open ${name}`,
  openQ: (name: string) =>
    `Open ${name} on the laptop for the phone? It starts without a window after your yes on the laptop, and stops by itself when no phone uses it.`,
  closeQ: (name: string) => `Close ${name}? It stops on the laptop. Unsaved files are kept for the next start.`,
};
