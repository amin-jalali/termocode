// The app shell (web UX 4): strip on top, bars under it, the view, the
// attention pill and the tab bar at the bottom on phones; a rail and
// columns on tablets and desktops, with the chat always on the right on
// a desktop (like the TUI's editor + AI chat).
import { h } from "preact";
import { effect, signal } from "@preact/signals";
import { bell } from "./lib/attention";
import { layout } from "./lib/layout";
import { route } from "./lib/router";
import type { Tab } from "./state/route";
import { t } from "./strings";
import { AttentionPill } from "./ui/AttentionPill";
import { FirstRunHint } from "./ui/FirstRunHint";
import { Nav } from "./ui/Nav";
import { OfflineBar } from "./ui/OfflineBar";
import { Strip } from "./ui/Strip";
import { Toasts } from "./ui/Toasts";
import { ChatView, FilesView, GitView, MoreView, TermView } from "./views/views";

/** lastPrimary is the last tab other than Chat: the desktop's middle column. */
export const lastPrimary = signal<Exclude<Tab, "chat">>("files");

effect(() => {
  const tab = route.value.tab;
  if (tab !== "chat") lastPrimary.value = tab;
  if (tab === "term") bell.value = false; // the bell was seen
});

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
  const primary: Tab = l === "desktop" && r.tab === "chat" ? lastPrimary.value : r.tab;
  return (
    <div class={`shell layout-${l}`}>
      <a class="skip" href="#main" onClick={skipToMain}>
        {t("a11y.skip")}
      </a>
      <Strip />
      <div class="bars">
        <OfflineBar />
        <FirstRunHint />
      </div>
      <div class="body">
        {l !== "phone" && <Nav variant="rail" active={r.tab} />}
        <main class="main" id="main" tabIndex={-1}>
          <ViewFor tab={primary} />
        </main>
        {l === "desktop" && (
          <aside class={`chatcol${r.tab === "chat" ? " is-focus" : ""}`} aria-label={t("tab.chat")}>
            <ChatView />
          </aside>
        )}
      </div>
      <AttentionPill />
      {l === "phone" && <Nav variant="tabs" active={r.tab} />}
      <Toasts />
    </div>
  );
}
