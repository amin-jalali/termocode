// Toasts shown by the page: from the laptop's toasts topic (WEB-12) and
// from the page itself. Info, ok and warn hide after 5 s; errors stay
// until closed (web UX 7.3). At most 3 show; older ones fold into "+N".
import { signal } from "@preact/signals";

export type Severity = "info" | "ok" | "warn" | "error";

/** ToastAction is one button on a toast (Undo). */
export interface ToastAction {
  label: string;
  run: () => void;
}

export interface Toast {
  id: number;
  severity: Severity;
  title: string;
  detail?: string;
  /** laptop is the toast's id on the laptop, for toasts from the topic. */
  laptop?: number;
  action?: ToastAction;
}

/** LaptopToast is one toast of the toasts topic (webproto.Toast). */
export interface LaptopToast {
  id: number;
  severity: string;
  title: string;
  detail?: string;
}

export const TOAST_MS = 5000;
export const toasts = signal<Toast[]>([]);
let nextID = 1;
const timers = new Map<number, ReturnType<typeof setTimeout>>();

function arm(id: number, severity: Severity): void {
  clearTimeout(timers.get(id));
  timers.delete(id);
  if (severity !== "error") timers.set(id, setTimeout(() => dismiss(id), TOAST_MS));
}

export function toast(severity: Severity, title: string, detail?: string, action?: ToastAction): number {
  const id = nextID++;
  toasts.value = [...toasts.peek(), { id, severity, title, detail, action }];
  arm(id, severity);
  return id;
}

export function dismiss(id: number): void {
  clearTimeout(timers.get(id));
  timers.delete(id);
  const x = toasts.peek().find((t) => t.id === id);
  if (x && x.laptop !== undefined) hidden.set(x.laptop, shape(x));
  toasts.value = toasts.peek().filter((t) => t.id !== id);
}

// The laptop's toasts that were shown and then hidden here (closed, or
// timed out), by laptop id, with the text they had: they stay hidden until
// the laptop changes them (a progress toast that moves on shows again).
const hidden = new Map<number, string>();

function shape(x: { severity: string; title: string; detail?: string }): string {
  return x.severity + "\n" + x.title + "\n" + (x.detail ?? "");
}

function severity(s: string): Severity {
  return s === "warn" || s === "error" || s === "ok" ? s : "info";
}

/** syncLaptopToasts makes the page show the laptop's current toasts: new
 * ones appear, changed ones update in place (and their timer restarts),
 * ones the laptop dismissed go. The page's own toasts are left alone. */
export function syncLaptopToasts(list: readonly LaptopToast[]): void {
  const want = new Map<number, LaptopToast>();
  for (const x of list) {
    if (typeof x.id !== "number" || typeof x.title !== "string") continue;
    const h = hidden.get(x.id);
    if (h !== undefined && h === shape(x)) continue;
    hidden.delete(x.id);
    want.set(x.id, x);
  }
  for (const id of hidden.keys()) if (!list.some((x) => x.id === id)) hidden.delete(id);
  const out: Toast[] = [];
  for (const cur of toasts.peek()) {
    if (cur.laptop === undefined) {
      out.push(cur);
      continue;
    }
    const x = want.get(cur.laptop);
    if (!x) {
      clearTimeout(timers.get(cur.id));
      timers.delete(cur.id);
      continue;
    }
    want.delete(cur.laptop);
    const next: Toast = { id: cur.id, laptop: x.id, severity: severity(x.severity), title: x.title, detail: x.detail };
    if (shape(next) !== shape(cur)) arm(cur.id, next.severity);
    out.push(next);
  }
  for (const x of want.values()) {
    const id = nextID++;
    const t: Toast = { id, laptop: x.id, severity: severity(x.severity), title: x.title, detail: x.detail };
    out.push(t);
    arm(id, t.severity);
  }
  toasts.value = out;
}
