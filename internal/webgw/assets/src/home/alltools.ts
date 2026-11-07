// The "All tools" menu (MOBUX-5): every built-in feature, every MCP
// server and every saved view in one searchable list. The built-in
// features are fixed here, in the registry's order (plan 3.11.8), each
// with the phone screen it opens; the MCP servers, their tool views and
// the saved views come from the laptop's view.catalog (MOBUX-15 /
// MOB-9). When the catalog cannot be read, the features still show.
// Pure: reading, grouping, searching and where a row goes.
import type { Route } from "../state/route";
import type { StringKey } from "../strings";

/** Target is where a row goes: a screen of the phone (route) or a view
 * of the registry, opened in the Home (#/home/view/<id>, plan 3.11.4). */
export type Target = { route: Route } | { view: string; ws?: string; args?: Record<string, string>; saved?: string };

/** Feature is one built-in feature of the menu. */
export interface Feature {
  id: string;
  /** label and views are string keys: the name and its views in words. */
  label: StringKey;
  views: StringKey;
  /** words are more search words (data, English): view ids and synonyms. */
  words: string;
  route: Route;
}

const more = (rest: string): Route => ({ tab: "more", rest, query: {} });
const tab = (t: Route["tab"]): Route => ({ tab: t, rest: "", query: {} });

/** FEATURES are the built-in features in the registry's order. */
export const FEATURES: readonly Feature[] = [
  { id: "chat", label: "tab.chat", views: "tools.v.chat", words: "chats ai ask", route: tab("chat") },
  { id: "git", label: "tab.git", views: "tools.v.git", words: "git.status git.diff git.log git.branches git.stash commit", route: tab("git") },
  { id: "lanes", label: "ai.wt_title", views: "tools.v.lanes", words: "lanes lane.diff races worktrees agents", route: more("agents") },
  { id: "runs", label: "more.runs", views: "tools.v.runs", words: "runs run usage cost", route: more("runs") },
  { id: "workflows", label: "more.workflows", views: "tools.v.workflows", words: "workflows workflow.tail", route: more("workflows") },
  { id: "tasks", label: "more.tasks", views: "tools.v.tasks", words: "tasks task.tail build", route: more("tasks") },
  { id: "db", label: "more.db", views: "tools.v.db", words: "db db.conns db.tables db.rows db.query sql database", route: more("db") },
  { id: "containers", label: "more.containers", views: "tools.v.containers", words: "containers logs.container docker", route: more("containers") },
  { id: "kube", label: "act.kube", views: "tools.v.kube", words: "kube kube.pods logs.pod k8s", route: more("kube") },
  { id: "tests", label: "act.testing", views: "tools.v.tests", words: "tests tests.output", route: more("testing") },
  { id: "services", label: "act.services", views: "tools.v.services", words: "services logs.service", route: more("services") },
  { id: "debug", label: "act.run", views: "tools.v.debug", words: "debug debug.state threads", route: more("debug") },
  { id: "search", label: "more.search", views: "tools.v.search", words: "search find grep", route: more("search") },
  { id: "files", label: "tab.files", views: "tools.v.files", words: "files tree", route: tab("files") },
  { id: "terms", label: "tab.term", views: "tools.v.terms", words: "terms terminal shell", route: tab("term") },
  { id: "settings", label: "more.settings", views: "tools.v.settings", words: "settings", route: more("settings") },
  { id: "itermoc", label: "im.title", views: "tools.v.itermoc", words: "itermoc models profile", route: more("itermoc") },
  { id: "mcpserver", label: "more.mcp", views: "tools.v.mcpserver", words: "mcp.status outside agents", route: more("mcp") },
];

/** CatalogView is one view of the laptop's catalog that the menu lists. */
export interface CatalogView {
  id: string;
  feature: string;
  title: string;
  /** server and tool: an MCP view (tool "resource" for the resource view). */
  server?: string;
  tool?: string;
  description?: string;
  /** run: it runs from the menu; else it opens as "Ask the AI". */
  run: boolean;
}

