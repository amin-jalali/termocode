// The workbench's commands (Ctrl/Cmd+Shift+P): what the page can do, by
// name, with its keys. Each runs here or asks the laptop through the
// same ops as the buttons do. Beside them, the laptop palette's commands
// the laptop offers to the web (palette.list: its own allow-list); one
// runs there by id, and a risky one only after a laptop card. The
// commands used last come first (kept per browser).
import { signal } from "@preact/signals";
import type { PaletteItem, PaletteList } from "../../../../webproto/proto";
import { link } from "../session";
import type { StringKey } from "../strings";
import { t } from "../strings";
import { toast } from "./toasts";
import { activeDesk } from "./deskeditor";
import { saveLive } from "../views/LiveEditor";
import { newChat } from "../views/Chat";
import { newTerm } from "../views/Term";
import { showOnLaptop } from "../views/FileView";
import { modKey } from "./platform";
import { loadRecentCmds, saveRecentCmds, setPref } from "./prefs";
import { toggleProblems } from "./lsp";
import { go, route } from "./router";
import { openSide } from "../ui/ActivityBar";
import { gitCommands } from "../views/git/gitcommands";
import { activeFile, closeTab, focusedGroup, focusGroup, groups, panes, quick, setPanes, splitEditor, tabsIn, wordWrap } from "./workbench";

export interface Command {
  id: string;
  /** label names a page command; text names a laptop command (its own title). */
  label?: StringKey;
  text?: string;
  /** laptop: it runs on the laptop (ask: after a laptop card). */
  laptop?: { ask: boolean };
  /** keys is the shortcut shown beside it ("Ctrl+B"). */
  keys?: string;
  /** when: shown only when it can run now. */
  when?: () => boolean;
  run: () => void;
}

/** tabsNow is the focused group's tab list. */
const tabsNow = (): string[] => tabsIn(focusedGroup.peek());

const hasFile = () => activeFile.peek() !== null && route.peek().tab !== "more";

/** commands is the list, built on each open (the key names follow the system). */
export function commands(): Command[] {
  const mod = modKey();
  return [
    { id: "quickOpen", label: "cmd.quick_open", keys: `${mod}+P`, run: () => (quick.value = { mode: "files" }) },
    { id: "gotoLine", label: "cmd.goto_line", keys: "Ctrl+G", when: hasFile, run: () => (quick.value = { mode: "line" }) },
    { id: "save", label: "cmd.save", keys: `${mod}+S`, when: hasFile, run: saveLive },
    { id: "closeEditor", label: "cmd.close_editor", keys: `${mod}+W`, when: hasFile, run: () => closeActive() },
    { id: "nextEditor", label: "cmd.next_editor", keys: "Ctrl+Tab", when: () => tabsNow().length > 1, run: () => cycleTab(1) },
    { id: "prevEditor", label: "cmd.prev_editor", keys: "Ctrl+Shift+Tab", when: () => tabsNow().length > 1, run: () => cycleTab(-1) },
    { id: "splitRight", label: "cmd.split_right", keys: `${mod}+\\`, when: hasFile, run: () => splitEditor(activeFile.peek(), "right") },
    { id: "splitDown", label: "cmd.split_down", when: hasFile, run: () => splitEditor(activeFile.peek(), "down") },
    { id: "focusGroup1", label: "cmd.focus_group1", keys: `${mod}+1`, run: () => focusGroup(0) },
    { id: "focusGroup2", label: "cmd.focus_group2", keys: `${mod}+2`, when: () => groups.peek().length > 1 || hasFile(), run: focusSecond },
    { id: "wrap", label: "cmd.wrap", keys: "Alt+Z", run: () => (wordWrap.value = !wordWrap.peek()) },
    { id: "aiExplain", label: "ai.explain", when: hasFile, run: () => activeDesk.peek()?.aiAct("explain") },
    { id: "aiFix", label: "ai.fix", when: hasFile, run: () => activeDesk.peek()?.aiAct("fix") },
    { id: "aiEdit", label: "ai.edit", keys: `${mod}+I`, when: hasFile, run: () => activeDesk.peek()?.aiAct("edit") },
    { id: "aiDoc", label: "ai.doc", when: hasFile, run: () => activeDesk.peek()?.aiAct("doc") },
    { id: "showOnLaptop", label: "cmd.show_on_laptop", when: hasFile, run: () => { const f = activeFile.peek(); if (f) showOnLaptop(f); } },
    { id: "sidebar", label: "cmd.toggle_side", keys: `${mod}+B`, run: () => setPanes({ side: !panes.peek().side }) },
    { id: "panel", label: "cmd.toggle_panel", keys: `${mod}+J`, run: () => setPanes({ panel: !panes.peek().panel }) },
    { id: "panelRight", label: "cmd.panel_right", when: () => panes.peek().panelPos !== "right", run: () => setPanes({ panel: true, panelPos: "right" }) },
    { id: "panelDown", label: "cmd.panel_down", when: () => panes.peek().panelPos === "right", run: () => setPanes({ panel: true, panelPos: "bottom" }) },
    { id: "panelMax", label: "cmd.panel_max", run: () => setPanes({ panel: true, panelMax: !panes.peek().panelMax }) },
    { id: "problems", label: "cmd.problems", keys: `${mod}+Shift+M`, run: toggleProblems },
    { id: "chatPanel", label: "cmd.toggle_chat", run: () => setPanes({ chat: !panes.peek().chat }) },
    { id: "explorer", label: "cmd.explorer", run: () => openSide("files") },
    { id: "scm", label: "cmd.scm", run: () => openSide("git") },
    { id: "search", label: "cmd.search", run: () => go({ tab: "more", rest: "search", query: {} }) },
    { id: "tasks", label: "cmd.tasks", run: () => go({ tab: "more", rest: "tasks", query: {} }) },
    { id: "runs", label: "cmd.runs", run: () => go({ tab: "more", rest: "runs", query: {} }) },
    { id: "settings", label: "cmd.settings", run: () => go({ tab: "more", rest: "settings", query: {} }) },
    { id: "terminal", label: "cmd.terminal", keys: "Ctrl+`", run: () => go({ tab: "term", rest: "", query: {} }) },
    { id: "newTerminal", label: "cmd.new_terminal", run: newTerm },
    { id: "chat", label: "cmd.chat", run: () => go({ tab: "chat", rest: "", query: {} }) },
    { id: "newChat", label: "cmd.new_chat", run: newChat },
    { id: "themeDark", label: "cmd.theme_dark", run: () => setPref("theme", "dark") },
    { id: "themeLight", label: "cmd.theme_light", run: () => setPref("theme", "light") },
    { id: "themeSystem", label: "cmd.theme_system", run: () => setPref("theme", "system") },
    ...gitCommands(),
  ];
}

