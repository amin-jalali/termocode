// The workbench's commands (Ctrl/Cmd+Shift+P): what the page can do, by
// name, with its keys. Each runs here or asks the laptop through the
// same ops as the buttons do; nothing new reaches the laptop.
import type { StringKey } from "../strings";
import { activeDesk } from "./deskeditor";
import { saveLive } from "../views/LiveEditor";
import { newChat } from "../views/Chat";
import { newTerm } from "../views/Term";
import { showOnLaptop } from "../views/FileView";
import { modKey } from "./platform";
import { setPref } from "./prefs";
import { go, route } from "./router";
import { activeFile, closeTab, panes, quick, setPanes, wordWrap } from "./workbench";

export interface Command {
  id: string;
  label: StringKey;
  /** keys is the shortcut shown beside it ("Ctrl+B"). */
  keys?: string;
  /** when: shown only when it can run now. */
  when?: () => boolean;
  run: () => void;
}

/** tabsNow is the editor's tab list, set by the workbench as it draws. */
export const tabsNow: { list: readonly string[] } = { list: [] };

const hasFile = () => activeFile.peek() !== null && route.peek().tab !== "more";

/** commands is the list, built on each open (the key names follow the system). */
export function commands(): Command[] {
  const mod = modKey();
  return [
    { id: "quickOpen", label: "cmd.quick_open", keys: `${mod}+P`, run: () => (quick.value = { mode: "files" }) },
    { id: "gotoLine", label: "cmd.goto_line", keys: "Ctrl+G", when: hasFile, run: () => (quick.value = { mode: "line" }) },
    { id: "save", label: "cmd.save", keys: `${mod}+S`, when: hasFile, run: saveLive },
    { id: "closeEditor", label: "cmd.close_editor", keys: `${mod}+W`, when: hasFile, run: () => closeActive() },
    { id: "nextEditor", label: "cmd.next_editor", keys: "Ctrl+Tab", when: () => tabsNow.list.length > 1, run: () => cycleTab(1) },
    { id: "prevEditor", label: "cmd.prev_editor", keys: "Ctrl+Shift+Tab", when: () => tabsNow.list.length > 1, run: () => cycleTab(-1) },
    { id: "wrap", label: "cmd.wrap", keys: "Alt+Z", run: () => (wordWrap.value = !wordWrap.peek()) },
    { id: "showOnLaptop", label: "cmd.show_on_laptop", when: hasFile, run: () => { const f = activeFile.peek(); if (f) showOnLaptop(f); } },
    { id: "sidebar", label: "cmd.toggle_side", keys: `${mod}+B`, run: () => setPanes({ side: !panes.peek().side }) },
    { id: "panel", label: "cmd.toggle_panel", keys: `${mod}+J`, run: () => setPanes({ panel: !panes.peek().panel }) },
    { id: "chatPanel", label: "cmd.toggle_chat", run: () => setPanes({ chat: !panes.peek().chat }) },
    { id: "explorer", label: "cmd.explorer", run: () => (setPanes({ side: true }), go({ tab: "files", rest: activeFile.peek() ?? "", query: {} })) },
    { id: "scm", label: "cmd.scm", run: () => (setPanes({ side: true }), go({ tab: "git", rest: "", query: {} })) },
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
  ];
}

/** closeActive closes the active editor tab. */
export function closeActive(): void {
  const f = activeFile.peek();
  if (f) closeTab(f, tabsNow.list);
}

/** cycleTab shows the next (1) or previous (-1) editor tab. */
export function cycleTab(dir: 1 | -1): void {
  const list = tabsNow.list;
  if (list.length < 2) return;
  const i = list.indexOf(activeFile.peek() ?? "");
  go({ tab: "files", rest: list[(i + dir + list.length) % list.length], query: {} });
}

/** gotoLine moves the desktop editor's cursor to line n (1-based). */
export function gotoLine(n: number): void {
  activeDesk.peek()?.reveal(n);
}
