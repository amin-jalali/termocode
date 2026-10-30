// The desktop workbench (lib/workbench.ts): the termocode TUI's layout in
// the browser. Rail | side panel (tree or git) | editor (tabs, breadcrumb,
// the file) over the bottom panel (terminal) | chat, and a status bar.
// Panels collapse from the rail or the keyboard (Ctrl/⌘+B side, Ctrl/⌘+J
// terminal) and resize by dragging their edges (or with arrow keys on
// the focused edge).
import { Fragment, h, type ComponentChildren } from "preact";
import { useEffect, useRef } from "preact/hooks";
import type { BufItem } from "../../../../webproto/proto";
import { modKey } from "../lib/platform";
import { go, route } from "../lib/router";
import {
  activeFile,
  CHAT_MAX,
  CHAT_MIN,
  closed,
  closeTab,
  opened,
  PANEL_MIN,
  panes,
  quick,
  setPanes,
  showFile,
  SIDE_MAX,
  SIDE_MIN,
  tabsOf,
  wordWrap,
} from "../lib/workbench";
import { link, listOf, store } from "../session";
import type { Tab } from "../state/route";
import { t } from "../strings";
import { ChatView, GitView, MoreView, TermView } from "../views/views";
import { FileView } from "../views/FileView";
import { TreeView } from "../views/Files";
import { IconChevronDown, IconClose, IconFileSmall, IconSidebar } from "./icons";
import { closeActive, cycleTab, tabsNow } from "../lib/commands";
import { Nav } from "./Nav";
import { QuickInput } from "./QuickInput";
import { StatusBar } from "./StatusBar";

function baseName(path: string): string {
  const i = path.lastIndexOf("/");
  return i < 0 ? path : path.slice(i + 1);
}

/** Sash is a drag handle between two panes. It reports the new size from
 * the size at the start plus the pointer's move (dir -1: the pane is on
 * the other side of the handle). */
