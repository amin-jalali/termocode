// Toasts shown by the page: from the laptop's toasts topic (WEB-12) and
// from the page itself. Info, ok and warn hide after 5 s; errors stay
// until closed (web UX 7.3). At most 3 show; older ones fold into "+N".
import { signal } from "@preact/signals";

export type Severity = "info" | "ok" | "warn" | "error";

export interface Toast {
  id: number;
  severity: Severity;
  title: string;
  detail?: string;
}

export const TOAST_MS = 5000;
export const toasts = signal<Toast[]>([]);
let nextID = 1;

export function toast(severity: Severity, title: string, detail?: string): number {
  const id = nextID++;
  toasts.value = [...toasts.value, { id, severity, title, detail }];
  if (severity !== "error") setTimeout(() => dismiss(id), TOAST_MS);
  return id;
}

export function dismiss(id: number): void {
  toasts.value = toasts.value.filter((t) => t.id !== id);
}
