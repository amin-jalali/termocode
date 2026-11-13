// The workbench's commands (Ctrl/Cmd+Shift+P): what the page can do, by
// name, with its keys. Each runs here or asks the laptop through the
// same ops as the buttons do. Beside them, the laptop palette's commands
// the laptop offers to the web (palette.list: its own allow-list); one
// runs there by id, and a risky one only after a laptop card. The
// commands used last come first (kept per browser).
import { signal } from "@preact/signals";
import type { FilesListRes, UserSnippetList, WorkspaceList, BookmarkList, BookmarkToggle, FilesRecent, LspSymbols, PaletteItem, PaletteList, SnippetList } from "../../../../webproto/proto";
import { link, store } from "../session";
import type { StringKey } from "../strings";
import { t } from "../strings";
import { toast } from "./toasts";
import { activeDesk, lineComment } from "./deskeditor";
import type { EditAct } from "../monaco/protocol";
import { saveLive } from "../views/LiveEditor";
import { newChat } from "../views/Chat";
import { chatFlow, exportChat, INFO_KINDS, moreChat, openAutoApprove, openInfo, openMCPProfiles, startFork } from "../views/ChatMore";
import { newTerm } from "../views/Term";
import { showOnLaptop } from "../views/FileView";
import { isMac, modKey } from "./platform";
import { loadRecentCmds, prefs, saveRecentCmds, setPref } from "./prefs";
import { toggleProblems } from "./lsp";
import { moreCommands } from "./morecmds";
import { go, route } from "./router";
import { openSide } from "../ui/ActivityBar";
import { httpForm } from "./views";
import { gitCommands } from "../views/git/gitcommands";
import { askLaptopForm, openItermoc } from "../views/ItermocView";
import { dbCommands } from "../views/desk/dbcommands";
import { ctrFilter, ctrSearch, ctrSel, openPort } from "../views/desk/DeskContainers";
import { configureServices, openCompose } from "../views/run/ServicesView";
import { activeFile, alternateFile, clips, closeGroup, closeTab, cursorAt, focusedGroup, focusGroup, groups, panes, previewTabs, quick, reopenClosed, setPanes, showFile, splitEditor, tabsIn, togglePin, wordWrap, zen } from "./workbench";
import { askLink, rowLinks } from "./links";
import { expandTo, hideDots } from "./tree";
import { sideView } from "../ui/ActivityBar";
import { renameFile } from "../views/Files";
import { undoRename } from "../views/files/FileActions";
import { fileOfTab } from "./difftab";
import { applyFix, formatBuf, goTo, inlayHints, quickFixes } from "./lsp";
import { session } from "./live";
import { canDictate, dictateChat, toggleDictate } from "./dictate";
import { askHere, askLaptop } from "../ui/AskHere";
import { gitFailed } from "../views/git/errors";
import { mdBufs } from "../views/MDPreview";
import { cmpTab, mdTab } from "./vtabs";
import { openPair } from "./device";

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

/** pasteHistory lists the tab's clipboard history; the pick goes in at
 * the focused editor's caret as the user's own edit. */
function pasteHistory(): void {
  const list = clips.peek();
  if (!list.length) {
    toast("info", t("clip.empty"));
    return;
  }
  const one = (s: string) => (s.length > 80 ? s.slice(0, 79) + "…" : s).replace(/\s+/g, " ");
  quick.value = {
    mode: "pick",
    pick: {
      title: t("clip.title"),
      items: list.map((s) => ({
        label: one(s),
        detail: s.includes("\n") ? t("clip.lines", { n: s.split("\n").length }) : undefined,
        run: () => {
          const d = activeDesk.peek();
          if (!d) return;
          setTimeout(() => {
            d.insert(s);
            d.focus();
          }, 0);
        },
      })),
    },
  };
}

