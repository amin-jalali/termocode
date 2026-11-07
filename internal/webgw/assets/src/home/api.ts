// The contract between the page and the phone's Home (MOB-4). The Home
// lives in its own file, home.js, built alone (cmd/webbuild homeChunk):
// the page loads it on a phone only (lib/lazy.ts loadHome) and gives it
// everything with state through this host (homehost.ts): Preact, the
// hooks, the signals, the store, the link, the router and the page's own
// views (a message, the composer, a block, a sheet). home.js imports
// types only from the page, so there is one Preact and one session
// (lint_test.go TestHomeChunkImportsNoState), and app.js does not grow.
import type { ComponentChildren, ComponentType, Fragment as PFragment, h as PH } from "preact";
import type * as Hooks from "preact/hooks";
import type { ReadonlySignal, computed as PComputed, effect as PEffect, signal as PSignal } from "@preact/signals";
import type { Card, ChatMsg, ChatSnap } from "../../../../webproto/proto";
import type { Layout } from "../lib/layout";
import type { PhoneHome } from "../lib/prefs";
import type { Link } from "../net/link";
import type { Route, Tab } from "../state/route";
import type { StatusView } from "../state/status";
import type { Store } from "../state/store";

export interface HomeHost {
  h: typeof PH;
  Fragment: typeof PFragment;
  useState: typeof Hooks.useState;
  useEffect: typeof Hooks.useEffect;
  useLayoutEffect: typeof Hooks.useLayoutEffect;
  useRef: typeof Hooks.useRef;
  useMemo: typeof Hooks.useMemo;
  signal: typeof PSignal;
  computed: typeof PComputed;
  effect: typeof PEffect;

  link: Link;
  store: Store;
  live: ReadonlySignal<boolean>;
  status: ReadonlySignal<StatusView>;
  cards: ReadonlySignal<Card[]>;
  layout: ReadonlySignal<Layout>;
  route: ReadonlySignal<Route>;
  go(r: Route, replace?: boolean): void;
  goTab(tab: Tab): void;
  /** phoneHome is the This phone choice: start on the tabs or the Home. */
  phoneHome: ReadonlySignal<PhoneHome>;
  setPhoneHome(v: PhoneHome): void;

  readChat(data: unknown): ChatSnap | null;
  setDraft(chat: string, text: string): void;
  /** errText is the page's one calm line for a failed request. */
  errText(e: unknown): string;
  /** isOffline: the request failed because the link is down. */
  isOffline(e: unknown): boolean;
  /** code is a refused request's error code ("" for anything else). */
  code(e: unknown): string;

  Message: ComponentType<{ m: ChatMsg; chat?: string; canFetch?: boolean }>;
  Composer: ComponentType<{ chat: string; busy: boolean; profile?: string; model?: string; onSent?: () => void; placeholder?: string }>;
  Sheet: ComponentType<{ onLater: () => void; children: ComponentChildren; label?: string }>;
  /** Block draws a block the laptop built (MOB-3's renderer); null while
   * the page has none, and the Home then draws the kinds it knows. */
  Block: ComponentType<{ block: unknown }> | null;
}

export interface HomeModule {
  /** init gives the Home its host, once, before the first draw. */
  init(host: HomeHost): void;
  /** Home is the phone's chat-first Home view. */
  Home: ComponentType;
}
