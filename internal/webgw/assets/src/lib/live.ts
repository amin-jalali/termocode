// Live sync of the in-page editor with the laptop's buffer (WEB-39). The
// page knows the laptop's text at a changedtick (known). The user's
// changes go out as one buf.set_text per pause (150 ms), guarded by that
// tick; nvim refuses a stale one. The laptop's changes come in as the
// buf topic's deltas: when the user has nothing unsent they show in the
// editor at once. When both sides changed (live editing together, G-COL)
// and the changes do not touch the same text, the editor shows both: the
// user's unsent typing is moved over the laptop's change (merge3, the
// hub's edge rules) and goes out on the laptop's tick. Only a real
// overlap stops it: the user's text stays, the editor stops sending and
// shows the laptop's change until the user chooses (use the laptop's
// text, or send theirs).
// The sync itself is in coedit.js (coedit/sync.ts), loaded with the
// editor (lib/coedit.ts); the page keeps only its state here.
import { computed, signal } from "@preact/signals";
import type { LiveSync } from "../coedit/sync";
import { focusSlot } from "./workbench";

export type { LiveSync };

export type LivePhase = "synced" | "sending" | "waiting" | "conflict" | "error" | "gone";

export interface LiveStatus {
  phase: LivePhase;
  /** note: the hub's words for an error. */
  note?: string;
  /** base and laptop: the text before and after the laptop's change (conflict). */
  base?: string;
  laptop?: string;
}

/** editing is the path open in the editor (null: the read view). */
export const editing = signal<string | null>(null);
/** syncs are the running syncs by editor slot (one per editor group). */
export const syncs = signal<ReadonlyMap<string, LiveSync>>(new Map());
/** session is the focused editor's sync (Save, Done and the keys act on it). */
export const session = computed<LiveSync | null>(() => syncs.value.get(focusSlot.value) ?? null);
const SYNCED: LiveStatus = { phase: "synced" };
/** liveStatus is the focused editor's state. */
export const liveStatus = computed<LiveStatus>(() => session.value?.status.value ?? SYNCED);

/** setSync puts (or with null, takes) the sync of an editor slot. */
export function setSync(slot: string, s: LiveSync | null): void {
  const next = new Map(syncs.peek());
  if (s) next.set(slot, s);
  else next.delete(slot);
  syncs.value = next;
}
