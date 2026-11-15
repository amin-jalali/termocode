import type { Clock } from "../src/engine";

// FakeClock runs timers by hand: tick(ms) moves time and fires every
// due timeout and interval in time order.
export class FakeClock implements Clock {
  now = 0;
  private seq = 0;
  private timers = new Map<number, { at: number; every: number; fn: () => void }>();

  setTimeout(fn: () => void, ms: number): number {
    const id = ++this.seq;
    this.timers.set(id, { at: this.now + ms, every: 0, fn });
    return id;
  }
  clearTimeout(id: unknown): void {
    this.timers.delete(id as number);
  }
  setInterval(fn: () => void, ms: number): number {
    const id = ++this.seq;
    this.timers.set(id, { at: this.now + ms, every: ms, fn });
    return id;
  }
  clearInterval(id: unknown): void {
    this.timers.delete(id as number);
  }
  pending(): number {
    return this.timers.size;
  }
  tick(ms: number): void {
    const end = this.now + ms;
    for (;;) {
      let next: [number, { at: number; every: number; fn: () => void }] | undefined;
      for (const e of this.timers) if (e[1].at <= end && (!next || e[1].at < next[1].at)) next = e;
      if (!next) break;
      const [id, t] = next;
      this.now = t.at;
      if (t.every) t.at += t.every;
      else this.timers.delete(id);
      t.fn();
    }
    this.now = end;
  }
}
