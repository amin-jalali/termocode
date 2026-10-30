// The desktop workbench (≥ 1200 px): the page looks and works like the
// termocode TUI on the laptop. Rail, side panel (the tree or git), the
// editor with a tab per open file, a bottom panel with the terminal, the
// chat on the right and a status bar at the bottom. The route still says
// what the user looks at; this module keeps what an IDE keeps besides:
// the open tabs, the active file, which panels are open and their sizes.
// Sizes and open panels are a per-browser convenience (lib/prefs.ts
// stores them; missing or blocked storage: the defaults).
import { computed, effect, signal } from "@preact/signals";
import type { BufItem } from "../../../../webproto/proto";
import { loadLayout, saveLayout } from "./prefs";
import { go, route } from "./router";

export interface Panes {
  /** side: the side panel shows; sideW is its width in px. */
  side: boolean;
  sideW: number;
  /** panel: the bottom panel (terminal) shows; panelH is its height in px. */
  panel: boolean;
  panelH: number;
  /** chat: the chat column shows; chatW is its width in px. */
  chat: boolean;
  chatW: number;
}

export const SIDE_MIN = 180;
export const SIDE_MAX = 560;
export const PANEL_MIN = 120;
export const CHAT_MIN = 300;
export const CHAT_MAX = 720;

const DEFAULTS: Panes = { side: true, sideW: 264, panel: false, panelH: 260, chat: true, chatW: 400 };

function num(v: unknown, lo: number, hi: number, def: number): number {
  return typeof v === "number" && Number.isFinite(v) ? Math.min(hi, Math.max(lo, Math.round(v))) : def;
}

/** readPanes validates a stored layout; anything odd is the default. */
export function readPanes(raw: unknown): Panes {
  if (typeof raw !== "object" || raw === null || Array.isArray(raw)) return DEFAULTS;
  const v = raw as Record<string, unknown>;
  return {
    side: v.side !== false,
    sideW: num(v.sideW, SIDE_MIN, SIDE_MAX, DEFAULTS.sideW),
    panel: v.panel === true,
    panelH: num(v.panelH, PANEL_MIN, 2000, DEFAULTS.panelH),
    chat: v.chat !== false,
    chatW: num(v.chatW, CHAT_MIN, CHAT_MAX, DEFAULTS.chatW),
  };
}

export const panes = signal<Panes>(readPanes(loadLayout()));

effect(() => saveLayout({ ...panes.value }));

/** setPanes changes some of the panes. */
export function setPanes(p: Partial<Panes>): void {
  panes.value = { ...panes.peek(), ...p };
}

/** closed are the laptop's open files the user closed here (their tab hides). */
export const closed = signal<ReadonlySet<string>>(new Set());
/** wordWrap: long rows wrap in the file view (on by default on a phone,
 * off on a desktop, as the laptop's editor has it). */
export const wordWrap = signal<boolean>(!matchMedia("(min-width: 1200px)").matches);

/** QuickMode is what the quick input lists: files, commands, or a line. */
export type QuickMode = "files" | "commands" | "line";

/** quick is the open quick input (Go to file, the command list), or null. */
export const quick = signal<{ mode: QuickMode } | null>(null);

/** cursorAt is the desktop editor's cursor (1-based), for the status bar. */
export const cursorAt = signal<{ line: number; col: number } | null>(null);

/** focusedGroup is the editor group that has the focus (0: the first). */
export const focusedGroup = signal<number>(0);

/** slotOf names an editor group's editor ("g0", "g1"). */
export function slotOf(group: number): string {
  return "g" + group;
}

/** focusSlot is the focused group's editor slot. */
export const focusSlot = computed<string>(() => slotOf(focusedGroup.value));

// Editor groups (at most two, side by side or one over the other). Each
// has its tabs in order, the one it shows and its preview tab: a file
// opened with a single click in the tree shows in italics and the next
// such file takes its place, until the user keeps it (a double click, an
// edit). The first group also lists the laptop's open files (bufs) the
// user did not close here, after its own.

export interface Group {
  tabs: readonly string[];
  active: string | null;
  preview: string | null;
}

/** MAX_GROUPS: the editor splits once (right or down). */
export const MAX_GROUPS = 2;

const EMPTY: Group = { tabs: [], active: null, preview: null };

/** groups are the editor groups, left (top) first. */
export const groups = signal<readonly Group[]>([EMPTY]);

