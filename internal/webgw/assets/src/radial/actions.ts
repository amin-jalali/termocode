// The radial menu's real actions on a touch phone or tablet (RM-2), in
// radial.js. Each one calls the op the page's own buttons call (the
// RadialHost gives them; this file imports no value of the page), so the
// radial is a fast launcher with no powers of its own: a push or a pull
// asks first as the git view does (the laptop's card or this page's),
// whatever needs input opens its view. At most 8 actions a ring (the
// marking-menu budget: positions become muscle memory).
import type { h as preactH, render as preactRender, VNode } from "preact";
import type { useEffect as preactUseEffect, useState as preactUseState } from "preact/hooks";
import type { ReadonlySignal } from "@preact/signals";
import type { Meta } from "../../../../webproto/proto";
import type { Category, Item } from "../../../../../web/packages/radial-menu/src/menu";
import type { Route, Tab } from "../state/route";
import type { StringKey } from "../strings";

/** RadialHost is what the page gives radial.js. */
export interface RadialHost {
  h: typeof preactH;
  render: typeof preactRender;
  /** the page's confirm sheet (ui/AskLaptop.tsx) */
  AskLaptop: (p: { label: string; text: string; send(): Promise<unknown>; onDone(): void }) => VNode | null;
  go(r: Route): void;
  route: ReadonlySignal<Route>;
  layout: ReadonlySignal<string>;
  link: { meta: ReadonlySignal<Meta | null>; request(op: string, args?: unknown): Promise<unknown>; subscribe(...topics: string[]): void };
  /** a topic's data now (the store's signal: a draw that reads it redraws) */
  topic(name: string): unknown;
  /** puts text in the chat's box and opens the chat; nothing is sent */
  askAI(text: string): void;
  live: ReadonlySignal<boolean>;
  /** the git topic as the git view reads it (branch, changes) */
  git(): { repo: boolean; branch: { name: string }; files: unknown[] };
  t(key: StringKey, vars?: Record<string, string | number>): string;
  toast(severity: "info" | "ok" | "warn" | "error", title: string): void;
  /** "↩ to chat" (lib/tochat.ts addRef) */
  addRef(r: { kind: string; path?: string }, title?: string): string;
  newChat(): void;
  newTerm(): void;
  /** starts editing a file (lib/live.ts editing) */
  edit(path: string): void;
  showOnLaptop(path: string): void;
  /** the More list's recent views (lib/prefs.ts) */
  prefs: ReadonlySignal<{ moreRecent: string[] }>;
  setPref(k: "moreRecent", v: string[]): void;
  useState: typeof preactUseState;
  useEffect: typeof preactUseEffect;
}

/** The radial's own words (English, like the phone Home's). */
export const RS = {
  noFile: "Open a file first.",
  viewOnly: "This device can only watch.",
  offline: "Not connected to the laptop.",
  secret: "That file looks secret: it stays on the laptop.",
  full: "The chat already has 8 attachments.",
  inChat: "Added to the chat.",
} as const;

/** Op is what an action does: open a view or call the page. */
export type Op = { to: Route } | { call: () => void };

/** Action is one item of a ring with what it does. act: it changes
 * something on the laptop (a viewer cannot, it needs the link). */
export interface Action {
  id: string;
  label: string;
  icon: string;
  detail: string;
  op: Op;
  act?: boolean;
  /** phone: only on a phone (the Home); wide: only on a tablet */
  only?: "phone";
}

export interface Ring {
  id: string;
  label: string;
  icon: string;
  actions: Action[];
}

const tab = (t: Tab): Route => ({ tab: t, rest: "", query: {} });
const more = (rest: string): Route => ({ tab: "more", rest, query: {} });

/** fileNow is the file the page shows (#/files/<path>), or "". */
export function fileNow(r: Route): string {
  return r.tab === "files" ? r.rest : "";
}

/** rings are the radial's categories (SAVED, the favourites, is added by
 * the menu itself). */
