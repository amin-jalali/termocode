// Requests that wait for the laptop (web design 3.9): the op answers at
// once with the id of a laptop card; the outcome arrives later as the
// answered event of that card, with the laptop's choice.
import { link } from "../session";

export interface AskOutcome {
  /** confirmed: the laptop said yes. Else it was cancelled (here or there) or went away. */
  confirmed: boolean;
  /** here: this browser cancelled it. */
  by: string;
}

/** onAnswered calls fn once when the card is answered; it returns the undo. */
export function onAnswered(card: string, fn: (o: AskOutcome) => void): () => void {
  const off = link.onEvent((kind, data) => {
    if (kind !== "answered" || typeof data !== "object" || data === null) return;
    const d = data as Record<string, unknown>;
    if (d.id !== card) return;
    off();
    fn({ confirmed: d.choice === "confirm", by: typeof d.by === "string" ? d.by : "" });
  });
  return off;
}

/** cancelAsk sends the safe answer of a laptop card. */
export function cancelAsk(card: string): Promise<unknown> {
  return link.request("cards.answer", { id: card, choice: "cancel" });
}