/** SplitDir: the second group sits right of the first, or below it. */
export type SplitDir = "right" | "down";
export const splitDir = signal<SplitDir>("right");

/** laptopBufs are the laptop's open files (the workbench keeps it). */
export const laptopBufs = signal<readonly BufItem[]>([]);

/** activeFile is the file the focused group shows (null: none). */
export const activeFile = computed<string | null>(() => groups.value[focusedGroup.value]?.active ?? null);

/** tabsOf is the editor's tab list: the files this page opened, then the
 * laptop's open files (bufs) the user did not close here. */
export function tabsOf(own: readonly string[], bufs: readonly BufItem[], gone: ReadonlySet<string>): string[] {
  const out = [...own];
  for (const b of bufs) if (b.path && !out.includes(b.path) && !gone.has(b.path)) out.push(b.path);
  return out;
}

/** tabsIn is group g's tab list as the tab bar shows it. */
export function tabsIn(g: number, all: readonly Group[] = groups.value): string[] {
  const gr = all[g];
  if (!gr) return [];
  return g === 0 ? tabsOf(gr.tabs, laptopBufs.value, closed.value) : [...gr.tabs];
}

/** isDirty: the laptop's buffer of path holds unsaved changes. */
function isDirty(path: string): boolean {
  return laptopBufs.peek().some((b) => b.path === path && b.dirty);
}

function setGroup(g: number, gr: Group): void {
  const next = groups.peek().slice();
  next[g] = gr;
  groups.value = next;
}

/** hide drops path from the laptop's tabs here (it was open there). */
function hide(path: string): void {
  if (!laptopBufs.peek().some((b) => b.path === path)) return;
  const c = new Set(closed.peek());
  c.add(path);
  closed.value = c;
}

function unhide(path: string): void {
  if (!closed.peek().has(path)) return;
  const c = new Set(closed.peek());
  c.delete(path);
  closed.value = c;
}

/** openIn shows path in group g: its tab opens (right of the active one)
 * if needed; preview: it takes the place of the group's preview tab. */
export function openIn(g: number, path: string, preview = false): void {
  const all = groups.peek();
  const gr = all[g] ?? EMPTY;
  const tabs = tabsIn(g, all);
  if (g === 0) unhide(path);
  if (tabs.includes(path)) {
    setGroup(g, { tabs, active: path, preview: preview ? gr.preview : gr.preview === path ? null : gr.preview });
    return;
  }
  const out = tabs.slice();
  const old = gr.preview;
  if (preview && old !== null && out.includes(old) && !isDirty(old)) {
    out[out.indexOf(old)] = path;
    if (g === 0) hide(old);
  } else {
    const at = gr.active !== null ? out.indexOf(gr.active) : -1;
    out.splice(at < 0 ? out.length : at + 1, 0, path);
  }
  setGroup(g, { tabs: out, active: path, preview: preview ? path : old === path ? null : old !== null && out.includes(old) ? old : null });
}

/** keep makes path a kept tab (no longer the preview) in group g. */
export function keep(path: string, g: number = focusedGroup.peek()): void {
  const gr = groups.peek()[g];
  if (gr?.preview === path) setGroup(g, { ...gr, preview: null });
}

/** previewNext: the next file route opens as a preview tab. */
let previewNext = false;

/** showFile shows path in the focused group (its tab opens if needed);
 * preview: as a preview tab (a single click in the tree). */
export function showFile(path: string, preview = false): void {
  previewNext = preview;
  go({ tab: "files", rest: path, query: {} });
  previewNext = false;
}

/** focusGroup gives group g the focus and shows its file. */
export function focusGroup(g: number): void {
  if (!groups.peek()[g]) return;
  const changed = focusedGroup.peek() !== g;
  focusedGroup.value = g;
  const f = groups.peek()[g].active;
  const r = route.peek();
  if (f && (changed || r.tab === "files") && !(r.tab === "files" && r.rest === f)) go({ tab: "files", rest: f, query: {} });
}

/** dropGroup removes an empty group; the other one takes the focus. */
function dropGroup(g: number): void {
  const all = groups.peek();
  if (all.length < 2) return;
  const next = all.filter((_, i) => i !== g);
  // The first group's own tabs include the laptop's: keep them listed.
  if (g === 0) next[0] = { ...next[0], tabs: tabsOf(next[0].tabs, laptopBufs.peek(), closed.peek()) };
  groups.value = next;
  focusedGroup.value = 0;
}