function Sash({ axis, size, min, max, dir, label, onSize }: { axis: "x" | "y"; size: number; min: number; max: number; dir: 1 | -1; label: string; onSize: (n: number) => void }) {
  const start = useRef<{ at: number; size: number } | null>(null);
  const clamp = (n: number) => Math.round(Math.min(max, Math.max(min, n)));
  return (
    <div
      class={`sash sash-${axis}`}
      role="separator"
      tabIndex={0}
      aria-orientation={axis === "x" ? "vertical" : "horizontal"}
      aria-label={label}
      aria-valuenow={size}
      aria-valuemin={min}
      aria-valuemax={max}
      onPointerDown={(e) => {
        (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
        start.current = { at: axis === "x" ? e.clientX : e.clientY, size };
        document.documentElement.classList.add(axis === "x" ? "is-resizing-x" : "is-resizing-y");
      }}
      onPointerMove={(e) => {
        const s = start.current;
        if (!s) return;
        const d = (axis === "x" ? e.clientX : e.clientY) - s.at;
        onSize(clamp(s.size + dir * d));
      }}
      onPointerUp={() => {
        start.current = null;
        document.documentElement.classList.remove("is-resizing-x", "is-resizing-y");
      }}
      onKeyDown={(e) => {
        const step = e.shiftKey ? 48 : 16;
        const grow = axis === "x" ? (e.key === "ArrowRight" ? 1 : e.key === "ArrowLeft" ? -1 : 0) : e.key === "ArrowDown" ? 1 : e.key === "ArrowUp" ? -1 : 0;
        if (!grow) return;
        e.preventDefault();
        onSize(clamp(size + dir * grow * step));
      }}
    />
  );
}

/** EditorTabs: one tab per open file (this page's and the laptop's). */
function EditorTabs({ tabs, bufs }: { tabs: readonly string[]; bufs: readonly BufItem[] }) {
  const cur = route.value.tab === "more" ? null : activeFile.value;
  if (tabs.length === 0) return null;
  return (
    <div class="ed-tabs" role="tablist" aria-label={t("wb.tabs")}>
      {tabs.map((path) => {
        const b = bufs.find((x) => x.path === path);
        const on = path === cur;
        const name = baseName(path);
        return (
          <div key={path} class={`ed-tab${on ? " is-on" : ""}${b?.dirty ? " is-dirty" : ""}`} title={path} onAuxClick={(e) => e.button === 1 && closeTab(path, tabs)}>
            <button type="button" class="ed-tab-open" role="tab" aria-selected={on ? "true" : "false"} data-path={path} onClick={() => showFile(path)}>
              <IconFileSmall />
              <span class="ed-tab-name" dir="auto">{name}</span>
            </button>
            <button type="button" class="ed-tab-x" aria-label={t("wb.close_tab", { name })} title={t("wb.close_tab", { name })} onClick={() => closeTab(path, tabs)}>
              <span class="ed-tab-dot" aria-hidden="true">●</span>
              <IconClose />
            </button>
          </div>
        );
      })}
    </div>
  );
}

/** Welcome is the editor with no file: what to do next, with the keys. */
function Welcome() {
  const mod = modKey();
  return (
    <section class="view wb-welcome" data-view="editor" aria-labelledby="vh-welcome">
      <div class="wb-welcome-box">
        <p class="wb-mark" aria-hidden="true" />
        <h1 class="wb-welcome-title" id="vh-welcome">
          {t("wb.welcome")}
        </h1>
        <dl class="wb-keys">
          <dt><kbd>{mod}</kbd><kbd>B</kbd></dt>
          <dd>{t("wb.key.side")}</dd>
          <dt><kbd>{mod}</kbd><kbd>J</kbd></dt>
          <dd>{t("wb.key.panel")}</dd>
        </dl>
      </div>
    </section>
  );
}

/** EditorBody is what the editor area shows: More, the active file or
 * the welcome. */
function EditorBody({ more, file }: { more: boolean; file: string | null }) {
  if (more) return <MoreView />;
  if (file) return <FileView key={file} path={file} />;
  return <Welcome />;
}

function PanelHead({ children }: { children: ComponentChildren }) {
  return (
    <div class="wb-panel-head">
      <span class="wb-panel-title">{children}</span>
      <button type="button" class="tb tb-icon" aria-label={t("wb.panel_hide")} title={`${t("wb.panel_hide")} (${modKey()}+J)`} onClick={() => setPanes({ panel: false })}>
        <IconChevronDown />
      </button>
    </div>
  );
}

/** workbenchKeys runs the workbench's shortcuts (also those the editor's
 * frame passes on). Keys the browser keeps for itself (Ctrl+W, Ctrl+Tab
 * in a normal window) work where the browser lets them through, as in an
 * installed app window. */
function workbenchKeys(e: KeyboardEvent): void {
  const mod = e.ctrlKey || e.metaKey;
  const k = e.key.toLowerCase();
  const run = (f: () => void) => {
    e.preventDefault();
    f();
  };
  if (e.key === "F1" || (mod && e.shiftKey && k === "p")) return run(() => (quick.value = { mode: "commands" }));
  if (e.altKey && !mod && (k === "z" || e.code === "KeyZ")) return run(() => (wordWrap.value = !wordWrap.peek()));
  if (e.ctrlKey && k === "tab") return run(() => cycleTab(e.shiftKey ? -1 : 1));
  if (e.ctrlKey && (k === "pagedown" || k === "pageup")) return run(() => cycleTab(k === "pagedown" ? 1 : -1));
  if (!mod || e.altKey || e.shiftKey) return;
  if (k === "p") return run(() => (quick.value = { mode: "files" }));
  if (k === "b") return run(() => setPanes({ side: !panes.peek().side }));
  if (k === "j" || k === "`") return run(() => setPanes({ panel: !panes.peek().panel }));
  if (k === "w") return run(closeActive);
  if (k === "g" && e.ctrlKey && activeFile.peek()) return run(() => (quick.value = { mode: "line" }));
}

/** railTap: the rail of the workbench opens and closes panels, as an IDE's
 * activity bar does. */
function railTap(tab: Tab): void {
  const r = route.peek();
  const p = panes.peek();
  switch (tab) {
    case "files":
      if (r.tab === "files" && p.side) setPanes({ side: false });
      else {
        setPanes({ side: true });
        go({ tab: "files", rest: activeFile.peek() ?? "", query: {} });
      }
      return;
    case "git":
      if (r.tab === "git" && p.side) setPanes({ side: false });
      else {
        setPanes({ side: true });
        go({ tab: "git", rest: "", query: {} });
      }
      return;
    case "term":
      if (r.tab === "term" && p.panel) {
        setPanes({ panel: false });
        go({ tab: "files", rest: activeFile.peek() ?? "", query: {} });
      } else go({ tab: "term", rest: "", query: {} });
      return;
    default:
      go({ tab, rest: "", query: {} });
  }
}

export function Workbench() {
  useEffect(() => link.subscribe("bufs"), []);
  // The workbench's keys (lib/commands.ts lists them with their names).
  useEffect(() => {
    addEventListener("keydown", workbenchKeys);
    return () => removeEventListener("keydown", workbenchKeys);
  }, []);
  const r = route.value;
  const p = panes.value;
  const bufs = listOf<BufItem>(store.topic("bufs").value?.data, "bufs").filter((b) => typeof b.path === "string" && b.path);
  const tabs = tabsOf(opened.value, bufs, closed.value);
  tabsNow.list = tabs;
  const file = activeFile.value;
  const sideGit = r.tab === "git";
  return (
    <div class="wb" style={{ "--side-w": p.sideW + "px", "--panel-h": p.panelH + "px", "--chat-w": p.chatW + "px" }}>
      <Nav variant="rail" active={r.tab} onTap={railTap} />
      {p.side && (
        <aside class="wb-side" aria-label={sideGit ? t("tab.git") : t("tab.files")}>
          {sideGit ? <GitView /> : <TreeView />}
        </aside>
      )}
      {p.side && <Sash axis="x" size={p.sideW} min={SIDE_MIN} max={SIDE_MAX} dir={1} label={t("wb.resize_side")} onSize={(n) => setPanes({ sideW: n })} />}
      <div class="wb-center">
        <div class="wb-editor">
          {!p.side && (
            <button type="button" class="tb tb-icon wb-side-show" aria-label={t("wb.side_show")} title={`${t("wb.side_show")} (${modKey()}+B)`} onClick={() => setPanes({ side: true })}>
              <IconSidebar />
            </button>
          )}
          <EditorTabs tabs={tabs} bufs={bufs} />
          {quick.value && <QuickInput key={quick.value.mode} />}
          <main class="main" id="main" tabIndex={-1}>
            <EditorBody more={r.tab === "more"} file={file} />
          </main>
        </div>
        {p.panel && (
          <>
            <Sash axis="y" size={p.panelH} min={PANEL_MIN} max={Math.max(PANEL_MIN, innerHeight - 200)} dir={-1} label={t("wb.resize_panel")} onSize={(n) => setPanes({ panelH: n })} />
            <section class="wb-panel" aria-label={t("tab.term")}>
              <PanelHead>{t("wb.terminal")}</PanelHead>
              <TermView />
            </section>
          </>
        )}
      </div>
      {p.chat && (
        <>
          <Sash axis="x" size={p.chatW} min={CHAT_MIN} max={CHAT_MAX} dir={-1} label={t("wb.resize_chat")} onSize={(n) => setPanes({ chatW: n })} />
          <aside class={`chatcol${r.tab === "chat" ? " is-focus" : ""}`} aria-label={t("tab.chat")}>
            <ChatView />
          </aside>
        </>
      )}
    </div>
  );
}

export { StatusBar };