export function rings(host: RadialHost): Ring[] {
  const file = (): string => fileNow(host.route.peek());
  const withFile = (f: (path: string) => void) => (): void => {
    const p = file();
    if (!p) host.toast("info", RS.noFile);
    else f(p);
  };
  const askFile = withFile((path) => {
    const r = host.addRef({ kind: "file", path });
    if (r === "secret") return host.toast("warn", RS.secret);
    if (r === "full") return host.toast("warn", RS.full);
    host.go(tab("chat"));
  });
  // A push, a pull or a stash asks first, with the git view's own words
  // (RemoteSheet); with no branch read yet the git view's remote sheet
  // opens instead, which reads the branches itself.
  const remote = (op: "git.push" | "git.pull" | "git.stash") => (): void => {
    const g = host.git();
    const branch = g.branch.name;
    if (!g.repo || !branch) return host.go({ tab: "git", rest: "", query: { sheet: "remote" } });
    const text = op === "git.push" ? host.t("git.push_q", { branch }) : op === "git.pull" ? host.t("git.pull_q", { branch }) : host.t("git.stash_q");
    ask(host, host.t("git.remote"), text, () => host.link.request(op, {}));
  };
  return [
    {
      id: "edit",
      label: "EDIT",
      icon: "open",
      actions: [
        { id: "edit-search", label: "search", icon: "find", detail: "search across the project", op: { to: more("search") } },
        { id: "edit-files", label: "go to file", icon: "folder", detail: "the project's files", op: { to: tab("files") } },
        { id: "edit-edit", label: "edit", icon: "pencil", detail: "edit the open file", act: true, op: { call: withFile((p) => host.edit(p)) } },
        { id: "edit-ask", label: "ask ai", icon: "ask", detail: "ask the AI about the open file", op: { call: askFile } },
        { id: "edit-laptop", label: "on laptop", icon: "laptop", detail: "show the open file on the laptop", act: true, op: { call: withFile((p) => host.showOnLaptop(p)) } },
      ],
    },
    {
      id: "ai",
      label: "AI",
      icon: "ask",
      actions: [
        { id: "ai-chat", label: "chat", icon: "chat", detail: "the AI chat", op: { to: tab("chat") } },
        { id: "ai-new", label: "new chat", icon: "plus", detail: "start an empty chat", act: true, op: { call: () => host.newChat() } },
        { id: "ai-task", label: "new task", icon: "list", detail: "give the AI a task", op: { to: more("new-task") } },
        { id: "ai-file", label: "ask file", icon: "ask", detail: "ask the AI about the open file", op: { call: askFile } },
        { id: "ai-review", label: "review", icon: "diff", detail: "review the AI's changes", op: { to: more("review") }, only: "phone" },
        { id: "ai-agents", label: "agents", icon: "branch", detail: "agent lanes and worktrees", op: { to: more("agents") } },
        { id: "ai-races", label: "races", icon: "flag", detail: "models racing on one task", op: { to: more("races") } },
        { id: "ai-models", label: "models", icon: "set", detail: "AI models and profiles", op: { to: more("itermoc") } },
      ],
    },
    {
      id: "git",
      label: "GIT",
      icon: "branch",
      actions: [
        { id: "git-changes", label: "changes", icon: "diff", detail: "source control: the changed files", op: { to: tab("git") } },
        { id: "git-commit", label: "commit", icon: "check", detail: "stage & commit changes", op: { to: tab("git") } },
        { id: "git-push", label: "push", icon: "up", detail: "push to the remote", act: true, op: { call: remote("git.push") } },
        { id: "git-pull", label: "pull", icon: "down", detail: "pull from the remote", act: true, op: { call: remote("git.pull") } },
        { id: "git-branch", label: "branch", icon: "branch", detail: "switch branch", op: { to: { tab: "git", rest: "", query: { sheet: "remote" } } } },
        { id: "git-stash", label: "stash", icon: "shell", detail: "stash working changes", act: true, op: { call: remote("git.stash") } },
      ],
    },
    {
      id: "run",
      label: "RUN",
      icon: "shell",
      actions: [
        { id: "run-tasks", label: "tasks", icon: "list", detail: "run a saved task", op: { to: more("tasks") } },
        { id: "run-debug", label: "run & debug", icon: "bug", detail: "start and watch a debug session", op: { to: more("debug") } },
        { id: "run-tests", label: "tests", icon: "flask", detail: "run the tests", op: { to: more("testing") } },
        { id: "run-term", label: "terminal", icon: "shell", detail: "the terminals", op: { to: tab("term") } },
        { id: "run-newterm", label: "new shell", icon: "plus", detail: "start a shell on the laptop", act: true, op: { call: () => host.newTerm() } },
        { id: "run-runs", label: "runs", icon: "play", detail: "AI runs: live, waiting, done", op: { to: more("runs") } },
        { id: "run-workflows", label: "workflows", icon: "flow", detail: "agent workflows", op: { to: more("workflows") } },
        { id: "run-services", label: "services", icon: "set", detail: "the project's services", op: { to: more("services") } },
      ],
    },
    {
      id: "view",
      label: "VIEW",
      icon: "set",
      actions: [
        { id: "view-home", label: "home", icon: "home", detail: "the phone's Home", op: { to: tab("home") }, only: "phone" },
        { id: "view-chat", label: "chat", icon: "chat", detail: "the AI chat", op: { to: tab("chat") } },
        { id: "view-files", label: "files", icon: "folder", detail: "the project's files", op: { to: tab("files") } },
        { id: "view-term", label: "terminal", icon: "shell", detail: "the terminals", op: { to: tab("term") } },
        { id: "view-git", label: "git", icon: "branch", detail: "source control", op: { to: tab("git") } },
        { id: "view-board", label: "live board", icon: "board", detail: "every lane, run and card at a glance", op: { to: more("board") } },
        { id: "view-projects", label: "projects", icon: "grid", detail: "switch project", op: { to: more("projects") } },
        { id: "view-tools", label: "all tools", icon: "grid", detail: "every feature in one list", op: { to: more("tools") } },
      ],
    },
    {
      id: "tools",
      label: "TOOLS",
      icon: "grid",
      actions: [
        { id: "tools-db", label: "databases", icon: "db", detail: "connections, tables, queries", op: { to: more("db") } },
        { id: "tools-containers", label: "containers", icon: "box", detail: "containers and images", op: { to: more("containers") } },
        { id: "tools-kube", label: "kubernetes", icon: "cloud", detail: "pods and logs", op: { to: more("kube") } },
        { id: "tools-mcp", label: "MCP server", icon: "plug", detail: "outside agents and their calls", op: { to: more("mcp") } },
        { id: "tools-phone", label: "this device", icon: "phone", detail: "this phone's settings and paired devices", op: { to: more("phone") } },
        { id: "tools-settings", label: "settings", icon: "gear", detail: "the laptop's settings", op: { to: more("settings") } },
        { id: "tools-more", label: "more", icon: "list", detail: "the More list", op: { to: tab("more") } },
      ],
    },
  ];
}

