// The desktop's activity bar (workbench): every view of the page is one
// item, as on the laptop's editor, not under a "More" list. The items
// with a side view open it in the side bar (a second click hides the
// side bar); Databases, Containers, Settings and This device open in the
// editor area, where their tables have room. Badges: git changes, cards
// waiting for an answer, runs that are live or need you.
import { h } from "preact";
import { signal } from "@preact/signals";
import { modKey } from "../lib/platform";
import { go, route } from "../lib/router";
import { activeFile, panes, setPanes } from "../lib/workbench";
import { status, waiting } from "../session";
import type { Route } from "../state/route";
import { t, type StringKey } from "../strings";
import { Cod } from "./icons";

export type Activity = "files" | "search" | "git" | "run" | "testing" | "db" | "containers" | "services" | "kube" | "mcp" | "chat" | "device" | "settings";

interface Item {
  id: Activity;
  label: StringKey;
  icon: string;
  keys?: string;
  /** side: the view opens in the side bar. */
  side: boolean;
  to: () => Route;
}

const more = (rest: string): Route => ({ tab: "more", rest, query: {} });

export const ACTIVITIES: readonly Item[] = [
  { id: "files", label: "act.explorer", icon: "files", keys: "Shift+E", side: true, to: () => ({ tab: "files", rest: activeFile.peek() ?? "", query: {} }) },
  { id: "search", label: "more.search", icon: "search", keys: "Shift+F", side: true, to: () => more("search") },
  { id: "git", label: "act.scm", icon: "source-control", keys: "Shift+G", side: true, to: () => ({ tab: "git", rest: "", query: {} }) },
  { id: "run", label: "act.run", icon: "debug-alt", keys: "Shift+D", side: true, to: () => more("tasks") },
  { id: "testing", label: "act.testing", icon: "beaker", side: true, to: () => more("testing") },
  { id: "db", label: "more.db", icon: "database", side: true, to: () => more("db") },
  { id: "containers", label: "more.containers", icon: "package", side: true, to: () => more("containers") },
  { id: "services", label: "act.services", icon: "server-process", side: true, to: () => more("services") },
  { id: "kube", label: "act.kube", icon: "cloud", side: false, to: () => more("kube") },
  { id: "mcp", label: "more.mcp", icon: "plug", side: true, to: () => more("mcp") },
  { id: "chat", label: "tab.chat", icon: "comment-discussion", side: false, to: () => ({ tab: "chat", rest: "", query: {} }) },
];

export const ACTIVITIES_BOTTOM: readonly Item[] = [
  { id: "device", label: "act.device", icon: "account", side: false, to: () => more("") },
  { id: "settings", label: "more.settings", icon: "gear", keys: ",", side: false, to: () => more("settings") },
];

/** activityOf is the item a route belongs to (null: none, the terminal). */
export function activityOf(r: Route): Activity | null {
  switch (r.tab) {
    case "files":
      return "files";
    case "git":
      return "git";
    case "chat":
      return "chat";
    case "term":
      return null;
    case "more": {
      const sub = r.rest.split("/")[0];
      if (sub === "search") return "search";
      if (sub === "tasks" || sub === "runs" || sub === "debug") return "run";
      if (sub === "testing") return "testing";
      if (sub === "services") return "services";
      if (sub === "kube") return "kube";
      if (sub === "db") return "db";
      if (sub === "containers") return "containers";
      if (sub === "mcp") return "mcp";
      if (sub === "settings") return "settings";
      return "device";
    }
  }
}

/** sideActivity: the route's view belongs in the side bar. */
export function sideActivity(r: Route): boolean {
  const a = activityOf(r);
  return [...ACTIVITIES, ...ACTIVITIES_BOTTOM].some((x) => x.id === a && x.side);
}

/** sideView is the view the side bar shows. A side view's route shows it
 * (#/git, #/more/search …); a file's own route (#/files/<path>, a tab
 * opened from any view) leaves the side bar as it is, as on the laptop's
 * editor. */
export const sideView = signal<Activity>("files");

/** routeSide is the side view a route asks for (null: it leaves it). */
export function routeSide(r: Route): Activity | null {
  if (r.tab === "files" && r.rest) return null;
  return sideActivity(r) ? activityOf(r) : null;
}

/** openSide shows a side view (its activity bar item, a key, a command). */
export function openSide(id: Activity): void {
  const x = [...ACTIVITIES, ...ACTIVITIES_BOTTOM].find((a) => a.id === id);
  if (!x) return;
  if (x.side) {
    setPanes({ side: true });
    sideView.value = id;
  }
  go(x.to());
}

/** shownActivity is the item the activity bar marks: the side bar's view,
 * or the editor area's page (Settings, This device, the chat). */
function shownActivity(r: Route): Activity | null {
  const a = activityOf(r);
  if (a && !sideActivity(r)) return a;
  return panes.value.side ? sideView.value : a;
}

function badge(id: Activity): { n: number; tone: "count" | "dot" } | null {
  const s = status.value;
  if (id === "git" && s.dirty > 0) return { n: s.dirty, tone: "count" };
  if (id === "chat" && waiting.value.count > 0) return { n: waiting.value.count, tone: "count" };
  if (id === "run" && s.runsNeedYou > 0) return { n: s.runsNeedYou, tone: "count" };
  if (id === "run" && s.runsLive > 0) return { n: s.runsLive, tone: "dot" };
  return null;
}

function tap(x: Item): void {
  // A second click on the side bar's own view hides the side bar.
  if (x.side && sideView.peek() === x.id && panes.peek().side && activityOf(route.peek()) === x.id) {
    setPanes({ side: false });
    return;
  }
  openSide(x.id);
}

function ActItem({ x, cur }: { x: Item; cur: Activity | null }) {
  const b = badge(x.id);
  const on = cur === x.id;
  const name = t(x.label);
  const tip = x.keys ? `${name} (${modKey()}+${x.keys})` : name;
  return (
    <button
      type="button"
      class={`nav-item act-item${on ? " is-active" : ""}`}
      data-act={x.id}
      aria-current={on ? "page" : undefined}
      aria-label={b && b.tone === "count" ? `${name} (${b.n})` : name}
      title={tip}
      onClick={() => tap(x)}
    >
      <Cod name={x.icon} big />
      {b && (b.tone === "count" ? <span class="act-badge">{b.n > 99 ? "99+" : b.n}</span> : <span class="act-badge is-dot" />)}
    </button>
  );
}

export function ActivityBar() {
  const cur = shownActivity(route.value);
  return (
    <nav class="rail act-bar" aria-label={t("tabs.label")}>
      {ACTIVITIES.map((x) => (
        <ActItem key={x.id} x={x} cur={cur} />
      ))}
      <span class="act-gap" />
      {ACTIVITIES_BOTTOM.map((x) => (
        <ActItem key={x.id} x={x} cur={cur} />
      ))}
    </nav>
  );
}
