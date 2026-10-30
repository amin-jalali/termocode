// The status topic as the shell reads it (web design 2.1). The hub sends
// the whole object; every field is optional here so an older or newer hub
// never breaks the strip.

export interface StatusView {
  /** root is the workspace folder's absolute path on the laptop (Copy Path). */
  root: string;
  workspace: string;
  branch: string;
  dirty: number;
  aiBusy: boolean;
  cardsWaiting: number;
  termCount: number;
  termBusy: number;
  runsLive: number;
  runsNeedYou: number;
  webClients: number;
}

function obj(v: unknown): Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v) ? (v as Record<string, unknown>) : {};
}

function num(v: unknown): number {
  return typeof v === "number" && Number.isFinite(v) && v >= 0 ? Math.floor(v) : 0;
}

function text(v: unknown, max = 80): string {
  return typeof v === "string" ? v.slice(0, max) : "";
}

/** readStatus turns raw status data (or nothing yet) into what the shell shows. */
export function readStatus(data: unknown): StatusView {
  const d = obj(data), ai = obj(d.ai), terms = obj(d.terminals), runs = obj(d.runs), web = obj(d.web);
  return {
    root: text(d.root, 4096),
    workspace: text(d.workspace_label),
    branch: text(d.branch),
    dirty: num(d.dirty_files),
    aiBusy: ai.busy === true,
    cardsWaiting: num(ai.cards_waiting),
    termCount: num(terms.count),
    termBusy: num(terms.busy),
    runsLive: num(runs.live),
    runsNeedYou: num(runs.needs_you),
    webClients: num(web.clients),
  };
}
