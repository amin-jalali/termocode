// Loads the phone remote's queue (queue.js, CMP-46) when the laptop's AI
// is busy or a message waits, and gives it the page's Preact, link and
// strings (one copy of each in the page). A failed load can be tried
// again.
import { h } from "preact";
import { signal } from "@preact/signals";
import type * as QueueModule from "../queue/main";
import { ReqError } from "../net/link";
import { link } from "../session";
import { addStrings, t } from "../strings";

export type Queue = typeof QueueModule;

/** queueMod is the loaded module (null: not yet). */
export const queueMod = signal<Queue | null>(null);

let p: Promise<Queue> | null = null;

export function loadQueue(): Promise<Queue> {
  return (p ??= import("/assets/queue.js").then((m: Queue) => {
    m.init({ h, request: (op, args) => link.request(op, args), t: t as (k: string, v?: Record<string, string | number>) => string, addStrings, isBusy: (e) => e instanceof ReqError && e.code === "busy" });
    return (queueMod.value = m);
  })).catch((e: unknown) => {
    p = null;
    throw e;
  });
}
