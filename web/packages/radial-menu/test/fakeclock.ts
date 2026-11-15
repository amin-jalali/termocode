import type { Clock } from "../src/menu";
import type { FakeWindow } from "./fakedom";

// FakeClock runs time by hand: tick(ms) moves time and fires every due
// timeout in time order; with a window it also runs one animation frame
// every 16 ms, as a screen would.
export class FakeClock implements Clock {
  t = 0;
  private seq = 0;
  private timers = new Map<number, { at: number; fn: () => void }>();
  constructor(private win?: FakeWindow) {}

  now(): number {
    return this.t;
  }
  setTimeout(fn: () => void, ms: number): number {
    const id = ++this.seq;
    this.timers.set(id, { at: this.t + ms, fn });
    return id;
  }
  clearTimeout(id: unknown): void {
    this.timers.delete(id as number);
  }
  pending(): number {
    return this.timers.size;
  }
  /** tick moves time by ms in 16 ms frames, firing timers and frames. */
  tick(ms: number): void {
    const end = this.t + ms;
    while (this.t < end) {
      const step = Math.min(16, end - this.t);
      const stop = this.t + step;
      for (;;) {
        let next: [number, { at: number; fn: () => void }] | undefined;
        for (const e of this.timers) if (e[1].at <= stop && (!next || e[1].at < next[1].at)) next = e;
        if (!next) break;
        this.timers.delete(next[0]);
        this.t = Math.max(this.t, next[1].at);
        next[1].fn();
      }
      this.t = stop;
      this.win?.frame();
    }
  }
}
