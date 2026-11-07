// The door (MOB-50): one phone link, several projects. The paired
// gateway forwards frames that carry a "ws" (a workspace key, never a
// path) to that workspace's hub and sets ws on what comes back. This
// module keeps one topic store per other workspace; the home workspace
// stays in the link's main store. It is phone-only and loaded lazily, so
// the desktop app.js does not carry it.
import { signal } from "@preact/signals";
import type { Meta, ServerMsg } from "../../../../webproto/proto";
import type { Store } from "../state/store";
import type { DoorSink, Link, Refusal } from "./link";

/** Project is another workspace behind the door: its topic copy and the
 * topics the page wants from it. */
interface Project {
  readonly store: Store;
  readonly wanted: Set<string>;
  readonly resyncing: Set<string>;
}

const MAX_TOPICS_PER_SUB = 32;

export class Door implements DoorSink {
  /** meta is the meta of each other workspace's hub (role, caps), by ws. */
  readonly meta = signal<ReadonlyMap<string, Meta>>(new Map());
  /** gone says why a project's hub ended, by ws, until it is asked again. */
  readonly gone = signal<ReadonlyMap<string, string>>(new Map());
  /** refused holds the topics a project's hub would not send, by "<ws>/<topic>". */
  readonly refused = signal<ReadonlyMap<string, Refusal>>(new Map());

  private readonly projects = new Map<string, Project>();
  private readonly evHandlers = new Set<(ws: string, kind: string, data: unknown) => void>();

  constructor(private readonly link: Link, private readonly home: Store) {
    link.attachDoor(this);
  }

  private isHome(ws: string): boolean {
    return ws === "" || ws === this.link.home();
  }

  /** store is the topic copy of ws (the main store for the home one). */
  store(ws: string): Store {
    return this.isHome(ws) ? this.home : this.project(ws).store;
  }

  private project(ws: string): Project {
    let p = this.projects.get(ws);
    if (!p) {
      p = { store: this.home.fork(), wanted: new Set(), resyncing: new Set() };
      this.projects.set(ws, p);
    }
    return p;
  }

  /** subscribe asks ws for topics, now if online and after every reconnect. */
  subscribe(ws: string, ...topics: string[]): void {
    if (this.isHome(ws)) return this.link.subscribe(...topics);
    const p = this.project(ws);
    const fresh = topics.filter((t) => !p.wanted.has(t));
    fresh.forEach((t) => p.wanted.add(t));
    this.clearGone(ws);
    this.sendSub(ws, fresh);
  }

  unsubscribe(ws: string, ...topics: string[]): void {
    if (this.isHome(ws)) return this.link.unsubscribe(...topics);
    const p = this.projects.get(ws);
    if (!p) return;
    const gone = topics.filter((t) => p.wanted.delete(t));
    gone.forEach((t) => p.store.drop(t));
    this.clearRefused(gone.map((t) => projKey(ws, t)));
    if (gone.length) this.link.sendFrame({ t: "unsub", topics: gone, ws });
    if (p.wanted.size === 0) this.projects.delete(ws);
  }

  /** retry asks a project whose hub ended for its topics again. */
  retry(ws: string): void {
    const p = this.projects.get(ws);
    if (!p) return;
    this.clearGone(ws);
    this.sendSub(ws, [...p.wanted]);
  }

  /** request sends one op to ws's hub; it fails at once while offline. */
  request(ws: string, op: string, args: unknown = {}): Promise<unknown> {
    return this.link.requestWithID(op, args, this.isHome(ws) ? undefined : ws).result;
  }

  /** onEvent hears ev messages of other workspaces; it returns the undo. */
  onEvent(fn: (ws: string, kind: string, data: unknown) => void): () => void {
    this.evHandlers.add(fn);
    return () => this.evHandlers.delete(fn);
  }

  message(ws: string, msg: ServerMsg): void {
    const p = this.projects.get(ws);
    switch (msg.t) {
      case "meta": {
        const next = new Map(this.meta.peek());
        next.set(ws, msg);
        this.meta.value = next;
        return;
      }
      case "snap":
        if (!p) return;
        p.resyncing.delete(msg.topic);
        this.clearRefused([projKey(ws, msg.topic)]);
        p.store.apply(msg);
        return;
      case "delta":
      case "resync": {
        if (!p) return;
        const r = p.store.apply(msg);
        if (!r.ok && !p.resyncing.has(r.resync)) {
          p.resyncing.add(r.resync);
          this.link.sendFrame({ t: "sub", topics: [r.resync], ws });
        }
        return;
      }
      case "err": {
        // An err without an id: the hub would not send a topic.
        const topic = isObj(msg.data) ? msg.data.topic : undefined;
        if (!p || typeof topic !== "string" || !p.wanted.has(topic)) return;
        const next = new Map(this.refused.peek());
        next.set(projKey(ws, topic), { code: msg.code, msg: msg.msg });
        this.refused.value = next;
        return;
      }
      case "ev": {
        if (msg.kind === "ws.gone") {
          const why = isObj(msg.data) && typeof msg.data.why === "string" ? msg.data.why : "closed";
          if (p) {
            p.store.markAllStale();
            p.resyncing.clear();
          }
          const next = new Map(this.gone.peek());
          next.set(ws, why);
          this.gone.value = next;
        }
        const data = msg.data;
        this.evHandlers.forEach((fn) => fn(ws, msg.kind, data));
        return;
      }
    }
  }

  online(): void {
    // Project hubs have no resume: every wanted topic gets a fresh snap.
    for (const [ws, p] of this.projects) {
      p.resyncing.clear();
      this.sendSub(ws, [...p.wanted]);
    }
  }

  down(): void {
    for (const p of this.projects.values()) p.store.markAllStale();
  }

  private sendSub(ws: string, topics: string[]): void {
    for (let i = 0; i < topics.length; i += MAX_TOPICS_PER_SUB) {
      if (!this.link.sendFrame({ t: "sub", topics: topics.slice(i, i + MAX_TOPICS_PER_SUB), ws })) return;
    }
  }

  private clearGone(ws: string): void {
    const cur = this.gone.peek();
    if (!cur.has(ws)) return;
    const next = new Map(cur);
    next.delete(ws);
    this.gone.value = next;
  }

  private clearRefused(keys: string[]): void {
    const cur = this.refused.peek();
    if (!keys.some((k) => cur.has(k))) return;
    const next = new Map(cur);
    keys.forEach((k) => next.delete(k));
    this.refused.value = next;
  }
}

/** projKey names a topic of another workspace: "<ws>/<topic>". */
export function projKey(ws: string, topic: string): string {
  return `${ws}/${topic}`;
}

function isObj(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}
