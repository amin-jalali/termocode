// The tunnel to the laptop through the relay (ADR 0022): one Noise
// session over one relay WebSocket, with byte streams inside it, one per
// HTTP request or WebSocket of the page. The frames and windows match
// internal/relaytun/session.go.

import { concat, handshake, type CipherState, type StaticKey } from "./noise";

const OPEN = 1;
const DATA = 2;
const FIN = 3;
const CLOSE = 4;
const WINDOW = 5;
/** WINDOW_BYTES is each stream's receive window. */
const WINDOW_BYTES = 256 * 1024;
const CHUNK = 16 * 1024;

/** Wire carries whole messages: the relay WebSocket, or a test pipe. */
export interface Wire {
  send(b: Uint8Array): void;
  close(): void;
  onmessage: ((b: Uint8Array) => void) | null;
  onclose: (() => void) | null;
}

/** RefusedError: the laptop said no after the handshake (why: not_paired
 * or refused). */
export class RefusedError extends Error {
  constructor(readonly why: string) {
    super("relay: refused: " + why);
  }
}

export class Tunnel {
  private readonly streams = new Map<number, Stream>();
  private nextID = 1;
  private sendQ: Promise<unknown> = Promise.resolve();
  private recvQ: Promise<unknown> = Promise.resolve();
  private dead = false;
  /** onclose runs once when the session ends. */
  onclose: (() => void) | null = null;

  private constructor(
    private readonly wire: Wire,
    private readonly tx: CipherState,
    private readonly rx: CipherState,
  ) {}

  /** open runs the handshake on wire with the gateway key gw and hello. */
  static async open(wire: Wire, s: StaticKey, gw: Uint8Array, hello: Record<string, unknown>): Promise<Tunnel> {
    const inbox: Uint8Array[] = [];
    let waiting: ((b: Uint8Array | null) => void) | null = null;
    let closed = false;
    wire.onmessage = (b) => {
      if (waiting) {
        const w = waiting;
        waiting = null;
        w(b);
      } else inbox.push(b);
    };
    wire.onclose = () => {
      closed = true;
      if (waiting) waiting(null);
    };
    const read = (): Promise<Uint8Array> =>
      new Promise((ok, fail) => {
        const b = inbox.shift();
        if (b) return ok(b);
        if (closed) return fail(new Error("relay: closed"));
        waiting = (m) => (m ? ok(m) : fail(new Error("relay: closed")));
      });
    let hs;
    try {
      hs = await handshake((b) => wire.send(b), read, s, gw, new TextEncoder().encode(JSON.stringify({ v: 1, ...hello })));
    } catch (e) {
      wire.close();
      throw e;
    }
    let w: { ok?: unknown; why?: unknown } = {};
    try {
      w = JSON.parse(new TextDecoder().decode(hs.reply)) as typeof w;
    } catch {
      // no answer: refused
    }
    if (w.ok !== true) {
      wire.close();
      throw new RefusedError(typeof w.why === "string" ? w.why : "refused");
    }
    const t = new Tunnel(wire, hs.send, hs.recv);
    wire.onmessage = (b) => t.got(b);
    wire.onclose = () => t.end();
    for (const b of inbox) t.got(b);
    if (closed) t.end();
    return t;
  }

  get closed(): boolean {
    return this.dead;
  }

  /** openStream opens a stream to the laptop. */
  openStream(): Stream {
    if (this.dead) throw new Error("relay: closed");
    const id = this.nextID;
    this.nextID += 2;
    const st = new Stream(this, id);
    this.streams.set(id, st);
    this.frame(OPEN, id);
    return st;
  }

  close(): void {
    this.wire.close();
    this.end();
  }

  private end(): void {
    if (this.dead) return;
    this.dead = true;
    for (const st of this.streams.values()) st.kill();
    this.streams.clear();
    this.onclose?.();
  }

  forget(id: number): void {
    this.streams.delete(id);
  }

