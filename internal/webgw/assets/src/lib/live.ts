// Live sync of the in-page editor with the laptop's buffer (WEB-39). The
// page knows the laptop's text at a changedtick (known). The user's
// changes go out as one buf.set_text per pause (150 ms), guarded by that
// tick; nvim refuses a stale one. The laptop's changes come in as the
// buf topic's deltas: when the user has nothing unsent they show in the
// editor at once; when both sides changed, nothing is merged: the user's
// text stays, the editor stops sending and shows the laptop's change
// until the user chooses (use the laptop's text, or send theirs).
import { computed, signal, type Signal } from "@preact/signals";
import type { BufSnap, EditRes } from "../../../../webproto/proto";
import type { EditorHandle } from "../editor/api";
import { OfflineError, ReqError } from "../net/link";
import { link, live } from "../session";
import { own, save as saveBuf } from "./edit";
import { diff, pos } from "./textdiff";
import { focusSlot } from "./workbench";

export type LivePhase = "synced" | "sending" | "waiting" | "conflict" | "error" | "gone";

export interface LiveStatus {
  phase: LivePhase;
  /** note: the hub's words for an error. */
  note?: string;
  /** base and laptop: the text before and after the laptop's change (conflict). */
  base?: string;
  laptop?: string;
}

/** SEND_MS: typing sends once it pauses this long. */
export const SEND_MS = 150;

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

export class LiveSync {
  private known: { text: string; tick: number };
  private latest: BufSnap;
  private timer: ReturnType<typeof setTimeout> | undefined;
  private flying: Promise<void> | null = null;
  private conflict: { base: string } | null = null;
  private gone = false;
  /** status is this editor's state (shown above it and in its header). */
  readonly status: Signal<LiveStatus> = signal<LiveStatus>(SYNCED);

  constructor(readonly buf: number, private readonly ed: EditorHandle, snap: BufSnap) {
    this.known = { text: snap.lines.join("\n"), tick: snap.tick };
    this.latest = snap;
  }

  /** dirty: the editor holds text the laptop does not have yet. */
  dirty(): boolean {
    return this.ed.text() !== this.known.text;
  }

  /** settled is the changedtick the page's text is known at: null while
   * text is unsent or on its way, in a conflict, or when the buffer is gone. */
  settled(): number | null {
    if (this.conflict || this.gone || this.flying || this.dirty()) return null;
    return this.known.tick;
  }

  /** local: the user changed the text. */
  local(): void {
    if (this.conflict || this.gone) return;
    clearTimeout(this.timer);
    this.timer = setTimeout(() => void this.flush(), SEND_MS);
  }

  /** store takes the page's newest copy of the buffer. */
  store(s: BufSnap, gone: boolean): void {
    this.latest = s;
    if (gone && !this.gone) {
      this.gone = true;
      clearTimeout(this.timer);
      this.status.value = { phase: "gone" };
      return;
    }
    this.process();
  }

  private process(): void {
    if (this.flying || this.gone) return;
    const s = this.latest;
    if (s.tick < this.known.tick) return; // this page's own change is on its way
    const text = s.lines.join("\n");
    if (text === this.known.text) {
      this.known.tick = s.tick; // a save, or this page's own change
      return;
    }
    if (this.conflict) {
      this.status.value = { phase: "conflict", base: this.conflict.base, laptop: text };
      return;
    }
    if (!this.dirty()) {
      this.known = { text, tick: s.tick };
      this.ed.remote(text);
      return;
    }
    this.conflict = { base: this.known.text };
    clearTimeout(this.timer);
    this.status.value = { phase: "conflict", base: this.known.text, laptop: text };
  }

  /** flush sends what is unsent now; it resolves when the hub answered. */
  flush(): Promise<void> {
    clearTimeout(this.timer);
    if (this.flying) return this.flying;
    if (this.conflict || this.gone) return Promise.resolve();
    const now = this.ed.text();
    const d = diff(this.known.text, now);
    if (!d) {
      this.status.value = { phase: "synced" };
      return Promise.resolve();
    }
    if (!live.peek()) {
      this.status.value = { phase: "waiting" };
      return Promise.resolve();
    }
    const base = this.known;
    const args = { buf: this.buf, tick: base.tick, start: pos(base.text, d.from), end: pos(base.text, d.to), lines: d.insert.split("\n") };
    this.status.value = { phase: "sending" };
    this.flying = link.request("buf.set_text", args).then(
      (res) => {
        const r = res as EditRes;
        own.tick = Math.max(own.tick, r.tick);
        this.known = { text: now, tick: r.tick };
        this.flying = null;
        this.status.value = { phase: "synced" };
        this.process();
        if (!this.conflict && this.dirty()) this.local();
      },
      (e: unknown) => {
        this.flying = null;
        if (e instanceof ReqError && e.code === "conflict") {
          this.conflict = { base: base.text };
          this.status.value = { phase: "conflict", base: base.text, laptop: this.latest.lines.join("\n") };
        } else if (e instanceof OfflineError) {
          this.status.value = { phase: "waiting" };
        } else {
          this.status.value = { phase: "error", note: e instanceof Error ? e.message : "" };
        }
        this.process();
      },
    );
    return this.flying;
  }

  /** takeLaptop shows the laptop's text; the user's text is one undo away. */
  takeLaptop(): void {
    const s = this.latest;
    const text = s.lines.join("\n");
    this.conflict = null;
    this.known = { text, tick: s.tick };
    this.ed.replace(text);
    this.status.value = { phase: "synced" };
  }

  /** sendMine sends the user's text over the laptop's newest. */
  sendMine(): Promise<void> {
    const s = this.latest;
    this.conflict = null;
    this.known = { text: s.lines.join("\n"), tick: s.tick };
    return this.flush();
  }

  /** save sends what is unsent, then writes the file through nvim. */
  async save(): Promise<boolean> {
    await this.flush();
    if (this.conflict || this.gone || this.dirty()) return false;
    return saveBuf(this.buf);
  }

  /** undoEd and redoEd step the editor's own history (it sends the result). */
  undoEd(): void {
    if (!this.ed.undo()) return;
  }

  redoEd(): void {
    if (!this.ed.redo()) return;
  }

  dispose(): void {
    clearTimeout(this.timer);
  }
}