/** cursors runs a multi-cursor command in the focused editor. */
function cursors(op: "above" | "below" | "next" | "all"): void {
  activeDesk.peek()?.cursors(op);
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
    // Multi-cursor (WEBP-1): the editor's own actions, with its keys.
    { id: "cursorAbove", label: "cmd.cursor_above", keys: isMac() ? "⌥⌘↑" : "Ctrl+Alt+Up", when: hasFile, run: () => cursors("above") },
    { id: "cursorBelow", label: "cmd.cursor_below", keys: isMac() ? "⌥⌘↓" : "Ctrl+Alt+Down", when: hasFile, run: () => cursors("below") },
    { id: "nextMatch", label: "cmd.next_match", keys: `${mod}+D`, when: hasFile, run: () => cursors("next") },
    { id: "allMatches", label: "cmd.all_matches", keys: `${mod}+Shift+L`, when: hasFile, run: () => cursors("all") },
    { id: "pasteHistory", label: "cmd.paste_history", when: hasFile, run: pasteHistory },
    { id: "vim", label: "cmd.vim", run: () => setPref("vim", !prefs.peek().vim) },
    { id: "zen", label: "cmd.zen", keys: `${mod}+K Z`, run: () => (zen.value = !zen.peek()) },
    { id: "aiExplain", label: "ai.explain", when: hasFile, run: () => activeDesk.peek()?.aiAct("explain") },
    { id: "aiFix", label: "ai.fix", when: hasFile, run: () => activeDesk.peek()?.aiAct("fix") },
    { id: "aiEdit", label: "ai.edit", keys: `${mod}+I`, when: hasFile, run: () => activeDesk.peek()?.aiAct("edit") },
    { id: "aiDoc", label: "ai.doc", when: hasFile, run: () => activeDesk.peek()?.aiAct("doc") },
    ...moreCommands(),
    { id: "itermoc", label: "cmd.itermoc", run: () => openItermoc(null) },
    { id: "itermocProfiles", label: "cmd.itermoc_profiles", run: () => openItermoc("profiles") },
    { id: "itermocModels", label: "cmd.itermoc_models", run: () => openItermoc("models") },
    { id: "itermocServices", label: "cmd.itermoc_services", run: () => openItermoc("services") },
    { id: "itermocAgents", label: "cmd.itermoc_agents", run: () => openItermoc("agents") },
    { id: "aiProfileShow", label: "cmd.ai_profile_show", run: () => openItermoc("profiles") },
    { id: "aiProfileNext", label: "cmd.ai_profile_next", run: () => openItermoc("profiles", { kind: "next" }) },
    { id: "aiAdd", label: "cmd.ai_add", run: () => askLaptopForm("ai-add") },
    { id: "aiModelSlotAdd", label: "cmd.ai_slot_add", run: () => askLaptopForm("ai-model-slot-add") },
    { id: "aiModelsEdit", label: "cmd.ai_slots_edit", run: () => askLaptopForm("ai-models-edit") },
    { id: "aiProfileEdit", label: "cmd.ai_profiles_edit", run: () => askLaptopForm("ai-profile-edit") },
    { id: "aiProfileNew", label: "cmd.ai_profile_new", run: () => askLaptopForm("ai-profile-new") },
    { id: "aiProfileForm", label: "cmd.ai_profile_form", run: () => askLaptopForm("ai-profile-form") },
    { id: "aiAttach", label: "ai.attach", when: hasFile, run: () => activeDesk.peek()?.aiAct("attach") },
    { id: "aiDictate", label: "cmd.ai_dictate", when: () => canDictate() && dictateChat.peek() !== null, run: toggleDictate },
    { id: "agents", label: "cmd.agents", run: () => (setPanes({ side: true }), go({ tab: "more", rest: "agents", query: {} })) },
    { id: "worktrees", label: "cmd.worktrees", run: () => (setPanes({ side: true }), go({ tab: "more", rest: "worktrees", query: {} })) },
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
    { id: "httpSendPick", label: "cmd.http_send", run: () => openSide("http") },
    { id: "httpQuick", label: "cmd.http_quick", run: () => ((httpForm.value = "quick"), openSide("http")) },
    { id: "search", label: "cmd.search", run: () => go({ tab: "more", rest: "search", query: {} }) },
    { id: "tasks", label: "cmd.tasks", run: () => go({ tab: "more", rest: "tasks", query: {} }) },
    { id: "newTask", label: "cmd.new_task", run: () => go({ tab: "more", rest: "new-task", query: {} }) },
    { id: "runs", label: "cmd.runs", run: () => go({ tab: "more", rest: "runs", query: {} }) },
    { id: "settings", label: "cmd.settings", run: () => go({ tab: "more", rest: "settings", query: {} }) },
    { id: "terminal", label: "cmd.terminal", keys: "Ctrl+`", run: () => go({ tab: "term", rest: "", query: {} }) },
    { id: "newTerminal", label: "cmd.new_terminal", run: newTerm },
    { id: "chat", label: "cmd.chat", run: () => go({ tab: "chat", rest: "", query: {} }) },
    { id: "newChat", label: "cmd.new_chat", run: newChat },
    ...chatCommands(),
    { id: "themeDark", label: "cmd.theme_dark", run: () => setPref("theme", "dark") },
    { id: "themeLight", label: "cmd.theme_light", run: () => setPref("theme", "light") },
    { id: "themeSystem", label: "cmd.theme_system", run: () => setPref("theme", "system") },
    { id: "containersFilter", label: "ctr.filter", run: () => (openSide("containers"), (ctrFilter.value = ctrFilter.peek() ?? "")) },
    { id: "containersSearch", label: "ctr.search", when: canAct, run: () => (openSide("containers"), (ctrSearch.value = true)) },
    { id: "containersOpenPort", label: "ctr.open_port", when: () => !!ctrSel.peek()?.ports, run: () => openPort(ctrSel.peek()) },
    { id: "servicesOpenCompose", label: "svc.open_compose", run: openCompose },
    { id: "servicesConfigure", label: "svc.configure", when: canAct, run: configureServices },
    ...gitCommands(),
    ...dbCommands(),
    ...editCommands(),
    { id: "lastFile", label: "cmd.last_file", when: () => activeFile.peek() !== null, run: () => alternateFile() || toast("info", t("cmd.no_last_file")) },
    { id: "reopenClosed", label: "cmd.reopen_closed", run: () => reopenClosed() || toast("info", t("cmd.no_closed")) },
    { id: "pinTab", label: "cmd.pin_tab", when: () => activeFile.peek() !== null, run: () => { const f = activeFile.peek(); if (f) togglePin(f); } },
    { id: "closeSplit", label: "cmd.close_split", when: () => groups.peek().length > 1, run: () => closeGroup() },
    { id: "openUrl", label: "cmd.open_url", when: hasDesk, run: openRowURL },
    { id: "revealFile", label: "cmd.reveal_file", when: () => realFile() !== null, run: () => { const f = realFile(); if (f) revealInTree(f); } },
    { id: "toggleHidden", label: "cmd.toggle_hidden", run: toggleHidden },
    { id: "togglePreview", label: "cmd.toggle_preview", run: togglePreviewTabs },
    { id: "renameFile", label: "cmd.rename_file", when: () => realFile() !== null && link.meta.peek()?.role === "controller", run: () => { const f = realFile(); if (f) { showSide(); renameFile(f); } } },
    { id: "gotoTypeDef", label: "cmd.goto_typedef", when: hasDesk, run: () => activeDesk.peek()?.nav("typedef") },
    { id: "gotoImpl", label: "cmd.goto_impl", when: hasDesk, run: () => activeDesk.peek()?.nav("impl") },
    { id: "formatDocument", label: "menu.format", when: () => hasDesk() && canWrite(), run: () => void formatBuf() },
    { id: "revertFile", label: "cmd.revert_file", when: () => hasDesk() && canWrite(), run: askRevert },
    { id: "markdownPreview", label: "cmd.md_preview", when: () => hasDesk() && /\.(md|markdown|mdx)$/i.test(activeFile.peek() ?? ""), run: openPreview },
    { id: "bookmarkToggle", label: "cmd.bm_toggle", when: () => hasDesk() && canWrite(), run: toggleBookmark },
    { id: "bookmarkList", label: "cmd.bm_list", run: listBookmarks },
    { id: "bookmarkClear", label: "cmd.bm_clear", when: canWrite, run: askClearBookmarks },
    { id: "openRecent", label: "cmd.open_recent", run: openRecent },
    { id: "workspaceSymbol", label: "cmd.ws_symbol", when: hasDesk, run: workspaceSymbols },
    { id: "snippetPicker", label: "cmd.snippets", when: () => hasDesk() && canWrite(), run: pickSnippet },
    { id: "quickFix", label: "cmd.quick_fix", when: () => hasDesk() && canWrite(), run: () => void pickFix() },
    { id: "quickFixPreferred", label: "cmd.quick_fix_pref", when: () => hasDesk() && canWrite(), run: () => void preferredFix() },
    { id: "inlayHints", label: "cmd.inlay_hints", when: hasDesk, run: () => (inlayHints.value = !inlayHints.peek()) },
    { id: "compareFiles", label: "cmd.compare", run: compareFiles },
    { id: "addFolder", label: "cmd.add_folder", when: canWrite, run: addFolder },
    { id: "removeFolder", label: "cmd.remove_folder", when: canWrite, run: removeFolder },
    { id: "openWorkspace", label: "cmd.open_workspace", when: canWrite, run: openWorkspace },
    { id: "replaceWorkspace", label: "cmd.replace_ws", when: canWrite, run: replaceWorkspace },
    { id: "manageSnippets", label: "cmd.snippets_manage", when: canWrite, run: manageSnippets },
    { id: "undoRename", label: "cmd.undo_rename", when: () => link.meta.peek()?.role === "controller", run: undoRename },
  ];
}

