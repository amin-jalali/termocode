// The contract between the page and the phone's Git pages (gitphone.js,
// built alone: cmd/webbuild gitphoneChunk). home.js loads them
// (lib/gitphone.ts) and gives them everything with state through this
// host, so gitphone.js imports types and pure helpers only: one Preact,
// one session.
import type { Fragment as PFragment, h as PH } from "preact";
import type * as Hooks from "preact/hooks";
import type { ReadonlySignal } from "@preact/signals";
import type { Link } from "../net/link";
import type { Route, Tab } from "../state/route";
import type { Store } from "../state/store";
import type { ChatRef, AddResult } from "../lib/tochat";
import type { StringKey } from "../strings";
import type { ToastAction } from "../lib/toasts";
import type { View as PView } from "../views/View";
import type { AskLaptop as PAskLaptop } from "../ui/AskLaptop";
import type { ShowSheet as PShowSheet } from "../views/git/ShowSheet";
import type { RemoteSheet as PRemoteSheet } from "../views/git/RemoteSheet";
import type { DiffSheet as PDiffSheet } from "../views/git/DiffSheet";
import type { Sheet as PSheet } from "../ui/Sheet";

export interface GitHost {
  h: typeof PH;
  Fragment: typeof PFragment;
  useState: typeof Hooks.useState;
  useEffect: typeof Hooks.useEffect;
  useRef: typeof Hooks.useRef;
  useMemo: typeof Hooks.useMemo;
  link: Link;
  live: ReadonlySignal<boolean>;
  store: Store;
  route: ReadonlySignal<Route>;
  go(r: Route, replace?: boolean): void;
  goTab(tab: Tab): void;
  t(key: StringKey, vars?: Record<string, string | number>): string;
  tn(key: "git.commit" | "git.commit_confirm", n: number, vars?: Record<string, string | number>): string;
  toast(kind: "ok" | "info" | "warn" | "error", title: string, detail?: string, action?: ToastAction): number;
  /** addRef puts a reference into the chat's composer (lib/tochat.ts). */
  addRef(r: ChatRef, title?: string): AddResult;
  /** draft puts text into the open chat's box and opens the chat. */
  draft(text: string): void;
  /** failed shows a failed git request (views/git/errors.ts). */
  failed(e: unknown): void;
  /** haptic is one short tap (lib/haptic.ts: the Haptics switch holds). */
  haptic(): void;
  /** stage stages (or unstages) one file and offers Undo. */
  stage(path: string, on: boolean): void;
  View: typeof PView;
  Sheet: typeof PSheet;
  AskLaptop: typeof PAskLaptop;
  DiffSheet: typeof PDiffSheet;
  ShowSheet: typeof PShowSheet;
  RemoteSheet: typeof PRemoteSheet;
}
