// Requests that need a confirm (web design 3.9): the op answers at once
// with the id of a card; the outcome arrives later as the answered event
// of that card, with its choice. When the page approves its own actions
// (meta.approve_here), the hub also sends this page the card's exact
// text as an ask ev first: asked holds those questions, and HereAsks
// (ui/AskLaptop.tsx) shows them, whichever view sent the request.
import { signal } from "@preact/signals";
import type { AskCard } from "../../../../webproto/proto";
import { link } from "../session";

export interface AskOutcome {
  /** confirmed: the card's yes (here or on the laptop). Else it was cancelled or went away. */
  confirmed: boolean;
  /** by: who answered ("laptop", a device name; "" when it went away). */
  by: string;
}

const waiting = new Map<string, (o: AskOutcome) => void>();

/** asked: this page's own questions (ask evs), oldest first. */
export const asked = signal<AskCard[]>([]);

/** ended drops the card's question and tells its waiter the outcome. */
export function ended(id: string, o: AskOutcome = { confirmed: false, by: "" }): void {
  asked.value = asked.value.filter((a) => a.id !== id);
  const fn = waiting.get(id);
  waiting.delete(id);
  fn?.(o);
}

link.onEvent((kind, data) => {
  if (typeof data !== "object" || data === null) return;
  const d = data as Record<string, unknown>;
  if (kind === "ask" && typeof d.id === "string") asked.value = [...asked.value, data as AskCard];
  else if (kind === "answered" && typeof d.id === "string") ended(d.id, { confirmed: d.choice === "confirm", by: typeof d.by === "string" ? d.by : "" });
});

/** onAnswered calls fn once when the card is answered; it returns the undo. */
export function onAnswered(card: string, fn: (o: AskOutcome) => void): () => void {
  waiting.set(card, fn);
  return () => void waiting.delete(card);
}

/** cancelAsk sends the safe answer of a card. */
export function cancelAsk(card: string): Promise<unknown> {
  return link.request("cards.answer", { id: card, choice: "cancel" });
}
