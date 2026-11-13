// The app shell (web UX 4): strip on top, bars under it, the view, the
// attention pill and the tab bar at the bottom on phones; a rail and
// columns on tablets; the workbench on desktops (ui/Workbench.tsx: the
// TUI's layout with editor tabs, panels and a status bar).
import { h } from "preact";
import { useEffect } from "preact/hooks";
import { effect } from "@preact/signals";
import { bell } from "./lib/attention";
import { layout } from "./lib/layout";
import { go, route } from "./lib/router";
import { prefs } from "./lib/prefs";
import { homeHost } from "./homehost";
import type { Tab } from "./state/route";
import { t } from "./strings";
import { AttentionPill } from "./ui/AttentionPill";
import { CardSheet } from "./ui/CardSheet";
import { FirstRunHint } from "./ui/FirstRunHint";
import { Nav } from "./ui/Nav";
import { OfflineBar } from "./ui/OfflineBar";
import { Strip } from "./ui/Strip";
import { Toasts } from "./ui/Toasts";
import { HereAsks } from "./ui/AskLaptop";
import { ViewerBar } from "./ui/ViewerBar";
import { TitleBar } from "./ui/TitleBar";
import { desk, deskFailed, home, homeFailed, homeHosted, loadDesk, loadHome } from "./lib/lazy";
import { soloBuf } from "./lib/popout";
import { ChatView, FilesView, GitView, MoreView, TermView } from "./views/views";

effect(() => {
  if (route.value.tab === "term") bell.value = false; // the bell was seen
});

// The desktop's workbench is its own file (workbench.js): a wide screen
// loads it, a phone never does.
effect(() => {
  if (layout.value === "desktop" && !desk.peek()) loadDesk().catch(() => undefined);
});

// The phone's Home (MOB-4, #/home) is a phone's only: a wider screen shows
// the chat. A phone whose This phone choice is the Home starts there.
if (layout.peek() === "phone" && prefs.peek().phoneHome === "chat" && (location.hash === "" || location.hash === "#")) {
  go({ tab: "home", rest: "", query: {} }, true);
}
effect(() => {
  if (route.value.tab === "home" && layout.value !== "phone") go({ tab: "chat", rest: "", query: {} }, true);
});

/** HomeView is the phone's Home, from home.js (loaded on first use; Try
 * again after a failed load). */
function HomeView() {
  // home.js may be in already (This phone, All tools) without the host.
  const m = homeHosted.value ? home.value : null;
  useEffect(() => {
    if (!homeHosted.peek()) loadHome(homeHost).catch(() => undefined);
  }, []);
  if (!m) {
    return (
      <main class="main home-loading" id="main" tabIndex={-1} aria-busy={!homeFailed.value}>
        {homeFailed.value ? (
          <button type="button" class="btn" onClick={() => loadHome(homeHost).catch(() => undefined)}>
            {t("retry")}
          </button>
        ) : (
          <p class="empty">{t("loading")}</p>
        )}
      </main>
    );
  }
  return (
    <main class="main main-home" id="main" tabIndex={-1}>
      <m.Home />
    </main>
  );
}

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
    case "home":
      return <ChatView />; // a wider screen: the redirect above moves it to the chat
  }
}

/** Solo is a popped-out terminal tab (WEBP-13): that shell and nothing
 * else but the link's bars, the cards and the toasts. A desktop shows
 * the workbench's terminal, a narrower window the phone's. */
function Solo({ buf }: { buf: number }) {
  const r = route.value;
  const l = layout.value;
  const d = desk.value;
  return (
    <div class={`shell layout-${l} layout-solo`}>
      <div class="bars">
        <OfflineBar />
        <ViewerBar />
      </div>
      <main class="main" id="main" tabIndex={-1}>
        {l !== "desktop" ? <TermView /> : d ? <d.SoloTerm buf={buf} /> : <p class="empty">{t(deskFailed.value ? "retry" : "loading")}</p>}
      </main>
      {r.card && <CardSheet key={r.card} id={r.card} />}
      <HereAsks />
      <Toasts />
    </div>
  );
}

export function App() {
  const r = route.value;
  // A session picked after sign-in (WEB-55) stands on its own as its
  // project page: the workbench shows only this laptop session, so a
  // desktop draws that page in the tablet layout (its tabs lead back).
  const l = layout.value === "desktop" && r.tab === "more" && r.rest.startsWith("projects/") ? "tablet" : layout.value;
  const solo = soloBuf(r);
  if (solo !== null) return <Solo buf={solo} />;
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
          <HereAsks />
        <Toasts />
      </div>
    );
  }
  if (l === "phone" && r.tab === "home") {
    // The Home has its own top bar, chips and box; the old tabs are in
    // its Panels drawer (≡), so there is no strip and no tab bar.
    return (
      <div class="shell layout-phone layout-home">
        <a class="skip" href="#main" onClick={skipToMain}>
          {t("a11y.skip")}
        </a>
        <div class="bars">
          <OfflineBar />
          <ViewerBar />
        </div>
        <HomeView />
        {r.card && <CardSheet key={r.card} id={r.card} />}
          <HereAsks />
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
      <HereAsks />
      <Toasts />
    </div>
  );
}
