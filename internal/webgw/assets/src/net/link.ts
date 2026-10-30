// The link to the hub: one WebSocket (subprotocol termocode.v2), hello
// with resume, topic subscriptions, requests with ids, ping/pong for the
// RTT and liveness, reconnect with backoff. Nothing is queued while the
// link is down: requests fail at once and the UI locks its inputs
// (web UX P5), so no key or edit is sent late by surprise.
import { batch, computed, signal } from "@preact/signals";
import type { Meta, ServerMsg } from "../../../../webproto/proto";
import { decodeServerMsg, PROTOCOL_VERSION, ProtoError, SUBPROTOCOL } from "../proto/guard";
import { has } from "../state/dict";
import type { Store } from "../state/store";
import { backoff } from "./backoff";

export type LinkState = "connecting" | "online" | "offline" | "final";

/** Close codes after which retrying cannot help: a new link is needed,
 * or the session is over. Everything else is retried with backoff. */
const FINAL_CODES: ReadonlySet<number> = new Set([
  1000, 1003, 1008, 1009, 4001, 4002, 4003, 4004, 4005, 4006, 4007, 4009, 4010, 4011, 4012,
]);

export const PING_EVERY_MS = 5000;
export const DEAD_AFTER_MS = 15000;
const HELLO_TIMEOUT_MS = 10000;
const REQUEST_TIMEOUT_MS = 30000;
const FRESHEN_AFTER_MS = 300;
const MAX_TOPICS_PER_SUB = 32;

export class OfflineError extends Error {
  constructor() {
    super("offline");
  }
}

/** ReqError is an err reply from the hub; code is from webproto.ErrCodes. */
export class ReqError extends Error {
  constructor(readonly code: string, msg: string, readonly data?: unknown) {
    super(msg);
  }
}

/** Refusal is why the hub would not send a topic (an err without an id
 * whose data names the topic): no workspace, a closed terminal, a cap. */
export interface Refusal {
  code: string;
  msg: string;
}

export interface LinkDeps {
  store: Store;
  url: string;
  token: () => string | null;
  deviceName?: string;
  WS?: typeof WebSocket;
  now?: () => number;
  random?: () => number;
}

interface Pending {
  resolve: (v: unknown) => void;
  reject: (e: Error) => void;
  timer: ReturnType<typeof setTimeout>;
}

export class Link {
  readonly state = signal<LinkState>("connecting");
  /** retryAt is when the next attempt starts (ms), while offline. */
  readonly retryAt = signal(0);
  /** downSince is when the link went down (ms), null while up. */
  readonly downSince = signal<number | null>(null);
  /** reconnectedAfter is how long the last outage took (ms), null if none yet. */
  readonly reconnectedAfter = signal<number | null>(null);
  readonly rtt = signal<number | null>(null);
  readonly meta = signal<Meta | null>(null);
  /** finalCode is the close code that ended the session for good. */
  readonly finalCode = signal<number | null>(null);
  readonly live = computed(() => this.state.value === "online");
  /** refused holds the topics the hub would not send, until a snap comes. */
  readonly refused = signal<ReadonlyMap<string, Refusal>>(new Map());

  private ws: WebSocket | null = null;
  private conn = "";
  private attempt = 0;
  private reqID = 0;
  private pingN = 0;
  private pingSent = 0;
  private pingWant = "";
  private lastHeard = 0;
  private visible = true;
  private byeCode: number | null = null;
  private readonly wanted = new Set<string>();
  private readonly resyncing = new Set<string>();
  private readonly pending = new Map<number, Pending>();
  private readonly evHandlers = new Set<(kind: string, data: unknown) => void>();
  private retryTimer: ReturnType<typeof setTimeout> | undefined;
  private pingTimer: ReturnType<typeof setInterval> | undefined;
  private helloTimer: ReturnType<typeof setTimeout> | undefined;
  private freshenTimer: ReturnType<typeof setTimeout> | undefined;
  private readonly WS: typeof WebSocket;
  private readonly now: () => number;
  private readonly random: () => number;

  constructor(private readonly deps: LinkDeps) {
    this.WS = deps.WS ?? WebSocket;
    this.now = deps.now ?? (() => Date.now());
    this.random = deps.random ?? Math.random;
  }

  /** start opens the link; call once. */
  start(): void {
    this.open();
  }

  /** tryNow skips the wait before the next attempt. */
  tryNow(): void {
    if (this.state.peek() !== "offline") return;
    clearTimeout(this.retryTimer);
    this.open();
  }

