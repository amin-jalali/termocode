// Entry of the termocode web UI. Built by cmd/webbuild (esbuild Go API).
import { secret } from "./boot"; // first: safety hook, secret out of the URL
import { h, render } from "preact";
import { effect, signal } from "@preact/signals";
import { AuthError, forgetToken, redeem, token } from "./net/auth";
import "./styles/tokens.css";
import "./styles/base.css";
import "./styles/shell.css";
import "./styles/ui.css";
import { App } from "./app";
import { prefs } from "./lib/prefs";
import { goTab } from "./lib/router";
import { link, status } from "./session";
import { TABS } from "./state/route";
import { ConnectScreen, type ConnectState } from "./ui/ConnectScreen";

const RTL_LANGS = /^(fa|ar|he|ur|ps|ckb|dv|yi|sd|ug)\b/i;

const phase = signal<"connect" | "shell">("connect");
const connect = signal<ConnectState>(secret ? "ready" : "nolink");

function startShell(): void {
  phase.value = "shell";
  link.subscribe("status", "toasts");
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
  return phase.value === "shell" ? <App /> : <ConnectScreen state={connect.value} onOpen={open} />;
}

const html = document.documentElement;

effect(() => {
  const theme = prefs.value.theme;
  if (theme === "system") delete html.dataset.theme;
  else html.dataset.theme = theme;
});

effect(() => {
  const d = prefs.value.dir;
  html.dir = d === "auto" ? (RTL_LANGS.test(navigator.language) ? "rtl" : "ltr") : d;
});

effect(() => {
  html.dataset.link = link.state.value;
  if (link.finalCode.value === 4001 || link.finalCode.value === 4002) forgetToken();
});

effect(() => {
  const n = status.value.cardsWaiting;
  document.title = n > 0 ? `(${n}) termocode` : "termocode";
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

if (!secret && token()) startShell();

const root = document.getElementById("app");
if (root) render(<Root />, root);
