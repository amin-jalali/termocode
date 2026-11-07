// "What can I do?", the MCP chip and pinned tracker blocks on the
// phone's Home (MOB-13). Pure data: the Home hands in its host (the
// page's link, signals and composer) so this file brings no Preact or
// session of its own (MOB-4 rule for home.js). The laptop builds the
// catalog (ai.catalog: cached lists only, nothing starts) and runs the
// pins (ai.tracker_run: read-only tools only); the phone only shows.
// English only (owner 2026-11-07); the strings live here, in home.js,
// so app.js does not grow.
import type { AICatalog, CatalogServer, CatalogTool, TrackerBlock, TrackerPin, TrackerPins } from "../../../../webproto/proto";

/** CatalogHost is what the Home gives these views. */
export interface CatalogHost {
  request(op: string, args?: unknown): Promise<unknown>;
  signal<T>(v: T): { value: T };
  /** fill puts text in the Home composer; nothing is sent. */
  fill(text: string): void;
  /** openView opens a view of the Views menu (#/home/view/<id>). */
  openView(id: string): void;
  /** toChat attaches a reference to the composer ("↩ to chat"). */
  toChat(label: string, ref: string): void;
  /** askHere asks Cancel first (focused), the verb armed after a moment. */
  askHere(a: { label: string; text: string; verb: string; run(): void }): void;
  toast(kind: "info" | "error", text: string): void;
  live(): boolean;
  /** controller: false for a viewer (no Refresh, no Unpin). */
  controller(): boolean;
}

/** STR are the views' strings (English only). */
export const STR = {
  whatTitle: "What can I do?",
  whatHint: "Tap a prompt to put it in the box. Nothing is sent until you send it.",
  mcpTitle: "MCP servers",
  search: "Search features and tools",
  tools: (n: number, asks: number) => (asks ? `${n} tools · ${asks} ask first` : `${n} tools`),
  openView: "Open view",
  noTools: "No tools known yet. Connect the server on the laptop.",
  noMatch: "Nothing matches.",
  more: "More tools than shown. Search to find one.",
  runs: "● runs at once",
  asks: "◐ asks first",
  off: "✗ off",
  untrusted: "✗ not trusted on the laptop",
  pinnedTitle: "Pinned",
  refresh: "Refresh",
  unpin: "Unpin",
  unpinAsk: (title: string) => `Unpin "${title}" from the Home of every paired phone?`,
  unpinned: "Unpinned.",
  cannotRun: "Can not refresh here",
  stale: "Last run",
  pinHow: 'Ask the AI to pin one: "pin my open issues from Linear to Home".',
  failed: "The laptop did not answer. Try again.",
  offline: "Offline — answers are not saved",
  retry: "Retry",
  empty: "Nothing to show.",
  loading: "Loading",
  noTitle: "(no title)",
  toChat: "↩ to chat",
} as const;

/** GATE_TEXT is a tool's gate with its shape (color always with a shape). */
export function gateText(t: CatalogTool, s: CatalogServer): string {
  if (!s.trusted) return STR.untrusted;
  return t.gate === "runs" ? STR.runs : t.gate === "asks" ? STR.asks : STR.off;
}

/** Load is a request's state for a view: data, or an error line. */
export interface Load<T> {
  busy: boolean;
  data: T | null;
  error: string;
}

/** loadInto runs op into the signal, keeping the old data on a failure. */
export function loadInto<T>(host: CatalogHost, s: { value: Load<T> }, op: string, args?: unknown): Promise<void> {
  s.value = { ...s.value, busy: true, error: "" };
  return host.request(op, args).then(
    (r) => {
      s.value = { busy: false, data: r as T, error: "" };
    },
    (e: unknown) => {
      s.value = { busy: false, data: s.value.data, error: errText(e) };
    },
  );
}

/** errText is the laptop's message of a failed request, or a plain line. */
export function errText(e: unknown): string {
  const m = (e as { message?: unknown } | null)?.message;
  return typeof m === "string" && m ? m : STR.failed;
}

/** CatalogState is the "What can I do?" / MCP block's state. */
export interface CatalogState {
  feature: "" | "mcp";
  query: { value: string };
  load: { value: Load<AICatalog> };
}