/** cmdTitle is a command's name as the list shows it. */
export function cmdTitle(c: Command): string {
  return c.text ?? (c.label ? t(c.label) : c.id);
}

/** laptopCmds are the laptop's commands for this page (null: not asked yet). */
export const laptopCmds = signal<PaletteItem[] | null>(null);
let laptopAt = 0;
const LAPTOP_TTL_MS = 30_000;

/** asking is the laptop command waiting for the in-page question (AskLaptop). */
export const asking = signal<PaletteItem | null>(null);

/** loadLaptopCmds asks the laptop for its commands (a controller only:
 * a viewer could not run them), at most once per LAPTOP_TTL_MS. */
export function loadLaptopCmds(): void {
  if (link.meta.peek()?.role !== "controller") {
    laptopCmds.value = [];
    return;
  }
  if (laptopCmds.peek() && Date.now() - laptopAt < LAPTOP_TTL_MS) return;
  laptopAt = Date.now();
  link.request("palette.list").then(
    (data) => (laptopCmds.value = (data as PaletteList).items ?? []),
    () => {
      laptopAt = 0;
      if (!laptopCmds.peek()) laptopCmds.value = [];
    },
  );
}

/** laptopCommand is one laptop command as a palette entry. */
function laptopCommand(it: PaletteItem): Command {
  return {
    id: "laptop:" + it.id,
    text: it.title,
    keys: it.keys,
    laptop: { ask: it.ask === true },
    run: () => {
      if (it.ask) {
        asking.value = it;
        return;
      }
      link.request("palette.run", { id: it.id }).then(
        () => toast("ok", t("cmd.ran_laptop", { name: it.title })),
        (e: unknown) => toast("warn", e instanceof Error ? e.message : t("cmd.failed")),
      );
    },
  };
}

/** allCommands is the page's commands, then the laptop's. */
export function allCommands(): Command[] {
  return [...commands(), ...(laptopCmds.value ?? []).map(laptopCommand)];
}

/** recentCmds are the ids of the commands run last, newest first (per browser). */
export const recentCmds = signal<string[]>(loadRecentCmds());

/** noteRun puts id first in the recently used commands. */
export function noteRun(id: string): void {
  const next = [id, ...recentCmds.peek().filter((x) => x !== id)].slice(0, 20);
  recentCmds.value = next;
  saveRecentCmds(next);
}

/** closeActive closes the active editor tab. */
export function closeActive(): void {
  const f = activeFile.peek();
  if (f) closeTab(f);
}

/** focusSecond focuses the second group, splitting the editor first
 * when there is one group. */
export function focusSecond(): void {
  if (groups.peek().length > 1) focusGroup(1);
  else splitEditor(activeFile.peek(), "right");
}

/** cycleTab shows the next (1) or previous (-1) editor tab. */
export function cycleTab(dir: 1 | -1): void {
  const list = tabsNow();
  if (list.length < 2) return;
  const i = list.indexOf(activeFile.peek() ?? "");
  go({ tab: "files", rest: list[(i + dir + list.length) % list.length], query: {} });
}

/** gotoLine moves the desktop editor's cursor to line n (1-based). */
export function gotoLine(n: number): void {
  activeDesk.peek()?.reveal(n);
}
