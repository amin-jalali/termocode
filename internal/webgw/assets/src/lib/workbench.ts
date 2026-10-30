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

/** opened are the files this page opened, in the order they were opened. */
export const opened = signal<string[]>([]);
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

/** activeFile is the file the editor shows (null: none). */
export const activeFile = signal<string | null>(null);

// The route drives the editor: a file route opens (or shows) its tab, a
// terminal route opens the bottom panel, the chat route the chat column.
effect(() => {
  const r = route.value;
  if (r.tab === "files" && r.rest) {
    const path = r.rest;
    if (!opened.peek().includes(path)) opened.value = [...opened.peek(), path];
    if (closed.peek().has(path)) {
      const c = new Set(closed.peek());
      c.delete(path);
      closed.value = c;
    }
    activeFile.value = path;
  }
  if (r.tab === "term" && !panes.peek().panel) setPanes({ panel: true });
  if (r.tab === "chat" && !panes.peek().chat) setPanes({ chat: true });
});

/** tabsOf is the editor's tab list: the files this page opened, then the
 * laptop's open files (bufs) the user did not close here. */
export function tabsOf(own: readonly string[], bufs: readonly BufItem[], gone: ReadonlySet<string>): string[] {
  const out = [...own];
  for (const b of bufs) if (b.path && !out.includes(b.path) && !gone.has(b.path)) out.push(b.path);
  return out;
}

/** showFile shows path in the editor (its tab opens if needed). */
export function showFile(path: string): void {
  go({ tab: "files", rest: path, query: {} });
}

/** closeTab closes a tab; the laptop's buffer stays open there. When it
 * was the active one, its neighbour shows (or no file). */
export function closeTab(path: string, tabs: readonly string[]): void {
  const i = tabs.indexOf(path);
  opened.value = opened.peek().filter((p) => p !== path);
  const c = new Set(closed.peek());
  c.add(path);
  closed.value = c;
  if (activeFile.peek() !== path) return;
  const rest = tabs.filter((p) => p !== path);
  const next = rest[Math.min(i, rest.length - 1)] ?? null;
  activeFile.value = next;
  const r = route.peek();
  if (r.tab === "files") go({ tab: "files", rest: next ?? "", query: {} }, true);
}