/** closeTab closes a tab of group g; the laptop's buffer stays open there.
 * When it was the active one, its neighbour shows (or no file). An empty
 * second group closes. */
export function closeTab(path: string, g: number = focusedGroup.peek()): void {
  const all = groups.peek();
  const gr = all[g];
  if (!gr) return;
  const tabs = tabsIn(g, all);
  const i = tabs.indexOf(path);
  const rest = tabs.filter((p) => p !== path);
  if (g === 0) hide(path);
  const next = gr.active === path ? (rest[Math.min(i, rest.length - 1)] ?? null) : gr.active;
  setGroup(g, { tabs: rest, active: next, preview: gr.preview === path ? null : gr.preview });
  if (rest.length === 0 && all.length > 1) {
    dropGroup(g);
    const f = groups.peek()[0].active;
    if (f) go({ tab: "files", rest: f, query: {} }, true);
    return;
  }
  if (gr.active !== path || g !== focusedGroup.peek()) return;
  const r = route.peek();
  if (r.tab === "files") go({ tab: "files", rest: next ?? "", query: {} }, true);
}

/** closeMany closes the tabs of group g that keepIt says no to. */
export function closeMany(g: number, keepIt: (path: string, i: number, tabs: readonly string[]) => boolean): void {
  const tabs = tabsIn(g);
  for (const [i, p] of tabs.entries()) if (!keepIt(p, i, tabs)) closeTab(p, g);
}

/** moveTab moves path from group from to group to, before index at
 * (to may be a new group: the editor splits). */
export function moveTab(path: string, from: number, to: number, at: number): void {
  const all = groups.peek().slice();
  if (to > all.length || to >= MAX_GROUPS || !all[from]) return;
  const src = tabsIn(from, all).filter((p) => p !== path);
  const fromGr = all[from];
  if (from === to) {
    src.splice(Math.max(0, Math.min(at, src.length)), 0, path);
    all[from] = { ...fromGr, tabs: src };
    groups.value = all;
    return;
  }
  const dst = to < all.length ? tabsIn(to, all).filter((p) => p !== path) : [];
  dst.splice(Math.max(0, Math.min(at, dst.length)), 0, path);
  const toGr = all[to] ?? EMPTY;
  all[to] = { tabs: dst, active: path, preview: toGr.preview === path ? null : toGr.preview };
  const nextActive = fromGr.active === path ? (src[Math.min(Math.max(0, tabsIn(from, all).indexOf(path)), src.length - 1)] ?? null) : fromGr.active;
  all[from] = { tabs: src, active: nextActive, preview: fromGr.preview === path ? null : fromGr.preview };
  if (from === 0) hide(path);
  groups.value = all;
  if (src.length === 0) {
    dropGroup(from);
    focusGroup(0);
    return;
  }
  focusGroup(to);
}

/** splitEditor opens path in the other group (a new one to the right or
 * below when there is one group) and gives it the focus. */
export function splitEditor(path: string | null, dir: SplitDir = "right"): void {
  if (!path) return;
  const all = groups.peek();
  if (all.length < MAX_GROUPS) {
    splitDir.value = dir;
    groups.value = [...all, { tabs: [path], active: path, preview: null }];
    focusGroup(all.length);
    return;
  }
  const other = focusedGroup.peek() === 0 ? 1 : 0;
  focusedGroup.value = other;
  openIn(other, path);
  focusGroup(other);
}

// The route drives the editor: a file route opens (or shows) its tab in
// the focused group, a terminal route opens the bottom panel, the chat
// route the chat column.
effect(() => {
  const r = route.value;
  if (r.tab === "files" && r.rest) {
    const g = focusedGroup.peek();
    const gr = groups.peek()[g];
    if (gr?.active !== r.rest || previewNext) openIn(g, r.rest, previewNext);
  }
  if (r.tab === "term" && !panes.peek().panel) setPanes({ panel: true });
  if (r.tab === "chat" && !panes.peek().chat) setPanes({ chat: true });
});

// An edited file is kept: its preview tab turns into a normal one.
effect(() => {
  const bufs = laptopBufs.value;
  const all = groups.peek();
  for (const [g, gr] of all.entries()) if (gr.preview && bufs.some((b) => b.path === gr.preview && b.dirty)) keep(gr.preview, g);
});