  /** setVisible pauses pings while the page is hidden and checks the link when it shows again. */
  setVisible(v: boolean): void {
    this.visible = v;
    if (v && this.state.peek() === "online") this.ping();
    if (v && this.state.peek() === "offline") this.tryNow();
  }

  /** subscribe adds topics the page wants; they are sent now if online and after every reconnect. */
  subscribe(...topics: string[]): void {
    const fresh = topics.filter((t) => !this.wanted.has(t));
    fresh.forEach((t) => this.wanted.add(t));
    if (fresh.length && this.state.peek() === "online") this.sendSub(fresh);
  }

  unsubscribe(...topics: string[]): void {
    const gone = topics.filter((t) => this.wanted.delete(t));
    gone.forEach((t) => this.deps.store.drop(t));
    this.clearRefused(gone);
    if (gone.length && this.state.peek() === "online") this.send({ t: "unsub", topics: gone });
  }

  /** retry asks again for a topic the hub refused. */
  retry(topic: string): void {
    if (!this.wanted.has(topic)) return;
    this.clearRefused([topic]);
    if (this.state.peek() === "online") this.sendSub([topic]);
  }

  /** onEvent registers a handler for ev messages; it returns the undo. */
  onEvent(fn: (kind: string, data: unknown) => void): () => void {
    this.evHandlers.add(fn);
    return () => this.evHandlers.delete(fn);
  }

  /** request sends one op; it fails at once while the link is down. */
  request(op: string, args: unknown = {}): Promise<unknown> {
    return this.requestWithID(op, args).result;
  }

