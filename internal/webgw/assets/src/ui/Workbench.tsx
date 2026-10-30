// The desktop workbench (lib/workbench.ts): the termocode TUI's layout in
// the browser. Rail | side panel (tree or git) | editor (tabs, breadcrumb,
// the file) over the bottom panel (terminal) | chat, and a status bar.
// Panels collapse from the rail or the keyboard (Ctrl/⌘+B side, Ctrl/⌘+J
// terminal) and resize by dragging their edges (or with arrow keys on
// the focused edge).
import { Fragment, h, type ComponentChildren } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import type { BufItem } from "../../../../webproto/proto";
import { modKey } from "../lib/platform";
import { go, route } from "../lib/router";
import {
  activeFile,
  CHAT_MAX,
  CHAT_MIN,
  closeMany,
  closeTab,
  focusedGroup,
  focusGroup,
  groups,
  keep,
  laptopBufs,
  MAX_GROUPS,
  moveTab,
  PANE_DEFAULTS,
  PANEL_MIN,
  PANEL_W_MIN,
  panes,
  quick,
  setPanes,
  showFile,
  SIDE_MAX,
  SIDE_MIN,
  slotOf,
  splitDir,
  splitEditor,
  tabsIn,
  wordWrap,
} from "../lib/workbench";
import { link, listOf, store } from "../session";
import type { Tab } from "../state/route";
import { t } from "../strings";
import { ChatView, GitView, MoreView } from "../views/views";
import { DeskTerm } from "../views/DeskTerm";
import { ExplorerSections } from "../views/Explorer";
import { DiffView } from "../views/DiffView";
import { diffOf, fileOfTab, tabName } from "../lib/difftab";
import { FileView } from "../views/FileView";
import { TreeView } from "../views/Files";
import { Cod, FileIcon } from "./icons";
import { asking, closeActive, cycleTab, focusSecond } from "../lib/commands";
import { AskLaptop } from "./AskLaptop";
import { ContextMenu, onMenu, openMenu, type MenuItems } from "./ContextMenu";
import { absPath, copyText } from "../lib/clip";
import { signal } from "@preact/signals";
import { ACTIVITIES, ACTIVITIES_BOTTOM, ActivityBar, activityOf, sideActivity, type Activity } from "./ActivityBar";
import type { StringKey } from "../strings";
import { MCPView } from "../views/more/MCPView";
import { RunsView } from "../views/more/RunsView";
import { DeskSearch } from "../views/desk/DeskSearch";
import { TasksView } from "../views/more/TasksView";
import { QuickInput } from "./QuickInput";
import { RefsPeek } from "./RefsPeek";
import { diags, panelTab, toggleProblems, type PanelTab } from "../lib/lsp";
import { ProblemsView } from "../views/ProblemsView";
import { StatusBar } from "./StatusBar";

/** Sash is a drag handle between two panes. It reports the new size from
 * the size at the start plus the pointer's move (dir -1: the pane is on
 * the other side of the handle). */
