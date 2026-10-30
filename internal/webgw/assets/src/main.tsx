// Entry of the termocode web UI. Built by cmd/webbuild (esbuild Go API).
import { pairSecret, secret } from "./boot"; // first: safety hook, secrets out of the URL
import { h, render } from "preact";
import { effect, signal } from "@preact/signals";
import { AuthError, forgetToken, redeem, token } from "./net/auth";
import "./styles/tokens.css";
import "./styles/base.css";
import "./styles/shell.css";
import "./styles/ui.css";
import "./styles/components.css";
import "./styles/chat.css";
import "./styles/git.css";
import "./styles/more.css";
import "./styles/moreviews.css";
import "./styles/files.css";
import "./styles/fonts.css";
import "./styles/hl.css";
import "./styles/media.css";
import "./styles/codicons.css";
import "./styles/workbench.css";
import { App } from "./app";
import { prefs } from "./lib/prefs";
import { setOnOpen, unseen } from "./lib/notify";
import { goTab } from "./lib/router";
import { watchViewport } from "./lib/viewport";
import { laptopTheme, link, waiting } from "./session";
import { dict, has } from "./state/dict";
import { THEME_TOKENS, themeTokens } from "./state/theme";
import { TABS } from "./state/route";
import { openCards } from "./ui/CardSheet";
import { ConnectScreen, type ConnectState } from "./ui/ConnectScreen";
import { PairScreen } from "./ui/PairScreen";
import { SignInScreen } from "./ui/SignInScreen";
import { passkeysHere } from "./net/passkey";
import { lang } from "./strings";

const RTL_LANGS = /^(fa|ar|he|ur|ps|ckb|dv|yi|sd|ug)\b/i;

// Where the page starts: a launch link opens the workspace, a pairing link
// pairs this device, and a page with neither offers the passkey sign-in
// of a paired device (where the browser can) or says to use the link.
const phase = signal<"connect" | "pair" | "signin" | "shell">(
  secret ? "connect" : pairSecret ? "pair" : passkeysHere() ? "signin" : "connect",
);
const connect = signal<ConnectState>(secret ? "ready" : "nolink");

function startShell(): void {
  phase.value = "shell";
  link.subscribe("status", "toasts", "cards");
  link.start();
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
      return <SignInScreen onDone={startShell} />;
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
effect(() => {
  const p = prefs.value.lang;
  const l = p === "auto" ? (/^fa\b/i.test(navigator.language) ? "fa" : "en") : p;
  lang.value = l;
  html.lang = l;
});

effect(() => {
  const d = prefs.value.dir;
  html.dir = d === "auto" ? (lang.value === "fa" || RTL_LANGS.test(navigator.language) ? "rtl" : "ltr") : d;
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
  if (phase.value !== "shell" || !(e.ctrlKey || e.metaKey) || e.altKey || e.shiftKey) return;
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

if (!secret && !pairSecret && token()) startShell();

const root = document.getElementById("app");
if (root) render(<Root />, root);