let askRoot: HTMLElement | null = null;

/** ask shows the page's confirm sheet (AskLaptop) for one request. */
export function ask(host: RadialHost, label: string, text: string, send: () => Promise<unknown>): void {
  if (!askRoot) {
    askRoot = document.createElement("div");
    askRoot.className = "radial-ask";
    document.body.appendChild(askRoot);
  }
  const root = askRoot;
  host.render(host.h(host.AskLaptop, { label, text, send, onDone: () => host.render(null, root) }), root);
}

/** run does an action: a viewer or an offline page cannot change things. */
export function run(host: RadialHost, a: Action): void {
  if (a.act && host.link.meta.peek()?.role === "viewer") return host.toast("info", RS.viewOnly);
  if (a.act && !host.live.peek()) return host.toast("warn", RS.offline);
  if ("to" in a.op) host.go(a.op.to);
  else a.op.call();
}

/** categories are the rings as the menu takes them, for a layout. */
export function categories(host: RadialHost, layout: string): Category[] {
  return rings(host).map((r) => ({
    id: r.id,
    label: r.label,
    icon: r.icon,
    items: r.actions
      .filter((a) => a.only !== "phone" || layout === "phone")
      .map((a): Item => ({ id: a.id, label: a.label, icon: a.icon, detail: a.detail, run: () => run(host, a) })),
  }));
}

/** DEFAULT_FAVORITES fill SAVED until the user picks their own. */
export const DEFAULT_FAVORITES = ["ai-chat", "edit-search", "git-commit", "run-term"];
