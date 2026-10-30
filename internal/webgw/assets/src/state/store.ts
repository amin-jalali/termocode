// The topic store: the browser's copy of each subscribed topic, kept by
// the same rules as the hub (web design 1.5). A snap sets a topic; a delta
// must carry seq = last + 1 and is folded in by the topic's reducer; any
// gap, unknown topic or reducer failure marks the topic stale and asks for
// a resync, so the page never shows a state the hub never had.
//
// Pure logic: no DOM, timers or network (see lint_test.go), so the same
// code can be replayed against the Go goldens.
import { batch, signal, type ReadonlySignal, type Signal } from "@preact/signals";
import type { Delta, Resync, Snap } from "../../../../webproto/proto";
import { isTopic, topicName } from "../proto/guard";
import { dict, type Dict } from "./dict";

/** Reducer folds one delta into a topic's data and returns the new data.
 * It must not change its inputs; a throw means the delta is bad. */
export type Reducer = (data: unknown, delta: unknown) => unknown;

export interface TopicState {
  readonly seq: number;
  readonly data: unknown;
  /** stale: the data may be old (after a reconnect, a gap or a resync);
   * the view shows it under a short skeleton until fresh data arrives. */
  readonly stale: boolean;
  /** broken: a delta was lost; only a fresh snap can repair the topic. */
  readonly broken: boolean;
}

export type ApplyResult = { ok: true } | { ok: false; resync: string; why: string };

/** replace is the default reducer: the delta is the whole new value
 * (the status topic, web design 2.1). */
export const replace: Reducer = (_data, delta) => delta;

export class Store {
  private readonly topics = new Map<string, Signal<TopicState | undefined>>();
  private readonly reducers = new Map<string, Reducer>();

  /** reducer sets how deltas of a topic name ("chat", "buf") are folded in. */
  reducer(name: string, fn: Reducer): void {
    this.reducers.set(name, fn);
  }

  /** topic returns the live state of one topic (undefined until its first snap). */
  topic(name: string): ReadonlySignal<TopicState | undefined> {
    let s = this.topics.get(name);
    if (!s) {
      s = signal<TopicState | undefined>(undefined);
      this.topics.set(name, s);
    }
    return s;
  }

  private set(name: string, st: TopicState | undefined): void {
    (this.topic(name) as Signal<TopicState | undefined>).value = st;
  }

  /** apply folds one snap, delta or resync into the store. */
  apply(msg: Snap | Delta | Resync): ApplyResult {
    if (!isTopic(msg.topic)) return { ok: false, resync: msg.topic, why: "unknown topic" };
    const cur = this.topic(msg.topic).peek();
    switch (msg.t) {
      case "snap":
        this.set(msg.topic, { seq: msg.seq, data: msg.data, stale: false, broken: false });
        return { ok: true };
      case "resync":
        return this.markStale(msg.topic, cur, "the hub dropped deltas");
      case "delta": {
        if (!cur) return this.markStale(msg.topic, cur, "delta before snap");
        if (cur.broken) return { ok: false, resync: msg.topic, why: "waiting for a fresh snap" };
        if (msg.seq !== cur.seq + 1) return this.markStale(msg.topic, cur, `seq ${msg.seq} after ${cur.seq}`);
        const fn = this.reducers.get(topicName(msg.topic)) ?? replace;
        let data: unknown;
        try {
          data = fn(cur.data, msg.data);
        } catch {
          return this.markStale(msg.topic, cur, "bad delta");
        }
        this.set(msg.topic, { seq: msg.seq, data, stale: false, broken: false });
        return { ok: true };
      }
    }
  }

  private markStale(topic: string, cur: TopicState | undefined, why: string): ApplyResult {
    if (cur) this.set(topic, { ...cur, stale: true, broken: true });
    return { ok: false, resync: topic, why };
  }

  /** markAllStale flags every topic as maybe old (after a reconnect),
   * keeping the data readable and the seqs good for a resume. */
  markAllStale(): void {
    batch(() => {
      for (const [name, s] of this.topics) {
        const cur = s.peek();
        if (cur && !cur.stale) this.set(name, { ...cur, stale: true });
      }
    });
  }

  /** freshen clears the stale flag of every topic that is not broken: the
   * resume finished and whatever deltas were missed have arrived. */
  freshen(): void {
    batch(() => {
      for (const [name, s] of this.topics) {
        const cur = s.peek();
        if (cur && cur.stale && !cur.broken) this.set(name, { ...cur, stale: false });
      }
    });
  }

  /** seqs returns the last good seq per topic, for hello.resume. A
   * broken topic is left out: it needs a snap, not the deltas after its seq. */
  seqs(): Dict<number> {
    const out = dict<number>();
    for (const [name, s] of this.topics) {
      const cur = s.peek();
      if (cur && !cur.broken) out[name] = cur.seq;
    }
    return out;
  }

  /** drop forgets a topic (after unsub). */
  drop(topic: string): void {
    this.set(topic, undefined);
  }
}