const canAct = () => link.meta.peek()?.role !== "viewer";
const isController = () => link.meta.peek()?.role === "controller";

/** chatCommands act on the laptop's open chat and read the laptop's AI
 * screens (the chat column shows their questions). */
function chatCommands(): Command[] {
  const show = () => setPanes({ chat: true });
  const withChat = (fn: (c: NonNullable<ReturnType<typeof moreChat>>) => void) => () => {
    const c = moreChat();
    if (!c) return;
    show();
    fn(c);
  };
  const hasChat = () => isController() && moreChat() !== null;
  return [
    { id: "chatRename", label: "chat.rename", when: hasChat, run: withChat((c) => (chatFlow.value = { s: "rename", id: c.id, title: c.title })) },
    { id: "chatFork", label: "chat.fork", when: hasChat, run: withChat((c) => startFork(c.msgs)) },
    { id: "chatExport", label: "chat.export", when: hasChat, run: withChat((c) => exportChat(c.id)) },
    { id: "chatDelete", label: "chat.delete", when: hasChat, run: withChat((c) => (chatFlow.value = { s: "delete", id: c.id, title: c.title })) },
    { id: "chatClear", label: "chat.clear", when: isController, run: () => (show(), (chatFlow.value = { s: "clear" })) },
    { id: "mcpProfile", label: "mcp.switch", when: isController, run: () => (show(), openMCPProfiles()) },
    { id: "autoApprove", label: "auto.title", when: isController, run: () => (show(), openAutoApprove()) },
    ...INFO_KINDS.map(([kind, key]) => ({ id: "aiInfo:" + kind, label: key, when: isController, run: () => (show(), openInfo(kind)) })),
  ];
}

