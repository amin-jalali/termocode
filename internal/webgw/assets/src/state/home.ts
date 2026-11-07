// The phone's Home stream (MOB-4, mobile plan 2.1): the chat's messages
// with the quick blocks the user asked for by chip, in the order they
// came. A quick block costs no tokens: it is a view run on the laptop
// (view.run) or, while the laptop has no view for it, built here from a
// topic the page already reads (inbox, cards, tasks, git, runs), so it
// stays live with that topic. Pure: no DOM, timers or network. Only the
// Home (home.js) imports it, and it imports types only.
import type { Card, ChatMsg, InboxItem, ProjectItem } from "../../../../webproto/proto";
import type { GitState } from "./git";
import type { RunView, TaskView } from "./tasks";

/** Tone of a row; the page always draws it with a shape too. */
export type Tone = "ok" | "warn" | "bad" | "busy" | "idle" | "info";

/** HomeItem is one row of a quick block, in the block protocol's shape
 * (internal/blocks BlockItem). id is "<kind>:<ref>" as in the inbox. */
export interface HomeItem {
  id: string;
  title: string;
  sub?: string;
  tone?: Tone;
  badge?: string;
  time?: number;
}

export interface HomeGroup {
  label: string;
  count: number;
  items: HomeItem[];
}

/** HomeBlock is the part of a block (internal/blocks Block) the page's
 * own quick blocks use: a list, a grouped list or a status line. */
export interface HomeBlock {
  id: string;
  kind: "list" | "status";
  title: string;
  source?: string;
  items?: HomeItem[];
  groups?: HomeGroup[];
  state?: Tone;
  total?: number;
  truncated?: boolean;
}

/** MAX_QUICK quick blocks stay in the stream; older ones drop off. */
export const MAX_QUICK = 12;
/** MAX_ROWS rows show in one quick block (the rest is counted). */
export const MAX_ROWS = 20;

/** Quick is one chip's answer in the stream. chat and after place it:
 * after the message with index after (-1: before every message) of that
 * chat. from says who built it: the laptop's view (block), or the page
 * from a topic (rebuilt on each draw, so it stays live). */
export interface Quick {
  id: string;
  chip: string;
  chat: string;
  after: number;
  at: number;
  state: "loading" | "ready" | "error";
  from: "view" | "page" | "";
  block?: unknown;
  error?: string;
}

export type StreamItem = { kind: "msg"; m: ChatMsg } | { kind: "quick"; q: Quick };

/** mergeStream puts the quick blocks of chat between its messages. */
export function mergeStream(chat: string, msgs: readonly ChatMsg[], quick: readonly Quick[]): StreamItem[] {
  const qs = quick.filter((q) => q.chat === chat).sort((a, b) => a.at - b.at || (a.id < b.id ? -1 : 1));
  const out: StreamItem[] = [];
  let j = 0;
  for (const m of msgs) {
    while (j < qs.length && qs[j].after < m.i) out.push({ kind: "quick", q: qs[j++] });
    out.push({ kind: "msg", m });
  }
  while (j < qs.length) out.push({ kind: "quick", q: qs[j++] });
  return out;
}

/** addQuick appends q and keeps the newest MAX_QUICK. */
export function addQuick(list: readonly Quick[], q: Quick): Quick[] {
  const next = [...list, q];
  return next.length > MAX_QUICK ? next.slice(next.length - MAX_QUICK) : next;
}

/** patchQuick changes the quick block id (nothing when it dropped off). */
export function patchQuick(list: readonly Quick[], id: string, patch: Partial<Quick>): Quick[] {
  return list.map((q) => (q.id === id ? { ...q, ...patch } : q));
}

/** dropQuick removes the quick block id (the user closed it). */
export function dropQuick(list: readonly Quick[], id: string): Quick[] {
  return list.filter((q) => q.id !== id);
}

/** lastIndex is the index of a chat's last message (-1: none). */
export function lastIndex(msgs: readonly ChatMsg[]): number {
  return msgs.length ? msgs[msgs.length - 1].i : -1;
}

/** Labels are the words the builders need (the Home's strings). */
export interface Labels {
  needsYou: string;
  nothingNeedsYou: string;
  board: string;
  boardEmpty: string;
  states: Record<InboxItem["state"], string>;
  tasks: string;
  noTasks: string;
  git: string;
  clean: string;
  noRepo: string;
  runs: string;
  noRuns: string;
  projects: string;
  noProjects: string;
}

const INBOX_TONE: Record<InboxItem["state"], Tone> = { needs_you: "warn", working: "busy", failed: "bad", done: "ok" };
const BOARD_ORDER: readonly InboxItem["state"][] = ["needs_you", "working", "failed", "done"];

function cut<T>(rows: readonly T[]): { rows: T[]; total: number; truncated: boolean } {
  return { rows: rows.slice(0, MAX_ROWS), total: rows.length, truncated: rows.length > MAX_ROWS };
}

function list(id: string, title: string, empty: string, items: HomeItem[]): HomeBlock {
  if (items.length === 0) return { id, kind: "status", title: empty, state: "ok" };
  const c = cut(items);
  return { id, kind: "list", title, items: c.rows, total: c.total, truncated: c.truncated };
}

/** readInbox reads the inbox topic's items (null: no inbox topic yet). */
export function readInbox(data: unknown): InboxItem[] | null {
  if (typeof data !== "object" || data === null) return null;
  const v = (data as { items?: unknown }).items;
  if (!Array.isArray(v)) return null;
  return v.filter((x): x is InboxItem => typeof x === "object" && x !== null && typeof (x as InboxItem).id === "string" && typeof (x as InboxItem).title === "string");
}