export function newCatalog(host: CatalogHost, feature: "" | "mcp"): CatalogState {
  const st: CatalogState = { feature, query: host.signal(""), load: host.signal<Load<AICatalog>>({ busy: true, data: null, error: "" }) };
  void reloadCatalog(host, st);
  return st;
}

/** reloadCatalog asks the laptop again with the current search. */
export function reloadCatalog(host: CatalogHost, st: CatalogState): Promise<void> {
  const args: { feature?: string; query?: string } = {};
  if (st.feature) args.feature = st.feature;
  const q = st.query.value.trim().slice(0, 64);
  if (q) args.query = q;
  return loadInto(host, st.load, "ai.catalog", args);
}

let searchTimer: ReturnType<typeof setTimeout> | null = null;

/** search waits a little after the last key (ai.catalog is 1 per 2 s). */
export function search(host: CatalogHost, st: CatalogState, q: string): void {
  st.query.value = q;
  if (searchTimer) clearTimeout(searchTimer);
  searchTimer = setTimeout(() => void reloadCatalog(host, st), 2100);
}

/** PinsState is the pinned tracker blocks: the list and each pin's run. */
export interface PinsState {
  list: { value: Load<TrackerPins> };
  runs: { value: Record<string, Load<TrackerBlock>> };
}

const timers = new Map<string, ReturnType<typeof setInterval>>();

export function newPins(host: CatalogHost): PinsState {
  const st: PinsState = { list: host.signal<Load<TrackerPins>>({ busy: true, data: null, error: "" }), runs: host.signal({}) };
  void reloadPins(host, st);
  return st;
}

/** reloadPins reads the list, runs every pin that may run, and re-arms
 * the refresh timers (they stop with stopPins, when the Home closes). */
export function reloadPins(host: CatalogHost, st: PinsState): Promise<void> {
  return loadInto(host, st.list, "ai.tracker_pins").then(() => {
    stopPins();
    const pins = st.list.value.data?.pins ?? [];
    for (const p of pins) {
      if (!p.runs || !host.controller()) continue;
      void runPin(host, st, p);
      if (p.refresh >= 60) timers.set(p.id, setInterval(() => host.live() && void runPin(host, st, p), p.refresh * 1000));
    }
  });
}

export function stopPins(): void {
  for (const id of timers.values()) clearInterval(id);
  timers.clear();
}

function setRun(st: PinsState, id: string, v: Load<TrackerBlock>): void {
  st.runs.value = { ...st.runs.value, [id]: v };
}

/** runPin re-runs one pin on the laptop. */
export function runPin(host: CatalogHost, st: PinsState, p: TrackerPin): Promise<void> {
  const was = st.runs.value[p.id];
  setRun(st, p.id, { busy: true, data: was?.data ?? null, error: "" });
  return host.request("ai.tracker_run", { id: p.id }).then(
    (r) => setRun(st, p.id, { busy: false, data: r as TrackerBlock, error: "" }),
    (e: unknown) => setRun(st, p.id, { busy: false, data: was?.data ?? null, error: errText(e) }),
  );
}

/** unpin asks Cancel first, then removes the pin on the laptop. */
export function unpin(host: CatalogHost, st: PinsState, p: TrackerPin): void {
  host.askHere({
    label: STR.unpin,
    text: STR.unpinAsk(p.title),
    verb: STR.unpin,
    run: () => {
      host.request("ai.tracker_unpin", { id: p.id }).then(
        (r) => {
          const t = timers.get(p.id);
          if (t) clearInterval(t);
          timers.delete(p.id);
          st.list.value = { busy: false, data: r as TrackerPins, error: "" };
          host.toast("info", STR.unpinned);
        },
        (e: unknown) => host.toast("error", errText(e)),
      );
    },
  });
}

/** ago is a short "3 min ago" for a run's time. */
export function ago(at: number, now = Date.now()): string {
  const s = Math.max(0, Math.round((now - at) / 1000));
  if (s < 60) return "just now";
  if (s < 3600) return `${Math.floor(s / 60)} min ago`;
  if (s < 86400) return `${Math.floor(s / 3600)} h ago`;
  return `${Math.floor(s / 86400)} d ago`;
}