function Sash({ axis, size, min, max, dir, label, def, onSize }: { axis: "x" | "y"; size: number; min: number; max: number; dir: 1 | -1; label: string; def: number; onSize: (n: number) => void }) {
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
      onDblClick={() => onSize(clamp(def))}
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

/** dragTab is the editor tab being dragged (its group), or null. */
const dragTab = signal<{ path: string; from: number } | null>(null);
/** dropAt marks where a dragged tab would land in a tab bar. */
const dropAt = signal<{ group: number; index: number } | null>(null);

function endDrag(): void {
  dragTab.value = null;
  dropAt.value = null;
}

/** EditorTabs: one tab per open file of group g (this page's and, in
 * the first group, the laptop's). A tab drags to another place, to the
 * other group, or to the editor's side (a split); a double click keeps a
 * preview tab. */
function EditorTabs({ group, tabs, bufs }: { group: number; tabs: readonly string[]; bufs: readonly BufItem[] }) {
  const gr = groups.value[group];
  const cur = route.value.tab === "more" && group === focusedGroup.value ? null : (gr?.active ?? null);
  const drop = dropAt.value;
  const over = (e: DragEvent, index: number) => {
    if (!dragTab.peek()) return;
    e.preventDefault();
    if (e.dataTransfer) e.dataTransfer.dropEffect = "move";
    if (drop?.group !== group || drop.index !== index) dropAt.value = { group, index };
  };
  const land = (e: DragEvent, index: number) => {
    const d = dragTab.peek();
    if (!d) return;
    e.preventDefault();
    e.stopPropagation();
    endDrag();
    moveTab(d.path, d.from, group, index);
  };
  if (tabs.length === 0) return null;
  return (
    <div class="ed-tabbar">
    <div class="ed-tabs" role="tablist" aria-label={t("wb.tabs")} data-group={group} onDragOver={(e) => over(e, tabs.length)} onDrop={(e) => land(e, tabs.length)}>
      {tabs.map((path, i) => {
        const b = bufs.find((x) => x.path === path);
        const on = path === cur;
        const name = tabName(path);
        const pv = gr?.preview === path;
        const mark = drop?.group === group && drop.index === i ? " is-drop-before" : drop?.group === group && drop.index === i + 1 && i === tabs.length - 1 ? " is-drop-after" : "";
        return (
          <div
            key={path}
            class={`ed-tab${on ? " is-on" : ""}${b?.dirty ? " is-dirty" : ""}${pv ? " is-preview" : ""}${mark}`}
            title={path}
            data-path={path}
            draggable
            onDragStart={(e) => {
              dragTab.value = { path, from: group };
              if (e.dataTransfer) {
                e.dataTransfer.effectAllowed = "move";
                e.dataTransfer.setData("text/plain", path);
              }
            }}
            onDragEnd={endDrag}
            onDragOver={(e) => {
              const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
              over(e, e.clientX > r.left + r.width / 2 ? i + 1 : i);
              e.stopPropagation();
            }}
            onDrop={(e) => land(e, drop?.group === group ? drop.index : i)}
            onAuxClick={(e) => e.button === 1 && closeTab(path, group)}
            onDblClick={() => keep(path, group)}
            onContextMenu={onMenu(name, () => tabMenu(path, group, tabs, bufs))}
          >
            <button
              type="button"
              class="ed-tab-open"
              role="tab"
              aria-selected={on ? "true" : "false"}
              data-path={path}
              onClick={() => {
                if (group !== focusedGroup.peek()) focusedGroup.value = group;
                showFile(path);
              }}
            >
              <FileIcon path={fileOfTab(path) ?? path} />
              <span class="ed-tab-name" dir="auto">{name}</span>
            </button>
            <button type="button" class="ed-tab-x" aria-label={t("wb.close_tab", { name })} title={`${t("wb.close_tab", { name })} (${modKey()}+W)`} onClick={() => closeTab(path, group)}>
              <Cod name="circle-filled" />
              <Cod name="close" />
            </button>
          </div>
        );
      })}
    </div>
      <div class="ed-actions" id={"ed-actions-" + slotOf(group)} />
    </div>
  );
}

/** tabMenu is an editor tab's context menu. */
function tabMenu(path: string, group: number, tabs: readonly string[], bufs: readonly BufItem[]): MenuItems {
  const mod = modKey();
  const i = tabs.indexOf(path);
  const dirty = (p: string) => bufs.some((b) => b.path === p && b.dirty);
  const items: (MenuItems[number])[] = [
    { label: t("close"), keys: `${mod}+W`, run: () => closeTab(path, group) },
    { label: t("menu.close_others"), disabled: tabs.length < 2, run: () => closeMany(group, (p) => p === path) },
    { label: t("menu.close_right"), disabled: i === tabs.length - 1, run: () => closeMany(group, (_p, j) => j <= i) },
    { label: t("menu.close_saved"), disabled: tabs.every(dirty), run: () => closeMany(group, dirty) },
    null,
    { label: t("menu.copy_path"), run: () => copyText(absPath(path)) },
    { label: t("menu.copy_rel"), run: () => copyText(path) },
    null,
    { label: t("menu.split_right"), keys: `${mod}+\\`, run: () => splitEditor(path, "right") },
    { label: t("menu.split_down"), run: () => splitEditor(path, "down") },
  ];
  if (groups.peek()[group]?.preview === path) items.push(null, { label: t("menu.keep"), run: () => keep(path, group) });
  return items;
}

type Zone = "center" | "right" | "down";

/** DropZone covers a group's body while a tab drags (the editor's frame
 * would take the events): the middle moves the tab into this group, the
 * right or bottom edge splits the editor (one group only). */
function DropZone({ group, canSplit }: { group: number; canSplit: boolean }) {
  const [zone, setZone] = useState<Zone | null>(null);
  const zoneOf = (e: DragEvent): Zone => {
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
    if (!canSplit) return "center";
    if (e.clientX > r.left + (r.width * 2) / 3) return "right";
    if (e.clientY > r.top + (r.height * 2) / 3) return "down";
    return "center";
  };
  return (
    <div
      class={`ed-drop${zone ? " zone-" + zone : ""}`}
      data-group={group}
      onDragOver={(e) => {
        e.preventDefault();
        const z = zoneOf(e);
        if (z !== zone) setZone(z);
      }}
      onDragLeave={() => setZone(null)}
      onDrop={(e) => {
        e.preventDefault();
        const d = dragTab.peek();
        const z = zoneOf(e);
        endDrag();
        setZone(null);
        if (!d) return;
        if (z === "center") {
          if (d.from !== group) moveTab(d.path, d.from, group, tabsIn(group).length);
          return;
        }
        // To the side: the tab moves into a new group; the only tab of
        // its group is shown in both.
        if (tabsIn(d.from).length > 1) {
          splitDir.value = z;
          moveTab(d.path, d.from, groups.peek().length, 0);
        } else splitEditor(d.path, z);
      }}
    >
      <span class="ed-drop-mark" aria-hidden="true" />
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
function EditorBody({ more, file, slot }: { more: boolean; file: string | null; slot: string }) {
  if (more) return <MoreView />;
  if (file && diffOf(file)) return <DiffView key={file} tab={file} slot={slot} />;
  if (file) return <FileView key={file} path={file} slot={slot} />;
  return <Welcome />;
}

/** PanelTabs are the bottom panel's tabs: PROBLEMS (with its count) and
 * TERMINAL, as the laptop's editor has them. */
function PanelTabs() {
  const on = panelTab.value;
  const s = diags();
  const n = s ? s.errors + s.warnings + s.infos + s.hints : 0;
  const tab = (id: PanelTab, label: string, extra?: ComponentChildren) => (
    <button type="button" class={`wb-panel-title${on === id ? " is-on" : ""}`} role="tab" aria-selected={on === id ? "true" : "false"} data-panel={id} onClick={() => (panelTab.value = id)}>
      {label}
      {extra}
    </button>
  );
  return (
    <span class="wb-panel-tabs" role="tablist">
      {tab("problems", t("wb.problems"), n > 0 ? <span class="wb-panel-badge">{n}</span> : null)}
      {tab("terminal", t("wb.terminal"))}
    </span>
  );
}

/** panelMenu: where the panel sits and its size (the head's … and right click). */
function panelMenu(): MenuItems {
  const p = panes.peek();
  return [
    p.panelPos === "right"
      ? { label: t("wb.panel_down"), run: () => setPanes({ panelPos: "bottom" }) }
      : { label: t("wb.panel_right"), run: () => setPanes({ panelPos: "right" }) },
    { label: t(p.panelMax ? "wb.panel_restore" : "wb.panel_max"), run: () => setPanes({ panelMax: !p.panelMax }) },
    null,
    { label: t("wb.panel_hide"), keys: `${modKey()}+J`, run: () => setPanes({ panel: false }) },
  ];
}

function PanelHead({ children }: { children: ComponentChildren }) {
  const p = panes.value;
  const max = p.panelMax;
  const right = p.panelPos === "right";
  return (
    <div class="wb-panel-head" onContextMenu={onMenu(t("wb.panel_more"), panelMenu)}>
      {children}
      <span class="wb-panel-view-actions" id="panel-view-actions" />
      <span class="wb-panel-actions">
        <button
          type="button"
          class="tb tb-icon"
          data-act="panel-more"
          aria-label={t("wb.panel_more")}
          title={t("wb.panel_more")}
          onClick={(e) => {
            const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
            openMenu(r.left, r.bottom, t("wb.panel_more"), panelMenu());
          }}
        >
          <Cod name="ellipsis" />
        </button>
        <button type="button" class="tb tb-icon" data-act="panel-max" aria-pressed={max ? "true" : "false"} aria-label={t(max ? "wb.panel_restore" : "wb.panel_max")} title={t(max ? "wb.panel_restore" : "wb.panel_max")} onClick={() => setPanes({ panelMax: !max })}>
          <Cod name={right ? (max ? "chevron-right" : "chevron-left") : max ? "chevron-down" : "chevron-up"} />
        </button>
        <button type="button" class="tb tb-icon" data-act="panel-hide" aria-label={t("wb.panel_hide")} title={`${t("wb.panel_hide")} (${modKey()}+J)`} onClick={() => setPanes({ panel: false })}>
          <Cod name="close" />
        </button>
      </span>
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
  if (mod && e.shiftKey && !e.altKey && (k === "e" || k === "f" || k === "g")) {
    const id = k === "e" ? "files" : k === "f" ? "search" : "git";
    const x = ACTIVITIES.find((a) => a.id === id);
    if (x) return run(() => (setPanes({ side: true }), go(x.to())));
  }
  if (mod && e.shiftKey && !e.altKey && k === "m") return run(toggleProblems);
  if (mod && !e.shiftKey && !e.altKey && k === ",") return run(() => go({ tab: "more", rest: "settings", query: {} }));
  if (!mod || e.altKey || e.shiftKey) return;
  if (k === "\\") return run(() => splitEditor(activeFile.peek(), "right"));
  if (k === "1") return run(() => focusGroup(0));
  if (k === "2") return run(focusSecond);
  if (k === "p") return run(() => (quick.value = { mode: "files" }));
  if (k === "b") return run(() => setPanes({ side: !panes.peek().side }));
  if (k === "j" || k === "`") return run(() => setPanes({ panel: !panes.peek().panel }));
  if (k === "w") return run(closeActive);
  if (k === "g" && e.ctrlKey && activeFile.peek()) return run(() => (quick.value = { mode: "line" }));
}

/** SideView is the side bar's view: the route's when it has one there,
 * else the last one shown (the editor area keeps its own). */
function SideView({ id }: { id: Activity }) {
  switch (id) {
    case "git":
      return <GitView />;
    case "search":
      return <DeskSearch />;
    case "run":
      return (
        <div class="side-stack">
          <TasksView />
          <RunsView />
        </div>
      );
    case "mcp":
      return <MCPView />;
    default:
      return (
        <div class="side-stack ex-stack">
          <TreeView />
          <ExplorerSections />
        </div>
      );
  }
}

/** sideLabel names the side bar's view for screen readers. */
function sideLabel(id: Activity): StringKey {
  return [...ACTIVITIES, ...ACTIVITIES_BOTTOM].find((x) => x.id === id)?.label ?? "act.explorer";
}

/** LaptopCommand asks before a risky laptop command runs: here first
 * (Cancel first), then a card on the laptop. */
function LaptopCommand({ id, name }: { id: string; name: string }) {
  return <AskLaptop label={name} text={t("cmd.ask_text", { name })} send={() => link.request("palette.ask", { id })} onDone={() => (asking.value = null)} />;
}

/** lastSide is the side bar's view while the route shows another area. */
const lastSide = signal<Activity>("files");

export function Workbench() {
  // The editor tabs (open buffers) and the git marks of the open file.
  useEffect(() => link.subscribe("bufs", "git", "diagnostics"), []);
  // The workbench's keys (lib/commands.ts lists them with their names).
  useEffect(() => {
    addEventListener("keydown", workbenchKeys);
    return () => removeEventListener("keydown", workbenchKeys);
  }, []);
  const r = route.value;
  const p = panes.value;
  const bufs = listOf<BufItem>(store.topic("bufs").value?.data, "bufs").filter((b) => typeof b.path === "string" && b.path);
  useEffect(() => {
    laptopBufs.value = bufs;
  }, [store.topic("bufs").value?.data]);
  // The editor's frame took the focus: its group is the focused one.
  useEffect(() => {
    const onBlur = () =>
      setTimeout(() => {
        const g = document.activeElement?.closest<HTMLElement>(".ed-group")?.dataset.group;
        if (g !== undefined && Number(g) !== focusedGroup.peek()) focusGroup(Number(g));
      }, 0);
    addEventListener("blur", onBlur);
    return () => removeEventListener("blur", onBlur);
  }, []);
  const all = groups.value;
  const fg = focusedGroup.value;
  const dragging = dragTab.value !== null;
  const routeSide = sideActivity(r) ? activityOf(r) : null;
  if (routeSide && routeSide !== lastSide.peek()) lastSide.value = routeSide;
  const side = routeSide ?? lastSide.peek();
  return (
    <div
      class={`wb${p.panel && p.panelMax ? " is-panel-max" : ""}${p.panelPos === "right" ? " is-panel-right" : ""}`}
      style={{ "--side-w": p.sideW + "px", "--panel-h": p.panelH + "px", "--panel-w": p.panelW + "px", "--chat-w": p.chatW + "px" }}
    >
      <ActivityBar />
      {p.side && (
        <aside class="wb-side" data-side={side} aria-label={t(sideLabel(side))}>
          <SideView id={side} />
        </aside>
      )}
      {p.side && <Sash axis="x" size={p.sideW} min={SIDE_MIN} max={SIDE_MAX} dir={1} label={t("wb.resize_side")} def={PANE_DEFAULTS.sideW} onSize={(n) => setPanes({ sideW: n })} />}
      <div class="wb-center">
        <div class="wb-editor">
          {quick.value && <QuickInput key={quick.value.mode} />}
          {asking.value && <LaptopCommand id={asking.value.id} name={asking.value.title} />}
          <ContextMenu />
          <div class={`ed-groups${all.length > 1 ? " is-split split-" + splitDir.value : ""}`}>
            {all.map((g, i) => (
              <section
                key={i}
                class={`ed-group${i === fg && all.length > 1 ? " is-focus" : ""}`}
                data-group={i}
                aria-label={all.length > 1 ? t("wb.group", { n: i + 1 }) : undefined}
                onPointerDownCapture={() => i !== focusedGroup.peek() && focusGroup(i)}
              >
                <EditorTabs group={i} tabs={tabsIn(i, all)} bufs={bufs} />
                <main class="main" id={i === 0 ? "main" : undefined} tabIndex={-1}>
                  <EditorBody more={i === fg && r.tab === "more" && !routeSide} file={g.active} slot={slotOf(i)} />
                </main>
                {dragging && <DropZone group={i} canSplit={all.length < MAX_GROUPS} />}
              </section>
            ))}
          </div>
          <RefsPeek />
        </div>
        {p.panel && (
          <>
            {p.panelPos === "right" ? (
              <Sash axis="x" size={p.panelW} min={PANEL_W_MIN} max={Math.max(PANEL_W_MIN, innerWidth - 600)} dir={-1} label={t("wb.resize_panel")} def={PANE_DEFAULTS.panelW} onSize={(n) => setPanes({ panelW: n })} />
            ) : (
              <Sash axis="y" size={p.panelH} min={PANEL_MIN} max={Math.max(PANEL_MIN, innerHeight - 200)} dir={-1} label={t("wb.resize_panel")} def={PANE_DEFAULTS.panelH} onSize={(n) => setPanes({ panelH: n })} />
            )}
            <section class="wb-panel" aria-label={t(panelTab.value === "problems" ? "wb.problems" : "tab.term")}>
              <PanelHead>
                <PanelTabs />
              </PanelHead>
              {panelTab.value === "problems" ? <ProblemsView /> : <DeskTerm />}
            </section>
          </>
        )}
      </div>
      {p.chat && (
        <>
          <Sash axis="x" size={p.chatW} min={CHAT_MIN} max={CHAT_MAX} dir={-1} label={t("wb.resize_chat")} def={PANE_DEFAULTS.chatW} onSize={(n) => setPanes({ chatW: n })} />
          <aside class={`chatcol${r.tab === "chat" ? " is-focus" : ""}`} aria-label={t("tab.chat")}>
            <ChatView />
          </aside>
        </>
      )}
    </div>
  );
}

export { StatusBar };