/** SavedView is a view saved on the laptop (shared by all paired phones). */
export interface SavedView {
  id: string;
  name: string;
  view: string;
  ws?: string;
  args?: Record<string, string>;
  pinned: boolean;
}

/** MCPServer groups one server's views. */
export interface MCPServer {
  name: string;
  tools: CatalogView[];
  resources: number;
}

/** Catalog is the part of view.catalog the menu needs. */
export interface Catalog {
  views: CatalogView[];
  servers: MCPServer[];
  saved: SavedView[];
}

export const EMPTY_CATALOG: Catalog = { views: [], servers: [], saved: [] };

type Obj = Record<string, unknown>;

function obj(v: unknown): Obj | null {
  return typeof v === "object" && v !== null && !Array.isArray(v) ? (v as Obj) : null;
}

/** pick reads key or its capitalised Go name (a result without json tags). */
function pick(o: Obj, key: string): unknown {
  return key in o ? o[key] : o[key[0].toUpperCase() + key.slice(1)];
}

function str(o: Obj, key: string): string {
  const v = pick(o, key);
  return typeof v === "string" ? v : "";
}

function strMap(v: unknown): Record<string, string> | undefined {
  const o = obj(v);
  if (!o) return undefined;
  const out: Record<string, string> = {};
  let n = 0;
  for (const [k, x] of Object.entries(o)) {
    if (typeof x === "string" && n < 12) {
      out[k] = x;
      n++;
    }
  }
  return n ? out : undefined;
}

const MCP_PREFIX = "mcp.";
// The menu shows at most this many rows of a kind; the laptop caps them too.
const MAX_VIEWS = 2000;
const MAX_SAVED = 32;

function readView(v: unknown): CatalogView | null {
  const o = obj(v);
  if (!o) return null;
  const id = str(o, "id");
  if (!id) return null;
  let server = str(o, "server");
  let tool = str(o, "tool");
  const src = obj(pick(o, "source"));
  if (src && !server) {
    server = str(src, "mcpServer") || str(src, "MCPServer");
    tool = str(src, "mcpTool") || str(src, "MCPTool") || (pick(src, "mcpResource") === true || pick(src, "MCPResource") === true ? "resource" : "");
  }
  if (!server && id.startsWith(MCP_PREFIX)) {
    const rest = id.slice(MCP_PREFIX.length);
    const dot = rest.indexOf(".");
    if (dot > 0) {
      server = rest.slice(0, dot);
      tool = rest.slice(dot + 1);
    }
  }
  const run = pick(o, "run");
  return {
    id,
    feature: str(o, "feature") || (server ? "mcp" : id.split(".")[0]),
    title: str(o, "title") || tool || id,
    server: server || undefined,
    tool: tool || undefined,
    description: str(o, "description") || undefined,
    // A built-in view runs unless the laptop says no; an MCP view only when it says yes.
    run: typeof run === "boolean" ? run : !server,
  };
}

function readSaved(v: unknown): SavedView | null {
  const o = obj(v);
  if (!o) return null;
  const id = str(o, "id");
  const spec = obj(pick(o, "spec"));
  const view = spec ? str(spec, "view") : str(o, "view");
  if (!id || !view) return null;
  return {
    id,
    name: str(o, "name") || (spec ? str(spec, "title") : "") || view,
    view,
    ws: (spec ? str(spec, "ws") : str(o, "ws")) || undefined,
    args: strMap(spec ? pick(spec, "args") : pick(o, "args")),
    pinned: pick(o, "pinned") === true || pick(o, "pin") === true,
  };
}

