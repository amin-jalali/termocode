// Loads live editing together (coedit.js, G-COL) with the editor and
// gives it the page's signals, link and edit helpers (one copy of each
// in the page). A failed load can be tried again.
import { signal } from "@preact/signals";
import type * as Coedit from "../coedit/main";
import { OfflineError, ReqError } from "../net/link";
import { link, live } from "../session";
import { own, save } from "./edit";

export type CoeditModule = typeof Coedit;

/** coedit is the loaded module (null: not yet). */
export const coedit = signal<CoeditModule | null>(null);

let loading: Promise<CoeditModule> | null = null;

export function loadCoedit(): Promise<CoeditModule> {
  return (loading ??= import("/assets/coedit.js").then((m: CoeditModule) => {
    m.init({
      signal,
      request: (op, args) => link.request(op, args),
      live: () => live.peek(),
      ownTick: (tick) => void (own.tick = Math.max(own.tick, tick)),
      save,
      isConflict: (e) => e instanceof ReqError && e.code === "conflict",
      isOffline: (e) => e instanceof OfflineError,
    });
    return (coedit.value = m);
  })).catch((e: unknown) => {
    loading = null;
    throw e;
  });
}
