// The page's one store and one link to the hub.
import { computed, effect, signal } from "@preact/signals";
import { onAttention } from "./lib/attention";
import { notify } from "./lib/notify";
import { syncLaptopToasts, type LaptopToast } from "./lib/toasts";
import { token } from "./net/auth";
import { Link } from "./net/link";
import { readCards, splitCards } from "./state/cards";
import { readStatus } from "./state/status";
import { readTheme } from "./state/theme";
import { Store } from "./state/store";
import { registerTopicReducers } from "./state/topics";

export const store = new Store();
registerTopicReducers(store);

function wsURL(): string {
  return (location.protocol === "https:" ? "wss:" : "ws:") + "//" + location.host + "/ws";
}

export const link = new Link({ store, url: wsURL(), token });

/** status is the shell's view of the status topic. */
export const status = computed(() => readStatus(store.topic("status").value?.data));

/** live is false while the link is down: inputs that need the laptop lock (web UX P5). */
export const live = link.live;

/** laptopTheme is the laptop's theme from status.theme (WEB-UX-8), or null. */
export const laptopTheme = computed(() => {
  const d = store.topic("status").value?.data;
  return readTheme(typeof d === "object" && d !== null ? (d as Record<string, unknown>).theme : undefined);
});

/** listOf reads data[key] as a list (an empty one when it is not). */
export function listOf<T>(data: unknown, key: string): T[] {
  const v = typeof data === "object" && data !== null ? (data as Record<string, unknown>)[key] : undefined;
  return Array.isArray(v) ? (v as T[]) : [];
}

// The laptop's toasts show on the page as they come and go (WEB-12).
effect(() => syncLaptopToasts(listOf<LaptopToast>(store.topic("toasts").value?.data, "toasts")));

/** answeredBy holds who answered each card that went away (ev answered):
 * "laptop", a browser's device name, or "" when the card expired. */
export const answeredBy = signal<ReadonlyMap<string, string>>(new Map());
const MAX_ANSWERED = 64;

link.onEvent((kind, data) => {
  if (kind === "attention") {
    onAttention(data);
    const k = typeof data === "object" && data !== null ? (data as Record<string, unknown>).kind : undefined;
    if (k === "run_done" || k === "task_done") notify("run_done");
    if (k === "run_failed") notify("run_failed");
  }
  if (kind === "answered" && typeof data === "object" && data !== null) {
    const d = data as Record<string, unknown>;
    if (typeof d.id !== "string") return;
    const next = new Map(answeredBy.peek());
    next.set(d.id, typeof d.by === "string" ? d.by : "");
    while (next.size > MAX_ANSWERED) next.delete(next.keys().next().value as string);
    answeredBy.value = next;
  }
});

/** cards is the waiting cards, oldest first. */
export const cards = computed(() => readCards(store.topic("cards").value?.data));

// A card that was not there before is worth a notification (the page
// decides only that one came; its text never leaves the page).
let known: ReadonlySet<string> | null = null;
effect(() => {
  const st = store.topic("cards").value;
  if (!st) return;
  const ids = new Set(cards.value.map((c) => c.id));
  const fresh = known !== null && [...ids].some((id) => !known!.has(id));
  known = ids;
  if (fresh) notify("card");
});

/** waiting splits the cards into those the web may answer and those that
 * wait for the laptop; before the cards topic arrives, the status count
 * stands in for the first. */
export const waiting = computed(() => {
  const st = store.topic("cards").value;
  if (st) return { ...splitCards(cards.value), count: cards.value.length };
  const n = status.value.cardsWaiting;
  return { answer: [], laptop: [], count: n };
});
