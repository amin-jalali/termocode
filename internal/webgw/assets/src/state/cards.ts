// The cards topic as the views read it (web design 2.4, web UX 5.3). A
// card the web may answer has a safe choice (Deny, Reject) and is not
// read-only; anything else waits for the laptop. Pure.
import type { Card, CardChoice } from "../../../../webproto/proto";

/** readCards returns the waiting cards of the topic data, oldest first. */
export function readCards(data: unknown): Card[] {
  const v = typeof data === "object" && data !== null ? (data as Record<string, unknown>).cards : undefined;
  if (!Array.isArray(v)) return [];
  return v.filter((c): c is Card => typeof c === "object" && c !== null && typeof (c as Card).id === "string");
}

/** safeChoice is the card's first safe choice: the one the web shows first and focused. */
export function safeChoice(c: Card): CardChoice | undefined {
  return (c.choices ?? []).find((x) => x.safe === true);
}

/** riskyChoice is the one other answer the web may give: the first choice
 * that is not safe and does not need the laptop. */
export function riskyChoice(c: Card): CardChoice | undefined {
  return (c.choices ?? []).find((x) => x.safe !== true && x.needs_laptop !== true);
}

/** answerable: the web may answer the card at all. Without a safe choice
 * there is no safe first button, so the card waits for the laptop. */
export function answerable(c: Card): boolean {
  return c.read_only !== true && c.kind !== "trust" && safeChoice(c) !== undefined;
}

export interface Waiting {
  /** answer: the cards the web may answer. */
  answer: Card[];
  /** laptop: the cards only the laptop can answer. */
  laptop: Card[];
}

export function splitCards(cards: readonly Card[]): Waiting {
  const out: Waiting = { answer: [], laptop: [] };
  for (const c of cards) (answerable(c) ? out.answer : out.laptop).push(c);
  return out;
}