  /** requestWithID is request that also says the request's id (0 when
   * offline), for ops whose events carry it (search.batch). */
  requestWithID(op: string, args: unknown = {}): { id: number; result: Promise<unknown> } {
    if (this.state.peek() !== "online" || !this.ws) return { id: 0, result: Promise.reject(new OfflineError()) };
    const id = ++this.reqID;
    const result = new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new ReqError("busy", "no answer"));
      }, REQUEST_TIMEOUT_MS);
      this.pending.set(id, { resolve, reject, timer });
      this.send({ t: "req", id, op, args });
    });
    return { id, result };
  }

  /** stop says bye and closes for good (the page is closing). */
  stop(): void {
    this.send({ t: "bye" });
    this.finish(1000);
  }

  private open(): void {
    const token = this.deps.token();
    if (!token) {
      this.finish(4001);
      return;
    }
    if (this.state.peek() !== "offline") this.state.value = "connecting";
    this.byeCode = null;
    let ws: WebSocket;
    try {
      ws = new this.WS(this.deps.url, [SUBPROTOCOL]);
    } catch {
      this.down(1006);
      return;
    }
    this.ws = ws;
    ws.onopen = () => {
      if (ws !== this.ws) return;
      const seqs = this.deps.store.seqs();
      this.send({
        t: "hello",
        v: PROTOCOL_VERSION,
        token,
        ...(this.deps.deviceName ? { device_name: this.deps.deviceName } : {}),
        ...(this.conn ? { resume: { conn: this.conn, topics: seqs } } : {}),
      });
      this.helloTimer = setTimeout(() => ws.close(), HELLO_TIMEOUT_MS);
    };
    ws.onmessage = (ev: MessageEvent) => {
      if (ws !== this.ws || typeof ev.data !== "string") return;
      let msg: ServerMsg;
      try {
        msg = decodeServerMsg(ev.data);
      } catch (e) {
        if (e instanceof ProtoError) return; // dropped; a seq gap will ask for a resync
        throw e;
      }
      this.lastHeard = this.now();
      this.handle(msg);
    };
    ws.onclose = (ev: CloseEvent) => {
      if (ws !== this.ws) return;
      this.down(this.byeCode ?? ev.code);
    };
  }

  private handle(msg: ServerMsg): void {
    const store = this.deps.store;
    switch (msg.t) {
      case "meta": {
        clearTimeout(this.helloTimer);
        const resumed = this.conn !== "" && msg.conn === this.conn;
        this.conn = msg.conn;
        this.attempt = 0;
        batch(() => {
          const since = this.downSince.peek();
          if (since !== null) this.reconnectedAfter.value = this.now() - since;
          this.downSince.value = null;
          this.meta.value = msg;
          this.state.value = "online";
        });
        const seqs = store.seqs();
        const need = [...this.wanted].filter((t) => !resumed || !has(seqs, t));
        if (need.length) this.sendSub(need);
        clearTimeout(this.freshenTimer);
        this.freshenTimer = setTimeout(() => store.freshen(), FRESHEN_AFTER_MS);
        this.startPings();
        return;
      }
      case "snap":
        this.resyncing.delete(msg.topic);
        this.clearRefused([msg.topic]);
        store.apply(msg);
        return;
      case "delta":
      case "resync": {
        const r = store.apply(msg);
        if (!r.ok && !this.resyncing.has(r.resync)) {
          this.resyncing.add(r.resync);
          this.send({ t: "sub", topics: [r.resync] });
        }
        return;
      }
      case "res":
        this.settle(msg.id, (p) => p.resolve(msg.data));
        return;
      case "err":
        if (msg.id !== undefined) this.settle(msg.id, (p) => p.reject(new ReqError(msg.code, msg.msg, msg.data)));
        else this.refuse(msg.code, msg.msg, msg.data);
        return;
      case "ev":
        this.evHandlers.forEach((fn) => fn(msg.kind, msg.data));
        return;
      case "pong":
        if (msg.n === this.pingWant) {
          this.rtt.value = this.now() - this.pingSent;
          this.pingWant = "";
        }
        return;
      case "bye":
        this.byeCode = msg.code;
        return;
    }
  }

  /** refuse records a sub the hub refused; the topic stays wanted, so
   * the next connection asks again. */
  private refuse(code: string, text: string, data: unknown): void {
    const topic = typeof data === "object" && data !== null ? (data as Record<string, unknown>).topic : undefined;
    if (typeof topic !== "string" || !this.wanted.has(topic)) return;
    const next = new Map(this.refused.peek());
    next.set(topic, { code, msg: text });
    this.refused.value = next;
  }

  private clearRefused(topics: string[]): void {
    const cur = this.refused.peek();
    if (!topics.some((t) => cur.has(t))) return;
    const next = new Map(cur);
    topics.forEach((t) => next.delete(t));
    this.refused.value = next;
  }

  private settle(id: number, fn: (p: Pending) => void): void {
    const p = this.pending.get(id);
    if (!p) return;
    clearTimeout(p.timer);
    this.pending.delete(id);
    fn(p);
  }

  private sendSub(topics: string[]): void {
    for (let i = 0; i < topics.length; i += MAX_TOPICS_PER_SUB) {
      this.send({ t: "sub", topics: topics.slice(i, i + MAX_TOPICS_PER_SUB) });
    }
  }

  private send(msg: object): void {
    if (this.ws && this.ws.readyState === 1) this.ws.send(JSON.stringify(msg));
  }

  private startPings(): void {
    clearInterval(this.pingTimer);
    this.pingTimer = setInterval(() => {
      if (this.now() - this.lastHeard > DEAD_AFTER_MS) {
        this.ws?.close();
        this.down(1006);
        return;
      }
      if (this.visible) this.ping();
    }, PING_EVERY_MS);
    this.ping();
  }

  private ping(): void {
    this.pingN = (this.pingN + 1) % 1e9;
    this.pingWant = String(this.pingN);
    this.pingSent = this.now();
    this.send({ t: "ping", n: this.pingWant });
  }

  private stopTimers(): void {
    clearInterval(this.pingTimer);
    clearTimeout(this.helloTimer);
    clearTimeout(this.freshenTimer);
    for (const [id, p] of this.pending) {
      clearTimeout(p.timer);
      p.reject(new OfflineError());
      this.pending.delete(id);
    }
    this.resyncing.clear();
  }

  private down(code: number): void {
    const ws = this.ws;
    this.ws = null;
    if (ws) {
      ws.onopen = ws.onmessage = ws.onclose = null;
    }
    this.stopTimers();
    if (FINAL_CODES.has(code)) {
      this.finish(code);
      return;
    }
    const delay = backoff(this.attempt++, this.random());
    batch(() => {
      if (this.state.peek() === "online") this.deps.store.markAllStale();
      if (this.downSince.peek() === null) this.downSince.value = this.now();
      this.retryAt.value = this.now() + delay;
      this.state.value = "offline";
    });
    clearTimeout(this.retryTimer);
    this.retryTimer = setTimeout(() => this.open(), delay);
  }

  private finish(code: number): void {
    const ws = this.ws;
    this.ws = null;
    if (ws) {
      ws.onopen = ws.onmessage = ws.onclose = null;
      try {
        ws.close(1000);
      } catch {
        // Already closed.
      }
    }
    this.stopTimers();
    clearTimeout(this.retryTimer);
    batch(() => {
      this.finalCode.value = code;
      this.state.value = "final";
    });
  }
}
