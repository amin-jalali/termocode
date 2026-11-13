// The session picker's rows (WEB-55): the owner's running termocode
// sessions on this laptop, from the projects topic. Only a session that
// runs is a row ("here" is the one this page is attached to); a sleeping
// or opening project is not: the page can pick a session, never create
// one. Pure: no signals, no DOM.
import type { ProjectItem } from "../../../../webproto/proto";

export interface SessionRow {
  id: string;
  name: string;
  /** where is the folder as the laptop sends it (~-relative). */
  where: string;
  branch: string;
  here: boolean;
  waiting: number;
  working: number;
  /** openedAt is the last time the folder was opened (Unix ms); 0 unknown. */
  openedAt: number;
}

const num = (v: unknown): number => (typeof v === "number" && Number.isFinite(v) && v > 0 ? Math.floor(v) : 0);

/** pickerRows turns the projects topic into the picker's rows: this
 * session first, then the ones that wait for you, the working ones and
 * the newest. */
export function pickerRows(data: unknown): SessionRow[] {
  const rows: SessionRow[] = [];
  const v = typeof data === "object" && data !== null ? (data as { projects?: unknown }).projects : undefined;
  const list = Array.isArray(v) ? (v as unknown[]) : [];
  for (const raw of list) {
    if (typeof raw !== "object" || raw === null) continue;
    const p = raw as ProjectItem;
    if (typeof p.id !== "string" || typeof p.name !== "string") continue;
    if (!(p.state === "here" || p.state === "running")) continue;
    rows.push({
      id: p.id,
      name: p.name,
      where: typeof p.where === "string" && !p.where.startsWith("/") ? p.where : "",
      branch: typeof p.branch === "string" ? p.branch : "",
      here: p.state === "here",
      waiting: num(p.waiting),
      working: num(p.working),
      openedAt: num(p.opened_at),
    });
  }
  const rank = (r: SessionRow) => (r.here ? 0 : r.waiting > 0 ? 1 : r.working > 0 ? 2 : 3);
  return rows.sort((a, b) => rank(a) - rank(b) || b.openedAt - a.openedAt || a.name.localeCompare(b.name));
}

/** ago says how long ago ms was, as a string key and a count ("" key: unknown). */
export function ago(ms: number, now: number): { key: "sp.ago_now" | "sp.ago_min" | "sp.ago_hour" | "sp.ago_day" | ""; n: number } {
  if (ms <= 0) return { key: "", n: 0 };
  const s = Math.max(0, Math.floor((now - ms) / 1000));
  if (s < 60) return { key: "sp.ago_now", n: 0 };
  if (s < 3600) return { key: "sp.ago_min", n: Math.floor(s / 60) };
  if (s < 86400) return { key: "sp.ago_hour", n: Math.floor(s / 3600) };
  return { key: "sp.ago_day", n: Math.floor(s / 86400) };
}