/** canWrite: a controller may change files. */
const canWrite = () => link.meta.peek()?.role === "controller";

/** askRevert asks here first (Cancel first): Revert File drops the
 * buffer's unsaved changes and reads the file from disk again. */
function askRevert(): void {
  const s = session.peek();
  const f = activeFile.peek();
  if (!s || !f) return;
  const name = f.slice(f.lastIndexOf("/") + 1);
  askHere.value = {
    label: t("cmd.revert_file"),
    text: t("cmd.revert_ask", { name }),
    verb: t("cmd.revert"),
    run: () => link.request("buf.reload", { buf: s.buf }).then(() => toast("ok", t("cmd.reverted")), gitFailed),
  };
}

/** openPreview opens the Markdown preview tab of the focused file. */
function openPreview(): void {
  const s = session.peek();
  const f = activeFile.peek();
  if (!s || !f) return;
  mdBufs.set(f, s.buf);
  showFile(mdTab(f));
}

/** failToast shows a request's refusal in the laptop's words. */
const failToast = (e: unknown) => toast("warn", e instanceof Error && e.message ? e.message : t("cmd.failed"));

/** baseOf is a path's last part; dirOf the folders before it. */
const baseOf = (p: string) => p.slice(p.lastIndexOf("/") + 1);
const dirOf = (p: string) => p.slice(0, Math.max(0, p.lastIndexOf("/")));

