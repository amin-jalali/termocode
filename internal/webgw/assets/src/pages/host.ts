// The contract between the page and pages.js (the phone's tool pages).
// pages.js is built alone (cmd/webbuild pagesChunk): it imports types
// and pure helpers only and gets Preact, the session and the shared
// views through this host (lib/pages.ts), so there is one Preact and one
// session, and the page's shared chunks are not split again.
import type { ComponentChildren, JSX as PJSX, h as PH, Fragment as PFragment } from "preact";
import type { signal as PSignal, Signal } from "@preact/signals";
import type { useEffect as PUseEffect, useMemo as PUseMemo, useRef as PUseRef, useState as PUseState } from "preact/hooks";
import type { Attach } from "../../../../webproto/proto";
import type { Link } from "../net/link";
import type { Store } from "../state/store";
import type { Route } from "../state/route";
import type { Severity } from "../lib/toasts";
import type { Sheet as PSheet } from "../ui/Sheet";
import type { View as PView } from "../views/View";
import type { TopicBody as PTopicBody } from "../views/Placeholder";

export interface PagesHost {
  h: typeof PH;
  Fragment: typeof PFragment;
  useState: typeof PUseState;
  useEffect: typeof PUseEffect;
  useMemo: typeof PUseMemo;
  useRef: typeof PUseRef;
  signal: typeof PSignal;
  link: Link;
  store: Store;
  go: (r: Route) => void;
  toast: (s: Severity, title: string) => void;
  errText: (e: unknown) => string;
  Sheet: typeof PSheet;
  View: typeof PView;
  TopicBody: typeof PTopicBody;
  addAttach: (chat: string, a: Attach) => boolean;
  pendingDraft: Signal<{ text: string; notChat: string } | null>;
  /** chatID is the laptop's open chat ("" while unknown). */
  chatID: () => string;
  /** canAct: this browser may change things (not a viewer). */
  canAct: () => boolean;
  /** ask sends one op; a refusal is a toast with the hub's words. */
  ask: (op: string, args?: unknown, then?: (data: unknown) => void) => void;
  /** openAt opens a workspace file at a 1-based line. */
  openAt: (path: string, line: number) => void;
  t: (key: string, vars?: Record<string, string | number>) => string;
  addStrings: (d: Readonly<Record<string, string>>) => void;
}

export let H!: PagesHost;

/** setHost keeps the page's host (init, once). */
export function setHost(host: PagesHost): void {
  H = host;
}

/** h is the page's createElement: JSX here makes the page's elements. */
export function h(type: unknown, props: unknown, ...children: unknown[]): PJSX.Element {
  return (H.h as unknown as (t: unknown, p: unknown, ...c: unknown[]) => PJSX.Element)(type, props, ...children);
}

/** Fragment is <>…</>: the page's own. */
export function Fragment(p: { children?: ComponentChildren }): PJSX.Element {
  return H.h(H.Fragment as never, null, p.children as never) as PJSX.Element;
}

// The JSX types of h: Preact's own (tsconfig.json: jsxFactory h).
export namespace h {
  export namespace JSX {
    export type Element = PJSX.Element;
    export type ElementClass = PJSX.ElementClass;
    export type ElementAttributesProperty = PJSX.ElementAttributesProperty;
    export type ElementChildrenAttribute = PJSX.ElementChildrenAttribute;
    export type IntrinsicAttributes = PJSX.IntrinsicAttributes;
    export type IntrinsicElements = PJSX.IntrinsicElements;
    export type LibraryManagedAttributes<C, P> = PJSX.LibraryManagedAttributes<C, P>;
  }
}
