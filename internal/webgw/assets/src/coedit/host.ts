// What the page gives coedit.js (lib/coedit.ts): its signals, its link
// and its edit helpers, so the chunk imports no value of the page and
// never holds a second copy of a module.
import type { Signal } from "@preact/signals";

export interface CoeditHost {
  signal<T>(v: T): Signal<T>;
  request(op: string, args: unknown): Promise<unknown>;
  /** live: the link is up. */
  live(): boolean;
  /** ownTick notes a tick this page's own edit made (lib/edit.ts own). */
  ownTick(tick: number): void;
  save(buf: number): Promise<boolean>;
  isConflict(e: unknown): boolean;
  isOffline(e: unknown): boolean;
}

export let host: CoeditHost;

/** setHost takes the page's host (once, on the first load). */
export function setHost(h: CoeditHost): void {
  host = h;
}