/** toggleBookmark sets the laptop's bookmark on the caret's row, or takes it away. */
function toggleBookmark(): void {
  const s = session.peek();
  if (!s) return;
  const line = (cursorAt.peek()?.line ?? 1) - 1;
  link.request("bookmarks.toggle", { buf: s.buf, line }).then((r) => {
    const b = r as BookmarkToggle;
    toast("ok", t(b.on ? "cmd.bm_set" : "cmd.bm_off", { mark: b.mark ?? "" }));
  }, failToast);
}

/** listBookmarks lists the laptop's bookmarks; one opens at its row. */
function listBookmarks(): void {
  link.request("bookmarks.list").then((r) => {
    const items = (r as BookmarkList).items ?? [];
    if (items.length === 0) {
      toast("info", t("cmd.bm_none"));
      return;
    }
    quick.value = {
      mode: "pick",
      pick: { title: t("cmd.bm_list"), items: items.map((b) => ({ label: `${b.mark}  ${b.path}:${b.line + 1}`, detail: b.text, file: b.path, run: () => goTo(b.path, b.line, 0) })) },
    };
  }, failToast);
}

/** askClearBookmarks asks here first (Cancel first): all of the laptop's bookmarks go. */
function askClearBookmarks(): void {
  askHere.value = {
    label: t("cmd.bm_clear"),
    text: t("cmd.bm_clear_ask"),
    verb: t("cmd.bm_clear_verb"),
    run: () => link.request("bookmarks.clear").then(() => toast("ok", t("cmd.bm_cleared")), failToast),
  };
}

/** openRecent lists the laptop's recent files in the workspace. */
function openRecent(): void {
  link.request("files.recent").then((r) => {
    const paths = (r as FilesRecent).paths ?? [];
    if (paths.length === 0) {
      toast("info", t("cmd.no_recent"));
      return;
    }
    quick.value = { mode: "pick", pick: { title: t("cmd.open_recent"), items: paths.map((p) => ({ label: baseOf(p), detail: dirOf(p), file: p, run: () => showFile(p) })) } };
  }, failToast);
}

/** workspaceSymbols asks the laptop's language servers as the user types. */
function workspaceSymbols(): void {
  const s = session.peek();
  if (!s) return;
  const buf = s.buf;
  quick.value = {
    mode: "pick",
    pick: {
      title: t("cmd.ws_symbol_type"),
      load: (q) =>
        q.trim() === ""
          ? Promise.resolve([])
          : link.request("lsp.workspace_symbols", { buf, query: q.trim() }).then((r) =>
              ((r as LspSymbols).items ?? []).map((x) => ({ label: x.name, detail: x.container ? `${x.container} · ${x.path}` : x.path, run: () => goTo(x.path, x.line, x.col) })),
            ),
    },
  };
}

/** pickSnippet lists the snippets of the file's type; one goes in at the cursor. */
function pickSnippet(): void {
  const s = session.peek();
  if (!s) return;
  link.request("snippets.list", { buf: s.buf }).then((r) => {
    const items = (r as SnippetList).items ?? [];
    if (items.length === 0) {
      toast("info", t("cmd.no_snippets"));
      return;
    }
    quick.value = { mode: "pick", pick: { title: t("cmd.snippets"), items: items.map((x) => ({ label: x.trigger, detail: x.body.split("\n")[0], run: () => activeDesk.peek()?.snippet(x.body) })) } };
  }, failToast);
}