/** readCatalog reads a view.catalog result; anything unknown is left out. */
export function readCatalog(data: unknown): Catalog {
  const o = obj(data);
  if (!o) return EMPTY_CATALOG;
  const rawViews = pick(o, "views");
  const views: CatalogView[] = [];
  const seen = new Set<string>();
  for (const v of Array.isArray(rawViews) ? rawViews.slice(0, MAX_VIEWS) : []) {
    const cv = readView(v);
    if (cv && !seen.has(cv.id)) {
      seen.add(cv.id);
      views.push(cv);
    }
  }
  const rawSaved = pick(o, "saved");
  const saved: SavedView[] = [];
  for (const v of Array.isArray(rawSaved) ? rawSaved.slice(0, MAX_SAVED) : []) {
    const sv = readSaved(v);
    if (sv) saved.push(sv);
  }
  return { views, servers: groupServers(views, pick(o, "servers")), saved };
}

/** groupServers puts the MCP views under their server, in first-seen
 * order; servers the laptop lists without views (none cached yet) still
 * get a row. */
function groupServers(views: CatalogView[], listed: unknown): MCPServer[] {
  const by = new Map<string, MCPServer>();
  const add = (name: string) => {
    let s = by.get(name);
    if (!s) {
      s = { name, tools: [], resources: 0 };
      by.set(name, s);
    }
    return s;
  };
  for (const v of Array.isArray(listed) ? listed : []) {
    const name = typeof v === "string" ? v : obj(v) ? str(obj(v)!, "name") : "";
    if (name) add(name);
  }
  for (const v of views) {
    if (!v.server) continue;
    const s = add(v.server);
    if (v.tool === "resource") s.resources++;
    s.tools.push(v);
  }
  return [...by.values()];
}

/** builtinViews are the catalog's views of the built-in features (not
 * MCP): a search shows them as their own rows. */
export function builtinViews(c: Catalog): CatalogView[] {
  return c.views.filter((v) => !v.server);
}

/** words splits a query into lower-case words. */
export function queryWords(q: string): string[] {
  return q.toLowerCase().split(/\s+/).filter(Boolean);
}

/** matches says whether every query word is in the text (case-insensitive);
 * an empty query matches everything. */
export function matches(words: readonly string[], text: string): boolean {
  const low = text.toLowerCase();
  return words.every((w) => low.includes(w));
}

/** score orders matches: a title that starts with the first word first,
 * then a title that has it, then the rest. */
export function score(words: readonly string[], title: string): number {
  if (!words.length) return 0;
  const low = title.toLowerCase();
  if (low.startsWith(words[0])) return 2;
  return low.includes(words[0]) ? 1 : 0;
}

/** filterRanked keeps the items whose text matches and orders them by
 * score, keeping the given order among equals. */
export function filterRanked<T>(words: readonly string[], items: readonly T[], title: (x: T) => string, text: (x: T) => string): T[] {
  return items
    .map((item, i) => ({ item, i, s: score(words, title(item)) }))
    .filter((x) => matches(words, text(x.item)))
    .sort((a, b) => b.s - a.s || a.i - b.i)
    .map((x) => x.item);
}

/** viewTarget is where a catalog view goes. */
export function viewTarget(v: CatalogView, ws?: string): Target {
  return { view: v.id, ws };
}

/** savedTarget is where a saved view goes: its spec, re-run live. */
export function savedTarget(s: SavedView): Target {
  return { view: s.view, ws: s.ws, args: s.args, saved: s.id };
}

/** viewHash is the Home deep link of a view (plan 3.11.4):
 * #/home/view/<id>?ws=…&arg.<name>=…&saved=<id>. */
export function viewHash(t: Exclude<Target, { route: Route }>): string {
  const q = new URLSearchParams();
  if (t.ws) q.set("ws", t.ws);
  for (const [k, v] of Object.entries(t.args ?? {})) q.set("arg." + k, v);
  if (t.saved) q.set("saved", t.saved);
  const s = q.toString();
  return "#/home/view/" + encodeURIComponent(t.view) + (s ? "?" + s : "");
}
