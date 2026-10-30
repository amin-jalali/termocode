// window.__replay for TestStoreReplayMatchesGo (dev kit only): a fresh
// store with the page's real reducers folds a Go replay golden — snap₀ at
// seq 1, then each delta — and returns the data it ends with.
import { Store } from "../state/store";
import { registerTopicReducers } from "../state/topics";

interface Golden {
  topic: string;
  snap0: unknown;
  deltas?: unknown[];
}

export function replay(c: Golden): { ok: boolean; why?: string; data?: unknown } {
  const store = new Store();
  registerTopicReducers(store);
  store.apply({ t: "snap", topic: c.topic, seq: 1, data: c.snap0 });
  let seq = 1;
  for (const d of c.deltas ?? []) {
    const r = store.apply({ t: "delta", topic: c.topic, seq: ++seq, data: d });
    if (!r.ok) return { ok: false, why: r.why };
  }
  return { ok: true, data: store.topic(c.topic).peek()?.data };
}

(globalThis as unknown as { __replay: typeof replay }).__replay = replay;