/** pickFix lists the quick fixes at the caret; the one picked is applied on the laptop. */
async function pickFix(): Promise<void> {
  const r = await quickFixes();
  if (!r) return;
  if (r.items.length === 0) {
    toast("info", t("cmd.no_fix"));
    return;
  }
  quick.value = {
    mode: "pick",
    pick: { title: t("cmd.quick_fix"), items: r.items.map((a, i) => ({ label: a.title, detail: a.preferred ? t("cmd.fix_preferred") : a.kind, run: () => applyFix(r.buf, r.tick, i) })) },
  };
}

/** preferredFix applies the preferred quick fix at the caret at once. */
async function preferredFix(): Promise<void> {
  const r = await quickFixes();
  if (!r) return;
  const i = r.items.findIndex((a) => a.preferred);
  if (i < 0) {
    toast("info", t("cmd.no_pref_fix"));
    return;
  }
  applyFix(r.buf, r.tick, i);
}

/** compareFiles compares the open file (or a first one picked) with a
 * file picked from the workspace, side by side as they are on disk. */
function compareFiles(): void {
  link.request("files.list").then((r) => {
    const paths = (r as FilesListRes).paths ?? [];
    const pickOf = (title: string, then: (p: string) => void) =>
      (quick.value = { mode: "pick", pick: { title, items: paths.map((p) => ({ label: baseOf(p), detail: dirOf(p), file: p, run: () => then(p) })) } });
    const second = (a: string) => pickOf(t("cmd.compare_with", { name: baseOf(a) }), (b) => showFile(cmpTab(a, b)));
    const a = realFile();
    if (a) second(a);
    else pickOf(t("cmd.compare"), (p) => setTimeout(() => second(p), 0));
  }, failToast);
}

/** addFolder asks for a laptop folder's path, then asks the laptop to
 * add it to the workspace (its card shows the whole path). */
function addFolder(): void {
  quick.value = {
    mode: "ask",
    ask: {
      title: t("cmd.folder_type"),
      run: (path) => {
        askLaptop.value = { label: t("cmd.add_folder"), text: t("cmd.add_folder_ask", { path }), send: () => link.request("workspace.add_folder", { folder: path }) };
      },
    },
  };
}

/** workspaceList reads the folder and recent workspace names. */
function workspaceList(then: (l: WorkspaceList) => void): void {
  link.request("workspace.list").then((r) => then(r as WorkspaceList), failToast);
}

/** removeFolder picks an added folder; the laptop removes it after its card. */
function removeFolder(): void {
  workspaceList((l) => {
    const extra = (l.folders ?? []).filter((f) => f.id > 0);
    if (extra.length === 0) {
      toast("info", t("cmd.no_folders"));
      return;
    }
    quick.value = {
      mode: "pick",
      pick: {
        title: t("cmd.remove_folder"),
        items: extra.map((f) => ({ label: f.name, run: () => (askLaptop.value = { label: t("cmd.remove_folder"), text: t("cmd.remove_folder_ask", { name: f.name }), send: () => link.request("workspace.remove_folder", { id: f.id }) }) })),
      },
    };
  });
}

/** openWorkspace picks a recent workspace; the laptop restarts there after its card. */
function openWorkspace(): void {
  workspaceList((l) => {
    const recent = l.recent ?? [];
    if (recent.length === 0) {
      toast("info", t("cmd.no_workspaces"));
      return;
    }
    quick.value = {
      mode: "pick",
      pick: {
        title: t("cmd.open_workspace"),
        items: recent.map((w) => ({ label: w.name, run: () => (askLaptop.value = { label: t("cmd.open_workspace"), text: t("cmd.open_workspace_body", { name: w.name }), send: () => link.request("workspace.open", { id: w.id }) }) })),
      },
    };
  });
}

/** workspaceMenu lists the workspace's actions (the status bar's remote
 * item, as the laptop's project switcher): open a recent workspace, add
 * or remove a folder. Each asks the laptop as from the command list. */
