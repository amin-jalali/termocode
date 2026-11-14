// Entry of the termocode web UI. Built by cmd/webbuild (esbuild Go API).
import { pairSecret, secret } from "./boot"; // first: safety hook, secrets out of the URL
import { h, render } from "preact";
import { useEffect, useState } from "preact/hooks";
import { effect, signal } from "@preact/signals";
import { AuthError, forgetToken, redeem, token } from "./net/auth";
import "./styles/tokens.css";
import "./styles/base.css";
import "./styles/shell.css";
import "./styles/ui.css";
import "./styles/components.css";
import "./styles/chat.css";
import "./styles/home.css";
import "./styles/git.css";
import "./styles/more.css";
import "./styles/moreviews.css";
import "./styles/review.css"; // the phone review (phone.js may not import a style sheet)
import "./styles/project.css";
import "./styles/files.css";
import "./styles/board.css";
import "./styles/fonts.css";
import "./styles/hl.css";
import "./styles/media.css";
import "./styles/saved.css";
import "./styles/home-catalog.css"; // MOB-13: the Home's catalog and pinned tracker blocks (home.js has no style sheet)
import "./styles/research.css"; // moved from a chunk file (chunks may not import style sheets)
import "./styles/race-models.css"; // moved from a chunk file (chunks may not import style sheets)
import "./styles/viewblock.css"; // moved from a chunk file (chunks may not import style sheets)
import "./styles/race.css"; // moved from a chunk file (chunks may not import style sheets)
import "./styles/phone-projects.css"; // moved from a chunk file (chunks may not import style sheets)
import "./styles/mcpviews.css"; // moved from a chunk file (chunks may not import style sheets)
import { App } from "./app";
import { loadPersian, loadPicker, type PickerModule } from "./lib/lazy";
import { prefs } from "./lib/prefs";
import { setOnOpen, unseen } from "./lib/notify";
import { watchTap } from "./lib/pushtap";
import { soloBuf } from "./lib/popout";
import { homeHost } from "./homehost";
import { goTab, route } from "./lib/router";
import { watchViewport } from "./lib/viewport";
import { laptopTheme, link, store, waiting } from "./session";
import { dict, has } from "./state/dict";
import { THEME_TOKENS, themeTokens } from "./state/theme";
import { TABS } from "./state/route";
import { openCards } from "./ui/CardSheet";
import { ConnectScreen, type ConnectState } from "./ui/ConnectScreen";
import { PairScreen } from "./ui/PairScreen";
import { SignInScreen, bookmarkRemoved, relayCode, signInWithBookmark } from "./ui/SignInScreen";
import { passkeysHere } from "./net/passkey";
import { isRemembered } from "./net/devicekey";
import { lang, addStrings, persian, t, type Lang } from "./strings";

const RTL_LANGS = /^(fa|ar|he|ur|ps|ckb|dv|yi|sd|ug)\b/i;

// Where the page starts: a launch link opens the workspace, a pairing link
// pairs this device, and a page with neither offers the passkey sign-in
// of a paired device (where the browser can, or with the bookmark key of
// a remembered browser, WEBP-12) or says to use the link.
const phase = signal<"connect" | "pair" | "signin" | "pick" | "shell">(
  secret ? "connect" : pairSecret ? "pair" : relayCode || passkeysHere() || isRemembered() ? "signin" : "connect",
);
const connect = signal<ConnectState>(secret ? "ready" : "nolink");

function startShell(): void {
  phase.value = "shell";
  link.subscribe("status", "toasts", "cards");
  link.start();
}

// After a passkey or bookmark sign-in (WEB-55) the link starts and the
// page lists the owner's running sessions; a pick, or a single session,
// opens the workspace. A launch link and a pairing go straight in.
function startPick(): void {
  startShell(); // the same link and topics, then the picker step first
  phase.value = "pick";
}

const pickSession = () => (phase.value = "shell");

/** PickerStep shows the session picker, drawn by picker.js (lib/lazy.ts);
 * when that cannot load, the page opens this session as it always did. */
function PickerStep() {
  const [m, setM] = useState<PickerModule | null>(null);
  useEffect(() => {
    loadPicker().then(setM, () => pickSession());
  }, []);
  return m && <m.Step host={homeHost()} t={t} addStrings={addStrings} onPick={pickSession} />;
}

function open(): void {
  if (!secret || connect.value === "opening") return;
  connect.value = "opening";
  redeem(secret).then(startShell, (e: unknown) => {
    connect.value = e instanceof AuthError && e.why === "busy" ? "busy" : "expired";
  });
}

function Root() {
  switch (phase.value) {
    case "shell":
      return <App />;
    case "pair":
      return <PairScreen secret={pairSecret ?? ""} onDone={startShell} />;
    case "signin":
      return <SignInScreen onDone={startPick} />;
    case "pick":
      return <PickerStep />;
    default:
      return <ConnectScreen state={connect.value} onOpen={open} />;
  }
}

const html = document.documentElement;

