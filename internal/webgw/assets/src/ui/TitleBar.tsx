// The desktop's title bar (workbench): the termocode mark, the search box
// in the middle that opens Go to file (the command center), and the
// layout buttons: side bar, panel, chat, and the light or dark look.
import { h, type ComponentChildren } from "preact";
import { modKey } from "../lib/platform";
import { panes, quick, setPanes } from "../lib/workbench";
import { status } from "../session";
import { t } from "../strings";
import { Cod } from "./icons";
import { ThemeToggle } from "./Strip";

function Toggle({ on, label, keys, onClick, children }: { on: boolean; label: string; keys?: string; onClick: () => void; children: ComponentChildren }) {
  return (
    <button type="button" class={`tb tb-icon tb-layout${on ? " is-on" : ""}`} aria-pressed={on ? "true" : "false"} aria-label={label} title={keys ? `${label} (${keys})` : label} onClick={onClick}>
      {children}
    </button>
  );
}

export function TitleBar() {
  const p = panes.value;
  const mod = modKey();
  const ws = status.value.workspace;
  return (
    <header class="titlebar">
      <span class="tb-mark" aria-hidden="true" />
      <button type="button" class="cc" title={`${t("cmd.quick_open")} (${mod}+P)`} onClick={() => (quick.value = { mode: "files" })}>
        <Cod name="search" />
        <span class="cc-text trunc" dir="auto">
          {ws || t("cmd.quick_open")}
        </span>
      </button>
      <span class="tb-right">
        <Toggle on={p.side} label={t("wb.toggle_side")} keys={`${mod}+B`} onClick={() => setPanes({ side: !p.side })}>
          <Cod name={p.side ? "layout-sidebar-left" : "layout-sidebar-left-off"} />
        </Toggle>
        <Toggle on={p.panel} label={t("wb.toggle_panel")} keys={`${mod}+J`} onClick={() => setPanes({ panel: !p.panel })}>
          <Cod name={p.panel ? "layout-panel" : "layout-panel-off"} />
        </Toggle>
        <Toggle on={p.chat} label={t("wb.toggle_chat")} onClick={() => setPanes({ chat: !p.chat })}>
          <Cod name={p.chat ? "layout-sidebar-right" : "layout-sidebar-right-off"} />
        </Toggle>
        <ThemeToggle />
      </span>
    </header>
  );
}
