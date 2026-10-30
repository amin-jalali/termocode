// The visual viewport (WEB-36, web UX 8.1): the part of the page the user
// really sees. On phones the on-screen keyboard covers the bottom of the
// page; some browsers shrink the page for it, others only the visual
// viewport. The shell takes its height from the visual viewport, so the
// input line always sits right above the keyboard, and while the keyboard
// is up the tab bar hides (focus not obscured, WCAG 2.4.11). It also
// gives the focus back after the page was in the background.
import { signal } from "@preact/signals";

/** keyboardUp is true while an on-screen keyboard covers the page. */
export const keyboardUp = signal(false);

/** KB_SHARE: an edit box has focus and the visible height is under this
 * share of the tallest one seen at this width: that is a keyboard. */
const KB_SHARE = 0.75;

const NOT_TEXT = new Set(["button", "checkbox", "radio", "range", "color", "file", "submit", "reset", "image", "hidden"]);

/** editing reports whether the focused element takes typed text. */
export function editing(el: Element | null): boolean {
  if (el instanceof HTMLTextAreaElement) return !el.readOnly && !el.disabled;
  if (el instanceof HTMLInputElement) return !NOT_TEXT.has(el.type) && !el.readOnly && !el.disabled;
  return el instanceof HTMLElement && el.isContentEditable;
}

let tallest = 0;
let width = 0;

function measure(): void {
  const vv = window.visualViewport;
  const h = vv ? vv.height : innerHeight;
  const w = vv ? vv.width : innerWidth;
  if (vv && vv.scale > 1.01) return; // the user zoomed the page: not a keyboard
  if (Math.abs(w - width) > 1) {
    width = w; // turned or resized: start over
    tallest = 0;
  }
  tallest = Math.max(tallest, h);
  const up = editing(document.activeElement) && h < tallest * KB_SHARE;
  const html = document.documentElement;
  html.style.setProperty("--vvh", Math.round(h) + "px");
  html.style.setProperty("--vvtop", Math.round(vv ? vv.offsetTop : 0) + "px");
  if (up) html.dataset.kb = "up";
  else delete html.dataset.kb;
  keyboardUp.value = up;
}

// Back from another app, some tablets leave nothing focused although an
// input had the focus: the input gets it back (the keyboard may reopen,
// as it does in a native app). Focus the user moved elsewhere stays.
let lastEdit: HTMLElement | null = null;

function rememberFocus(): void {
  const a = document.activeElement;
  lastEdit = a instanceof HTMLElement && editing(a) ? a : null;
}

function refocus(): void {
  const el = lastEdit;
  lastEdit = null;
  const a = document.activeElement;
  if (!el || !el.isConnected || (a && a !== document.body && a !== document.documentElement)) return;
  el.focus({ preventScroll: true });
}

let started = false;

/** watchViewport starts following the visual viewport (once). */
export function watchViewport(): void {
  if (started) return;
  started = true;
  const vv = window.visualViewport;
  vv?.addEventListener("resize", measure);
  vv?.addEventListener("scroll", measure);
  addEventListener("resize", measure);
  // Focus moves before the keyboard opens and after it closes.
  document.addEventListener("focusin", measure);
  document.addEventListener("focusout", () => requestAnimationFrame(measure));
  document.addEventListener("visibilitychange", () => (document.hidden ? rememberFocus() : refocus()));
  addEventListener("pagehide", rememberFocus);
  measure();
}
