// Terminal keys win over the browser (WEBP-11). A browser tab keeps
// Ctrl+W, Ctrl+T, Ctrl+N and the like for itself, so they never reach
// the shell. Two ways get them: full screen with the Keyboard Lock API
// (navigator.keyboard.lock; Esc held leaves full screen), or the
// installed app's own window, where the page has them. Without either a
// one-time hint (DeskTerm) says which keys the browser keeps.
import { computed, signal } from "@preact/signals";

/** BROWSER_KEYS are the keys a browser tab keeps, as the hint names them. */
export const BROWSER_KEYS = "Ctrl+W, Ctrl+T, Ctrl+N";

interface KeyboardLock {
  lock(codes?: string[]): Promise<void>;
  unlock(): void;
}

/** InstallAsk is the browser's beforeinstallprompt event. */
interface InstallAsk extends Event {
  prompt(): Promise<unknown>;
}

function keyboard(): KeyboardLock | null {
  const k = (navigator as unknown as { keyboard?: Partial<KeyboardLock> }).keyboard;
  return k && typeof k.lock === "function" && typeof k.unlock === "function" ? (k as KeyboardLock) : null;
}

/** canLock: this browser can hand the keys to the page in full screen. */
export function canLock(): boolean {
  return keyboard() !== null && typeof document.documentElement.requestFullscreen === "function";
}

const appMQ = [matchMedia("(display-mode: standalone)"), matchMedia("(display-mode: window-controls-overlay)")];

/** inApp: the page runs in the installed app's window. */
export const inApp = signal(appMQ.some((m) => m.matches));
for (const m of appMQ) m.addEventListener("change", () => (inApp.value = appMQ.some((x) => x.matches)));

/** locked: full screen with the keys locked to the page. */
export const locked = signal(false);

/** keysWin: every terminal key reaches the shell here. */
export const keysWin = computed(() => inApp.value || locked.value);

/** installAsk is the browser's offer to install the app (null: none). */
export const installAsk = signal<InstallAsk | null>(null);

addEventListener("beforeinstallprompt", (e: Event) => {
  e.preventDefault(); // the page offers it, in the hint
  installAsk.value = e as InstallAsk;
});
addEventListener("appinstalled", () => (installAsk.value = null));

/** install asks the browser to install the app (once per offer). */
export function install(): void {
  const e = installAsk.peek();
  if (!e) return;
  installAsk.value = null;
  e.prompt().catch(() => undefined);
}

/** fullScreenKeys goes full screen and locks every key to the page;
 * false when the browser refused. */
export async function fullScreenKeys(): Promise<boolean> {
  const k = keyboard();
  if (!k) return false;
  try {
    if (!document.fullscreenElement) await document.documentElement.requestFullscreen({ navigationUI: "hide" });
    await k.lock();
  } catch {
    return false;
  }
  locked.value = true;
  return true;
}

document.addEventListener("fullscreenchange", () => {
  if (document.fullscreenElement || !locked.peek()) return;
  keyboard()?.unlock();
  locked.value = false;
});
