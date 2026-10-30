// Attention events from the laptop (WEB-12): a run or a task finished, a
// terminal rang. The newest run or task event shows in the attention pill
// until it is tapped or a minute passes; a bell puts a dot on the Term tab
// until the tab is opened. Cards come from the cards topic, not from here.
import { signal } from "@preact/signals";

export type NoticeKind = "run_done" | "run_failed" | "task_done";

export const NOTICE_MS = 60000;

/** notice is the run or task event the pill shows, or null. */
export const notice = signal<NoticeKind | null>(null);
/** bell is true after a terminal rang, until the Term tab opens. */
export const bell = signal(false);
/** rang counts the bells, so an open terminal view can show each one. */
export const rang = signal(0);

let timer: ReturnType<typeof setTimeout> | undefined;

/** onAttention takes the data of one ev attention. */
export function onAttention(data: unknown): void {
  const kind = typeof data === "object" && data !== null ? (data as Record<string, unknown>).kind : undefined;
  if (kind === "bell") {
    bell.value = true;
    rang.value++;
    return;
  }
  if (kind !== "run_done" && kind !== "run_failed" && kind !== "task_done") return;
  notice.value = kind;
  clearTimeout(timer);
  timer = setTimeout(clearNotice, NOTICE_MS);
}

export function clearNotice(): void {
  clearTimeout(timer);
  notice.value = null;
}
