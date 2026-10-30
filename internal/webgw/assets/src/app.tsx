// The app shell (web UX 4): strip on top, bars under it, the view, the
// attention pill and the tab bar at the bottom on phones; a rail and
// columns on tablets; the workbench on desktops (ui/Workbench.tsx: the
// TUI's layout with editor tabs, panels and a status bar).
import { h } from "preact";
import { effect } from "@preact/signals";
import { bell } from "./lib/attention";
import { layout } from "./lib/layout";
import { route } from "./lib/router";
import type { Tab } from "./state/route";
import { t } from "./strings";
import { AttentionPill } from "./ui/AttentionPill";
import { CardSheet } from "./ui/CardSheet";
import { LinkSheet } from "./ui/LinkSheet";
import { linkAsk } from "./lib/links";
import { FirstRunHint } from "./ui/FirstRunHint";
import { Nav } from "./ui/Nav";
import { OfflineBar } from "./ui/OfflineBar";
import { Strip } from "./ui/Strip";
import { Toasts } from "./ui/Toasts";
import { ViewerBar } from "./ui/ViewerBar";
import { TitleBar } from "./ui/TitleBar";
import { desk, deskFailed, loadDesk } from "./lib/lazy";
import { ChatView, FilesView, GitView, MoreView, TermView } from "./views/views";

effect(() => {
  if (route.value.tab === "term") bell.value = false; // the bell was seen
});

// The desktop's workbench is its own file (workbench.js): a wide screen
// loads it, a phone never does.
effect(() => {
  if (layout.value === "desktop" && !desk.peek()) loadDesk().catch(() => undefined);
});

/** DeskLoading stands in for the workbench while workbench.js loads (or
 * after it failed: Try again). */
function DeskLoading() {
  return (
    <main class="main desk-loading" id="main" tabIndex={-1} aria-busy={!deskFailed.value}>
      {deskFailed.value ? (
        <button type="button" class="btn" onClick={() => loadDesk().catch(() => undefined)}>
          {t("retry")}
        </button>
      ) : (
        <p class="empty">{t("loading")}</p>
      )}
    </main>
  );
}

/** skipToMain moves focus to the view without touching the hash route. */
function skipToMain(e: Event): void {
  e.preventDefault();
  document.getElementById("main")?.focus();
}

function ViewFor({ tab }: { tab: Tab }) {
  switch (tab) {
    case "chat":
      return <ChatView />;
    case "files":
      return <FilesView />;
    case "term":
      return <TermView />;
    case "git":
      return <GitView />;
    case "more":
      return <MoreView />;
  }
}

export function App() {
  const r = route.value;
  const l = layout.value;
  if (l === "desktop") {
    // The desktop is the workbench: the TUI's layout, the status bar at
    // the bottom as on the laptop.
    const d = desk.value;
    const Workbench = d ? d.Workbench : DeskLoading;
    const StatusBar = d ? d.StatusBar : () => null;
    return (
      <div class="shell layout-desktop">
        <a class="skip" href="#main" onClick={skipToMain}>
          {t("a11y.skip")}
        </a>
        <TitleBar />
        <div class="bars">
          <OfflineBar />
          <ViewerBar />
        </div>
        <Workbench />
        <AttentionPill />
        <FirstRunHint />
        <StatusBar />
        {r.card && <CardSheet key={r.card} id={r.card} />}
        {linkAsk.value && <LinkSheet href={linkAsk.value} />}
        <Toasts />
      </div>
    );
  }
  return (
    <div class={`shell layout-${l}`}>
      <a class="skip" href="#main" onClick={skipToMain}>
        {t("a11y.skip")}
      </a>
      <Strip />
      <div class="bars">
        <OfflineBar />
        <ViewerBar />
        <FirstRunHint />
      </div>
      <div class="body">
        {l !== "phone" && <Nav variant="rail" active={r.tab} />}
        <main class="main" id="main" tabIndex={-1}>
          <ViewFor tab={r.tab} />
        </main>
      </div>
      <AttentionPill />
      {l === "phone" && <Nav variant="tabs" active={r.tab} />}
      {r.card && <CardSheet key={r.card} id={r.card} />}
      {linkAsk.value && <LinkSheet href={linkAsk.value} />}
      <Toasts />
    </div>
  );
}