export function workspaceMenu(): void {
  if (!canWrite()) {
    toast("info", t("viewonly"));
    return;
  }
  const later = (f: () => void) => () => setTimeout(f, 0);
  const items = [
    { label: t("cmd.open_workspace"), run: later(openWorkspace) },
    { label: t("cmd.add_folder"), run: later(addFolder) },
    { label: t("cmd.remove_folder"), run: later(removeFolder) },
  ];
  quick.value = { mode: "pick", pick: { title: t("sb.workspace"), items } };
}

/** replaceWorkspace asks for the text and its replacement here, then the
 * laptop's card counts the matches; its yes writes the files. */
function replaceWorkspace(): void {
  quick.value = {
    mode: "ask",
    ask: {
      title: t("cmd.replace_find"),
      run: (find) =>
        setTimeout(() => {
          quick.value = {
            mode: "ask",
            ask: {
              title: t("cmd.replace_with", { find }),
              run: (replace) => {
                askLaptop.value = { label: t("cmd.replace_ws"), text: t("cmd.replace_ws_ask", { find, replace }), send: () => link.request("search.replace", { find, replace }) };
              },
            },
          };
        }, 0),
    },
  };
}

/** unescapeBody turns the one-line input into a body: \\n a new line,
 * \\t a tab, \\\\ a backslash (other escapes stay, as snippets need them). */
function unescapeBody(s: string): string {
  return s.replace(/\\([nt\\])/g, (_m, c: string) => (c === "n" ? "\n" : c === "t" ? "\t" : "\\"));
}

/** manageSnippets lists the user's own snippets: a new one is named and
 * typed here; one picked is deleted after a question here. */
function manageSnippets(): void {
  link.request("snippets.user").then((r) => {
    const items = (r as UserSnippetList).items ?? [];
    const name = (scope: string, trigger: string) => (scope === "*" ? trigger : scope + ":" + trigger);
    quick.value = {
      mode: "pick",
      pick: {
        title: t("cmd.snippets_manage"),
        items: [
          { label: t("cmd.snippet_new"), run: () => setTimeout(newSnippet, 0) },
          ...items.map((s) => ({
            label: s.trigger,
            detail: "[" + s.scope + "] " + s.body.split("\n")[0],
            run: () =>
              (askHere.value = {
                label: t("cmd.snippets_manage"),
                text: t("cmd.snippet_delete_ask", { name: name(s.scope, s.trigger) }),
                verb: t("cmd.snippet_delete"),
                run: () => link.request("snippets.delete", { scope: s.scope, trigger: s.trigger }).then(() => toast("ok", t("cmd.snippet_deleted", { name: name(s.scope, s.trigger) })), failToast),
              }),
          })),
        ],
      },
    };
  }, failToast);
}

/** newSnippet asks the snippet's name, then its body, and saves it. */
function newSnippet(): void {
  quick.value = {
    mode: "ask",
    ask: {
      title: t("cmd.snippet_name"),
      run: (raw) => {
        const i = raw.lastIndexOf(":");
        const scope = i >= 0 ? raw.slice(0, i).trim() || "*" : "*";
        const trigger = (i >= 0 ? raw.slice(i + 1) : raw).trim();
        if (!/^[A-Za-z0-9_]+$/.test(trigger)) {
          toast("warn", t("cmd.snippet_bad"));
          return;
        }
        const label = scope === "*" ? trigger : scope + ":" + trigger;
        setTimeout(
          () =>
            (quick.value = {
              mode: "ask",
              ask: {
                title: t("cmd.snippet_body", { name: label }),
                run: (body) =>
                  link.request("snippets.save", { scope, trigger, body: unescapeBody(body) }).then(() => toast("ok", t("cmd.snippet_saved", { name: label })), failToast),
              },
            }),
          0,
        );
      },
    },
  };
}

/** realFile is the workspace file of the active tab (null: none, or a tab
 * that is not a file: a table, a commit graph …). */
function realFile(): string | null {
  const a = activeFile.peek();
  return a ? fileOfTab(a) : null;
}

/** showSide shows the files in the side bar. */
function showSide(): void {
  setPanes({ side: true });
  sideView.value = "files";
}