effect(() => {
  const theme = prefs.value.theme;
  if (theme === "system") delete html.dataset.theme;
  else html.dataset.theme = theme;
});

// Reduce motion on this device (MOBUX-22): the CSS stops animations under
// :root[data-motion="reduce"], as it does for the system setting.
effect(() => {
  if (prefs.value.motion === "reduce") html.dataset.motion = "reduce";
  else delete html.dataset.motion;
});

const lightMQ = matchMedia("(prefers-color-scheme: light)");
const systemLight = signal(lightMQ.matches);
lightMQ.addEventListener("change", () => (systemLight.value = lightMQ.matches));

// The laptop's theme, in the scheme this page shows (WEB-UX-8): its
// tokens go on :root through the CSSOM (no style attribute in the page,
// so the CSP stays strict) and override the built-in ones; without a
// theme from the hub the built-in tokens stay.
effect(() => {
  const th = laptopTheme.value;
  const p = prefs.value.theme;
  const scheme = p === "system" ? (systemLight.value ? "light" : "dark") : p;
  const tokens = th ? themeTokens(th, scheme) : dict<string>();
  for (const k of THEME_TOKENS) {
    if (has(tokens, k)) html.style.setProperty(k, tokens[k]);
    else html.style.removeProperty(k);
  }
  if (th && th.name) html.dataset.laptopTheme = th.name;
  else delete html.dataset.laptopTheme;
});

// The UI language: Persian when chosen, or on "auto" when the browser
// asks for Persian first; English otherwise (WEB-UX-5).
function wantedLang(): Lang {
  const p = prefs.value.lang;
  return p === "auto" ? (/^fa\b/i.test(navigator.language) ? "fa" : "en") : p;
}

// Persian switches on once its strings (strings-fa.js) are here, so the
// page never shows half Persian; English stays meanwhile, and a failed
// load stays English until the language is picked again.
effect(() => {
  const l = wantedLang();
  if (l === "fa" && !persian.value) {
    loadPersian().catch(() => {});
    return; // runs again when persian is set
  }
  lang.value = l;
  html.lang = l;
});

effect(() => {
  const d = prefs.value.dir;
  html.dir = d === "auto" ? (lang.value === "fa" || RTL_LANGS.test(navigator.language) ? "rtl" : "ltr") : d;
});

// A sign-in that cannot go on is marked for the phone app, which then
// shows its own Pair again screen: the page has no way back to it.
effect(() => {
  const p = phase.value;
  const mark = p === "signin" && bookmarkRemoved() ? "removed" : p === "connect" && connect.value === "nolink" ? "none" : "";
  if (mark) html.dataset.signin = mark;
  else delete html.dataset.signin;
});

effect(() => {
  html.dataset.link = link.state.value;
  if (link.finalCode.value === 4001 || link.finalCode.value === 4002) forgetToken();
});

// The title badge: the waiting cards, or a dot for a run that ended
// while the page was in the background (WEB-37).
effect(() => {
  const n = waiting.value.count;
  document.title = n > 0 ? `(${n}) termocode` : unseen.value ? "(•) termocode" : "termocode";
});

document.addEventListener("visibilitychange", () => link.setVisible(!document.hidden));

addEventListener("keydown", (e: KeyboardEvent) => {
  if (phase.value !== "shell" || !(e.ctrlKey || e.metaKey) || e.altKey || e.shiftKey || soloBuf(route.value) !== null) return;
  const i = "12345".indexOf(e.key);
  if (i >= 0) {
    e.preventDefault();
    goTab(TABS[i]);
  }
});

// Installable only on an https (tailnet) site, whose origin stays the
// same between starts (WEB-35); the gateway serves the manifest only there.
if (location.protocol === "https:") {
  const m = document.createElement("link");
  m.rel = "manifest";
  m.href = "/manifest.webmanifest";
  document.head.appendChild(m);
}

watchViewport();
setOnOpen((kind) => (kind === "card" ? openCards() : goTab("more")));
// A tap on a push (lib/push.ts) opens the cards as soon as one is here.
const wantCards = signal(false);
watchTap(() => (wantCards.value = true));
effect(() => wantCards.value && waiting.value.count > 0 && ((wantCards.value = false), openCards()));

// A popped-out terminal tab (WEBP-13) came with this session's token;
// it keeps no handle on the tab that opened it.
if (soloBuf(route.peek()) !== null && window.opener) window.opener = null;

if (!secret && !pairSecret && token()) startShell();
else if (!secret && !pairSecret && !relayCode && isRemembered()) {
  // A bookmark: sign in with the kept key; when it is refused here and
  // no passkey can be used, the page says to use the laptop's link.
  void signInWithBookmark(startPick).then((going) => {
    if (!going && !passkeysHere() && !bookmarkRemoved()) phase.value = "connect";
  });
}

// A Persian page draws first in Persian: it waits for strings-fa.js (or
// its failure) before the first render.
function mount() {
  const root = document.getElementById("app");
  if (root) render(<Root />, root);
}
if (wantedLang() === "fa" && !persian.value) loadPersian().then(mount, mount);
else mount();
