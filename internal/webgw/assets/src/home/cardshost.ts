// What the Home's inline cards (MOB-6) get from the page: home.js is
// built alone and must not bring a second Preact, store or link, so the
// page hands over its own (lib/homecards.ts builds this; MOB-4's
// HomeHost carries it as `cards`). Types only here.
import type { h as H } from "preact";
import type { useRef as UseRef, useState as UseState } from "preact/hooks";
import type { Card as CardData } from "../../../../webproto/proto";
import type { StringKey } from "../strings";
import type { Card as KitCard } from "../ui/Card";
import type { Severity } from "../lib/toasts";

export interface ReadSignal<T> {
  readonly value: T;
}

export interface HomeCardsHost {
  h: typeof H;
  useState: typeof UseState;
  useRef: typeof UseRef;
  /** Card is the kit's card (ui/Card.tsx): the same buttons, arming and keys as the sheet. */
  Card: typeof KitCard;
  /** cards is the cards topic, oldest first. */
  cards: ReadSignal<readonly CardData[]>;
  live: ReadSignal<boolean>;
  answeredBy: ReadSignal<ReadonlyMap<string, string>>;
  t: (key: StringKey, vars?: Record<string, string | number>) => string;
  /** cardsWaiting is "{n} approval(s) waiting" (tn("pill.cards", n)). */
  cardsWaiting: (n: number) => string;
  toast: (severity: Severity, title: string) => void;
  /** request sends one op over the page's link. */
  request: (op: "cards.answer", args: { id: string; choice: string; hunks?: number[] }) => Promise<unknown>;
  /** failure sorts a failed request: "gone" (answered first), "offline" (the bar says it), or the short line to show. */
  failure: (e: unknown) => "gone" | "offline" | string;
  /** openCard opens the sheet over the Home (#/home?card=<id>), with a step back. */
  openCard: (id: string) => void;
  haptic: (kind: "light" | "medium") => void;
}