/** revealInTree opens the file's folders in the tree and shows its row. */
function revealInTree(path: string): void {
  showSide();
  expandTo(path);
  const end = Date.now() + 3000;
  const find = () => {
    const row = document.querySelector<HTMLElement>(`.wb-side .tree-row[data-path="${CSS.escape(path)}"]`);
    if (row) {
      row.scrollIntoView({ block: "nearest" });
      row.focus();
    } else if (Date.now() < end) setTimeout(find, 50);
  };
  find();
}

/** toggleHidden hides the dotfiles here or shows them again (the laptop
 * must show them for the page to). */
function toggleHidden(): void {
  const d = store.topic("tree").peek()?.data as { hidden?: unknown } | undefined;
  if (d?.hidden !== true) {
    toast("info", t("files.dots_laptop"));
    return;
  }
  hideDots.value = !hideDots.peek();
}

/** togglePreviewTabs turns the tree's preview tabs off or on again. */
function togglePreviewTabs(): void {
  previewTabs.value = !previewTabs.peek();
  toast("info", t(previewTabs.peek() ? "cmd.preview_on" : "cmd.preview_off"));
}

/** openRowURL asks to open the URL on the caret's row (the one under the
 * caret, else the first); a row with none says so. */
function openRowURL(): void {
  const at = cursorAt.peek();
  const text = activeDesk.peek()?.text() ?? "";
  const row = text.split("\n")[(at?.line ?? 1) - 1] ?? "";
  const found = rowLinks(row);
  const col = (at?.col ?? 1) - 1;
  const hit = found.find((f) => col >= f.from && col <= f.from + f.url.length) ?? found[0];
  if (!hit) {
    toast("info", t("cmd.no_url"));
    return;
  }
  askLink(hit.url);
}

/** hasDesk: a file is open in the desktop editor. */
const hasDesk = () => hasFile() && activeDesk.peek() !== null;

/** EDIT_ACTS are the editor's line and text commands (the laptop's
 * Edit: and fold entries): id, name, the laptop's key. */
const EDIT_ACTS: { id: EditAct; label: StringKey; keys?: string }[] = [
  { id: "copyLinesDown", label: "cmd.copy_lines_down", keys: "Shift+Alt+Down" },
  { id: "commentLine", label: "cmd.comment_line", keys: "Ctrl+/" },
  { id: "foldAll", label: "cmd.fold_all" },
  { id: "unfoldAll", label: "cmd.unfold_all" },
  { id: "toggleFold", label: "cmd.toggle_fold" },
  { id: "joinLines", label: "cmd.join_lines" },
  { id: "sortLinesAsc", label: "cmd.sort_asc" },
  { id: "sortLinesDesc", label: "cmd.sort_desc" },
  { id: "reverseLines", label: "cmd.reverse_lines" },
  { id: "upperCase", label: "cmd.upper" },
  { id: "lowerCase", label: "cmd.lower" },
  { id: "trimWhitespace", label: "cmd.trim_ws" },
  { id: "tabsToSpaces", label: "cmd.tabs_to_spaces" },
  { id: "spacesToTabs", label: "cmd.spaces_to_tabs" },
  { id: "collapseBlankLines", label: "cmd.collapse_blank" },
  { id: "replaceInFile", label: "cmd.replace_file", keys: "Ctrl+H" },
];

/** editCommands run on the focused desktop editor's text. */
function editCommands(): Command[] {
  const mod = modKey();
  return EDIT_ACTS.map(({ id, label, keys }) => ({
    id,
    label,
    keys: keys?.replace("Ctrl", mod),
    when: hasDesk,
    run: () => {
      if (id === "commentLine" && !lineComment(activeFile.peek() ?? "")) {
        toast("info", t("cmd.no_comment"));
        return;
      }
      activeDesk.peek()?.act(id);
    },
  }));
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
    // page: the page opens it itself (Pair a Device…, Devices…).
    laptop: it.page ? undefined : { ask: it.ask === true },
    run: () => {
      if (it.page) return openPair(it.id);
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