function inboxItem(x: InboxItem): HomeItem {
  return { id: x.id, title: x.title, sub: x.lane || x.chat || undefined, tone: INBOX_TONE[x.state] ?? "idle", time: x.since };
}

/** needsYouBlock lists what waits for the user: the inbox's needs_you
 * items, or the waiting cards while the laptop has no inbox topic. */
export function needsYouBlock(inbox: readonly InboxItem[] | null, cards: readonly Card[], L: Labels): HomeBlock {
  const items = inbox
    ? inbox.filter((x) => x.state === "needs_you").map(inboxItem)
    : cards.map((c): HomeItem => ({ id: "card:" + c.id, title: c.title, tone: "warn", time: c.since }));
  return list("needs", L.needsYou, L.nothingNeedsYou, items);
}

/** boardBlock groups the inbox by state: needs you, working, failed, done. */
export function boardBlock(inbox: readonly InboxItem[], L: Labels): HomeBlock {
  const groups: HomeGroup[] = [];
  for (const st of BOARD_ORDER) {
    const all = inbox.filter((x) => x.state === st).map(inboxItem);
    if (all.length) groups.push({ label: L.states[st], count: all.length, items: all.slice(0, MAX_ROWS) });
  }
  if (groups.length === 0) return { id: "board", kind: "status", title: L.boardEmpty, state: "idle" };
  return { id: "board", kind: "list", title: L.board, groups, total: inbox.length };
}

const TASK_TONE: Record<string, Tone> = { running: "busy", ok: "ok", failed: "bad" };

/** tasksBlock lists the workspace's tasks with their last run. */
export function tasksBlock(tasks: readonly TaskView[], L: Labels): HomeBlock {
  return list(
    "tasks",
    L.tasks,
    L.noTasks,
    tasks.map((x) => ({ id: "task:" + x.label, title: x.label, sub: x.group || x.source || undefined, tone: x.last ? (TASK_TONE[x.last.state] ?? "idle") : "idle", time: x.last?.when || undefined })),
  );
}

/** gitBlock lists the changed files of the branch. */
export function gitBlock(g: GitState, L: Labels): HomeBlock {
  if (!g.repo) return { id: "git", kind: "status", title: L.noRepo, state: "idle" };
  const title = g.branch.name ? L.git + " · " + g.branch.name : L.git;
  const items = g.files.map(
    (f): HomeItem => ({ id: "file:" + f.path, title: f.path, badge: f.conflict ? "U" : f.untracked ? "?" : (f.x + f.y).trim() || "M", tone: f.conflict ? "bad" : f.staged ? "ok" : "warn" }),
  );
  const b = list("git", title, L.clean, items);
  if (b.kind === "list") b.total = items.length + g.more;
  return b;
}

/** runsBlock lists the AI runs: the ones that need the user first. */
export function runsBlock(runs: readonly RunView[], L: Labels): HomeBlock {
  const tone = (r: RunView): Tone => (r.needsYou ? "warn" : r.live ? "busy" : r.state === "failed" ? "bad" : r.state === "done" ? "ok" : "idle");
  const sorted = [...runs].sort((a, b) => Number(b.needsYou) - Number(a.needsYou) || Number(b.live) - Number(a.live));
  return list(
    "runs",
    L.runs,
    L.noRuns,
    sorted.map((r) => ({ id: "run:" + r.key, title: r.title || r.key, sub: r.steps ? `${r.step}/${r.steps} ${r.stepTitle}`.trim() : r.why || undefined, tone: tone(r), badge: r.cost ? "$" + r.cost.toFixed(2) : undefined })),
  );
}

const PROJECT_TONE: Record<ProjectItem["state"], Tone> = { here: "ok", running: "busy", asleep: "idle" };

/** readProjects reads the projects topic (MOB-8; [] without it). */
export function readProjects(data: unknown): ProjectItem[] {
  const v = typeof data === "object" && data !== null ? (data as { projects?: unknown }).projects : undefined;
  return Array.isArray(v) ? v.filter((x): x is ProjectItem => typeof x === "object" && x !== null && typeof (x as ProjectItem).id === "string" && typeof (x as ProjectItem).name === "string") : [];
}

/** projectsBlock lists the laptop's projects: those waiting for the user
 * first, then the working ones. */
export function projectsBlock(projects: readonly ProjectItem[], L: Labels): HomeBlock {
  const sorted = [...projects].sort((a, b) => (b.waiting ?? 0) - (a.waiting ?? 0) || (b.working ?? 0) - (a.working ?? 0));
  return list(
    "projects",
    L.projects,
    L.noProjects,
    sorted.map((p) => ({
      id: "project:" + p.id,
      title: p.name,
      sub: p.where,
      tone: (p.waiting ?? 0) > 0 ? "warn" : (PROJECT_TONE[p.state] ?? "idle"),
      badge: (p.waiting ?? 0) > 0 ? "● " + p.waiting : (p.working ?? 0) > 0 ? "◐ " + p.working : undefined,
    })),
  );
}

/** readBlock takes a view.run reply: the block, or null when it is not one. */
export function readBlock(data: unknown): { kind: string; title: string } | null {
  const v = typeof data === "object" && data !== null && "block" in (data as object) ? (data as { block: unknown }).block : data;
  if (typeof v !== "object" || v === null) return null;
  const b = v as { kind?: unknown; title?: unknown };
  return typeof b.kind === "string" && typeof b.title === "string" ? (v as { kind: string; title: string }) : null;
}
