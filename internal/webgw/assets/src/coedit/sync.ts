// Live sync of the in-page editor (coedit.js) with the laptop's buffer (WEB-39). The
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
import type { Signal } from "@preact/signals";
import type { BufSnap, EditRes } from "../../../../webproto/proto";
import type { EditorHandle } from "../editor/api";
import type { LiveStatus } from "../lib/live";
import { diff, pos } from "../lib/textdiff";
import { host } from "./host";
import { merge3 } from "./merge";

/** SEND_MS: typing sends once it pauses this long. */
export const SEND_MS = 150;
const SYNCED: LiveStatus = { phase: "synced" };

export class LiveSync {
  private known: { text: string; tick: number };
  private latest: BufSnap;
  private timer: ReturnType<typeof setTimeout> | undefined;
  private flying: Promise<void> | null = null;
  private conflict: { base: string } | null = null;
  private gone = false;
  /** behind: the hub moved the last send over the laptop's edits; the
   * laptop's text at known.tick is not known.text until its copy comes. */
  private behind = false;
  /** prior is the laptop's text before the last merge (lost shows it). */
  private prior: string | null = null;
  /** status is this editor's state (shown above it and in its header). */
  readonly status: Signal<LiveStatus> = host.signal<LiveStatus>(SYNCED);

  constructor(readonly buf: number, private readonly ed: EditorHandle, snap: BufSnap) {
    this.known = { text: snap.lines.join("\n"), tick: snap.tick };
    this.latest = snap;
  }

  /** textNow is the editor's text now. */
  textNow(): string {
    return this.ed.text();
  }

  /** dirty: the editor holds text the laptop does not have yet. */
  dirty(): boolean {
    return this.ed.text() !== this.known.text;
  }

  /** settled is the changedtick the page's text is known at: null while
   * text is unsent or on its way, in a conflict, or when the buffer is gone. */
  settled(): number | null {
    if (this.conflict || this.gone || this.flying || this.behind || this.dirty()) return null;
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
    this.behind = false;
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
    const merged = merge3(this.known.text, this.ed.text(), text);
    if (merged !== null) {
      // Both typed, apart: the laptop's change shows, the user's unsent
      // typing stays where it was and goes out on the laptop's tick.
      this.prior = this.known.text;
      this.known = { text, tick: s.tick };
      this.ed.remote(merged);
      if (this.dirty()) this.local();
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
    if (this.behind) return Promise.resolve(); // process sends once the laptop's text is here
    const now = this.ed.text();
    const d = diff(this.known.text, now);
    if (!d) {
      this.status.value = { phase: "synced" };
      return Promise.resolve();
    }
    if (!host.live()) {
      this.status.value = { phase: "waiting" };
      return Promise.resolve();
    }
    const base = this.known;
    const args = { buf: this.buf, tick: base.tick, start: pos(base.text, d.from), end: pos(base.text, d.to), lines: d.insert.split("\n") };
    this.status.value = { phase: "sending" };
    this.flying = host.request("buf.set_text", args).then(
      (res) => {
        const r = res as EditRes;
        host.ownTick(r.tick);
        this.known = { text: now, tick: r.tick };
        this.behind = r.rebased === true;
        this.flying = null;
        this.status.value = { phase: "synced" };
        this.process();
        if (!this.conflict && this.dirty()) this.local();
      },
      (e: unknown) => {
        this.flying = null;
        if (host.isConflict(e)) {
          this.conflict = { base: base.text };
          this.status.value = { phase: "conflict", base: base.text, laptop: this.latest.lines.join("\n") };
        } else if (host.isOffline(e)) {
          this.status.value = { phase: "waiting" };
        } else {
          this.status.value = { phase: "error", note: e instanceof Error ? e.message : "" };
        }
        this.process();
      },
    );
    return this.flying;
  }

  /** lost: the editor could not show the laptop's change over typing it
   * got meanwhile (the two overlap). Nothing more goes out: sending now
   * would undo the laptop's change. The user chooses, as in a conflict. */
  lost(): void {
    if (this.gone) return;
    const base = this.prior ?? this.known.text;
    this.conflict = { base };
    clearTimeout(this.timer);
    this.status.value = { phase: "conflict", base, laptop: this.latest.lines.join("\n") };
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
    return host.save(this.buf);
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
