// OS notifications (WEB-37). Only fixed texts from the closed list below
// are ever shown: never words from a terminal, the AI, a path or a
// command, so a locked screen shows nothing private (never-do 14). The
// page asks for the permission only from the user's click, shows a
// notification only while it is in the background, and never uses a
// service worker or push: no page open, no notification.
import { signal } from "@preact/signals";
import { t, type StringKey } from "../strings";
import { prefs, setPref } from "./prefs";

/** NotifyKind is the closed list of things worth a notification. */
export type NotifyKind = "card" | "run_done" | "run_failed";

const TEXT: Record<NotifyKind, StringKey> = {
  card: "notify.card",
  run_done: "notify.run_done",
  run_failed: "notify.run_failed",
};

/** AGAIN_MS: the same kind shows at most once in this time. */
const AGAIN_MS = 10000;

/** unseen is true when something ended while the page was hidden. */
export const unseen = signal(false);

export type NotifyState = "unsupported" | "blocked" | "off" | "on";

function api(): typeof Notification | undefined {
  return typeof window.Notification === "function" ? window.Notification : undefined;
}

/** notifyState says what the More tab offers. */
export function notifyState(): NotifyState {
  const N = api();
  if (!N) return "unsupported";
  if (N.permission === "denied") return "blocked";
  return prefs.value.notify && N.permission === "granted" ? "on" : "off";
}

/** perm mirrors Notification.permission, so the More tab redraws after asking. */
export const perm = signal<string>(api()?.permission ?? "");

/** turnOn runs from a click only: it asks for the permission once. */
export function turnOn(): void {
  const N = api();
  if (!N) return;
  if (N.permission === "granted") {
    setPref("notify", true);
    return;
  }
  N.requestPermission().then(
    (p) => {
      perm.value = p;
      if (p === "granted") setPref("notify", true);
    },
    () => undefined,
  );
}

export function turnOff(): void {
  setPref("notify", false);
}

const last = new Map<NotifyKind, number>();

/** onOpen is where a tap on a notification leads (set by the shell). */
let onOpen: (kind: NotifyKind) => void = () => undefined;

export function setOnOpen(f: (kind: NotifyKind) => void): void {
  onOpen = f;
}

/** notify shows kind's fixed text, if the user turned it on and the page is hidden. */
export function notify(kind: NotifyKind): void {
  if (!document.hidden) return;
  if (kind !== "card") unseen.value = true;
  const N = api();
  if (!N || !prefs.peek().notify || N.permission !== "granted") return;
  const now = Date.now();
  if (now - (last.get(kind) ?? -AGAIN_MS) < AGAIN_MS) return;
  last.set(kind, now);
  try {
    const n = new N(t("connect.title"), { body: t(TEXT[kind]), tag: "termocode-" + kind });
    n.onclick = () => {
      window.focus();
      onOpen(kind);
      n.close();
    };
  } catch {
    // Some browsers allow notifications only from a service worker: none then.
  }
}

document.addEventListener("visibilitychange", () => {
  if (!document.hidden) unseen.value = false;
});