  /** frame encrypts and sends one frame, in order. */
  frame(typ: number, id: number, body: Uint8Array = new Uint8Array(0)): Promise<void> {
    const pt = new Uint8Array(5 + body.length);
    pt[0] = typ;
    new DataView(pt.buffer).setUint32(1, id);
    pt.set(body, 5);
    const p = this.sendQ.then(async () => {
      if (this.dead) throw new Error("relay: closed");
      this.wire.send(await this.tx.encrypt(new Uint8Array(0), pt));
    });
    this.sendQ = p.catch(() => {});
    return p;
  }

  private got(ct: Uint8Array): void {
    this.recvQ = this.recvQ.then(async () => {
      if (this.dead) return;
      let pt: Uint8Array;
      try {
        pt = await this.rx.decrypt(new Uint8Array(0), ct);
      } catch {
        this.close(); // changed, dropped or reordered: the session ends
        return;
      }
      if (pt.length < 5) return this.close();
      const id = new DataView(pt.buffer, pt.byteOffset).getUint32(1);
      const st = this.streams.get(id);
      const body = pt.subarray(5);
      if (!st) return; // late frames of an ended stream
      switch (pt[0]) {
        case DATA:
          st.gotData(body);
          break;
        case FIN:
          st.gotFin();
          break;
        case CLOSE:
          st.gotClose();
          break;
        case WINDOW:
          if (body.length === 4) st.gotWindow(new DataView(body.buffer, body.byteOffset).getUint32(0));
          break;
        default:
          this.close();
      }
    });
  }
}

/** Stream is one byte stream to the laptop. */
export class Stream {
  private buf: Uint8Array[] = [];
  private finIn = false;
  private closed = false;
  private dead = false;
  private sendWin = WINDOW_BYTES;
  private unacked = 0;
  private wakes: (() => void)[] = [];

  constructor(
    private readonly t: Tunnel,
    readonly id: number,
  ) {}

  /** kick wakes every waiting read and write: each checks again. */
  private kick(): void {
    const ws = this.wakes;
    this.wakes = [];
    for (const w of ws) w();
  }

  private sleep(): Promise<void> {
    return new Promise((ok) => this.wakes.push(ok));
  }

  gotData(b: Uint8Array): void {
    if (this.closed || this.finIn) return;
    this.buf.push(b.slice());
    this.kick();
  }

  gotFin(): void {
    this.finIn = true;
    this.kick();
  }

  gotClose(): void {
    this.finIn = true;
    this.dead = true;
    this.t.forget(this.id);
    this.kick();
  }

  gotWindow(n: number): void {
    this.sendWin += n;
    this.kick();
  }

  kill(): void {
    this.dead = true;
    this.finIn = true;
    this.kick();
  }

  /** read gives the next bytes, or null at the end. */
  async read(): Promise<Uint8Array | null> {
    for (;;) {
      const b = this.buf.shift();
      if (b) {
        this.unacked += b.length;
        if (this.unacked >= WINDOW_BYTES / 4 || (this.buf.length === 0 && !this.finIn)) {
          const n = new Uint8Array(4);
          new DataView(n.buffer).setUint32(0, this.unacked);
          this.unacked = 0;
          if (!this.closed && !this.dead) void this.t.frame(WINDOW, this.id, n).catch(() => {});
        }
        return b;
      }
      if (this.finIn || this.closed) return null;
      await this.sleep();
    }
  }

  /** write sends b, waiting for the laptop's window. */
  async write(b: Uint8Array): Promise<void> {
    let off = 0;
    while (off < b.length) {
      if (this.closed || this.dead) throw new Error("relay: stream closed");
      if (this.sendWin === 0) {
        await this.sleep();
        continue;
      }
      const n = Math.min(b.length - off, CHUNK, this.sendWin);
      this.sendWin -= n;
      await this.t.frame(DATA, this.id, b.subarray(off, off + n));
      off += n;
    }
  }

  /** close ends the stream both ways. */
  close(): void {
    if (this.closed) return;
    this.closed = true;
    this.t.forget(this.id);
    if (!this.dead) void this.t.frame(CLOSE, this.id).catch(() => {});
    this.kick();
  }
}

/** readAll reads s to its end. */
export async function readAll(s: Stream): Promise<Uint8Array> {
  const parts: Uint8Array[] = [];
  for (;;) {
    const b = await s.read();
    if (b === null) return concat(...parts);
    parts.push(b);
  }
}
