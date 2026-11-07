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

/** The kit's card states (ui/Card.tsx), repeated here so the pure rules
 * below need no UI import. peek: an inline card with a diff; it offers
 * the safe answer and opens the sheet to see the change. */
export type CardViewState = "open" | "answered" | "offline" | "gone" | "viewonly" | "long" | "peek";
export type CardViewKind = "tool" | "review" | "plan" | "mcp" | "trust" | "workflow";

export interface CardFacts {
  /** present: the card is still in the topic. */
  present: boolean;
  sending: boolean;
  /** gone: the answer came back "gone" (the card left first). */
  gone: boolean;
  live: boolean;
  /** by: who answered it ("" = nobody named: it is gone), undefined = not answered. */
  by?: string;
  /** inline: drawn in the Home stream, not in the sheet. */
  inline?: boolean;
}

/** cardState is the one rule set the sheet and the Home share (MOB-6):
 * which state and which kind the kit's Card shows for c. */
export function cardState(c: Card, f: CardFacts): { state: CardViewState; kind: CardViewKind } {
  const web = answerable(c) && riskyChoice(c) !== undefined;
  let state: CardViewState;
  if (f.gone) state = "gone";
  else if (!f.present && !f.sending) state = f.by === "" ? "gone" : "answered";
  else if (!f.live) state = "offline";
  else if (web && c.diff?.truncated) state = "long";
  else if (web && f.inline && c.diff) state = "peek";
  else state = "open";
  return { state, kind: web ? (c.kind as CardViewKind) : "trust" };
}

/** answerArgs are the cards.answer arguments for an answer to c; drop
 * holds the review changes the user unticked. "keep_none" means every
 * change was dropped (the user should Reject); undefined means there is
 * no such choice. */
export function answerArgs(c: Card, which: "safe" | "risky", drop: ReadonlySet<number> = new Set()): { id: string; choice: string; hunks?: number[] } | "keep_none" | undefined {
  const choice = which === "safe" ? safeChoice(c) : riskyChoice(c);
  if (!choice) return undefined;
  const all = which === "risky" && c.kind === "review" ? (c.diff?.hunks ?? []) : [];
  if (all.length > 0 && drop.size >= all.length) return "keep_none";
  const hunks = drop.size > 0 ? all.map((_, k) => k).filter((k) => !drop.has(k)) : [];
  return hunks.length > 0 ? { id: c.id, choice: choice.id, hunks } : { id: c.id, choice: choice.id };
}

/** homeCards are the cards the Home stream shows inline: the ones the
 * web may answer first, then the ones that wait for the laptop, oldest
 * first in each; at most max. */
export function homeCards(cards: readonly Card[], max = 5): Card[] {
  const w = splitCards(cards);
  return [...w.answer, ...w.laptop].slice(0, max);
}
