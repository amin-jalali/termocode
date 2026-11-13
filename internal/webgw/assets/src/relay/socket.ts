// A WebSocket over a tunnel stream (ADR 0022): the panel's own
// WebSocket code runs unchanged through the relay. The client side of
// RFC 6455: the upgrade request, masked frames out, frames in (with
// fragments, ping and close). The page's link.ts only needs what is
// here: on* handlers, send, close, readyState, protocol.

import { concat } from "./noise";
import { readHead } from "./http";
import type { Stream, Tunnel } from "./tunnel";

const GUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11";
const enc = new TextEncoder();
const MAX_MSG = 16 << 20;

type Data = string | ArrayBufferLike | Blob | ArrayBufferView;

function closeEvent(code: number, reason: string, clean: boolean): Event {
  if (typeof CloseEvent === "function") return new CloseEvent("close", { code, reason, wasClean: clean });
  return Object.assign(new Event("close"), { code, reason, wasClean: clean });
}

function b64(b: Uint8Array): string {
  let s = "";
  for (const c of b) s += String.fromCharCode(c);
  return btoa(s);
}

/** makeSocketClass gives a WebSocket class whose sockets go through the
 * tunnel that tunnel() resolves (a new one when the last one ended). */
export function makeSocketClass(tunnel: () => Promise<Tunnel>): typeof WebSocket {
  class RelaySocket extends EventTarget {
    static readonly CONNECTING = 0;
    static readonly OPEN = 1;
    static readonly CLOSING = 2;
    static readonly CLOSED = 3;
    readonly CONNECTING = 0;
    readonly OPEN = 1;
    readonly CLOSING = 2;
    readonly CLOSED = 3;
    readonly url: string;
    readyState = 0;
    protocol = "";
    extensions = "";
    binaryType: BinaryType = "blob";
    bufferedAmount = 0;
    onopen: ((ev: Event) => void) | null = null;
    onmessage: ((ev: MessageEvent) => void) | null = null;
    onclose: ((ev: Event) => void) | null = null;
    onerror: ((ev: Event) => void) | null = null;
    private s: Stream | null = null;
    private sentClose = false;
    private ended = false;

    constructor(url: string | URL, protocols?: string | string[]) {
      super();
      const u = new URL(String(url), location.href);
      this.url = u.href;
      const list = protocols === undefined ? [] : typeof protocols === "string" ? [protocols] : protocols;
      void this.start(u, list);
    }

    private fire(ev: Event, h: ((ev: never) => void) | null): void {
      try {
        (h as ((ev: Event) => void) | null)?.call(this, ev);
      } finally {
        this.dispatchEvent(ev);
      }
    }

    private fail(code: number): void {
      if (this.ended) return;
      this.ended = true;
      const was = this.readyState;
      this.readyState = 3;
      this.s?.close();
      if (was === 0 || code === 1006) this.fire(new Event("error"), this.onerror);
      this.fire(closeEvent(code, "", false), this.onclose);
    }

    private async start(u: URL, protocols: string[]): Promise<void> {
      try {
        const t = await tunnel();
        const s = t.openStream();
        this.s = s;
        const key = b64(crypto.getRandomValues(new Uint8Array(16)));
        let head = `GET ${u.pathname}${u.search} HTTP/1.1\r\nHost: laptop\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: ${key}\r\nSec-WebSocket-Version: 13\r\n`;
        if (protocols.length) head += `Sec-WebSocket-Protocol: ${protocols.join(", ")}\r\n`;
        await s.write(enc.encode(head + "\r\n"));
        const h = await readHead(s);
        const want = b64(new Uint8Array(await crypto.subtle.digest("SHA-1", enc.encode(key + GUID))));
        if (h.status !== 101 || h.headers.get("sec-websocket-accept") !== want) return this.fail(1006);
        this.protocol = h.headers.get("sec-websocket-protocol") ?? "";
        if (this.ended) return; // closed while connecting
        this.readyState = 1;
        this.fire(new Event("open"), this.onopen);
        await this.readLoop(s, h.rest);
      } catch {
        this.fail(1006);
      }
    }

    private async readLoop(s: Stream, first: Uint8Array): Promise<void> {
      let got = first;
      let msg: Uint8Array[] = [];
      let msgOp = 0;
      const need = async (n: number): Promise<boolean> => {
        while (got.length < n) {
          const b = await s.read();
          if (b === null) return false;
          got = concat(got, b);
        }
        return true;
      };
      for (;;) {
        if (!(await need(2))) return this.fail(1006);
        const fin = (got[0] & 0x80) !== 0;
        const op = got[0] & 0x0f;
        if (got[1] & 0x80) return this.fail(1002); // a server never masks
        let len = got[1] & 0x7f;
        let at = 2;
        if (len === 126) {
          if (!(await need(4))) return this.fail(1006);
          len = (got[2] << 8) | got[3];
          at = 4;
        } else if (len === 127) {
          if (!(await need(10))) return this.fail(1006);
          const v = new DataView(got.buffer, got.byteOffset + 2, 8);
          len = v.getUint32(0) * 0x100000000 + v.getUint32(4);
          at = 10;
        }
        if (len > MAX_MSG) return this.fail(1009);
        if (!(await need(at + len))) return this.fail(1006);
        const payload = got.slice(at, at + len);
        got = got.subarray(at + len);
        if (op === 0x8) {
          const code = payload.length >= 2 ? (payload[0] << 8) | payload[1] : 1005;
          const reason = new TextDecoder().decode(payload.subarray(2));
          if (!this.sentClose) await this.frame(0x8, payload.subarray(0, 2)).catch(() => {});
          this.ended = true;
          this.readyState = 3;
          s.close();
          this.fire(closeEvent(code, reason, true), this.onclose);
          return;
        }
        if (op === 0x9) {
          await this.frame(0xa, payload).catch(() => {});
          continue;
        }
        if (op === 0xa) continue;
        if (op === 0x1 || op === 0x2) {
          msg = [payload];
          msgOp = op;
        } else if (op === 0x0 && msgOp) {
          msg.push(payload);
        } else return this.fail(1002);
        if (!fin) continue;
        const all = concat(...msg);
        msg = [];
        const data = msgOp === 0x1 ? new TextDecoder().decode(all) : this.binaryType === "arraybuffer" ? all.buffer.slice(all.byteOffset, all.byteOffset + all.length) : new Blob([new Uint8Array(all)]);
        msgOp = 0;
        this.fire(new MessageEvent("message", { data }), this.onmessage);
      }
    }

    /** frame sends one masked frame. */
    private frame(op: number, payload: Uint8Array): Promise<void> {
      const s = this.s;
      if (!s) return Promise.reject(new Error("not open"));
      const n = payload.length;
      const head = n < 126 ? Uint8Array.of(0x80 | op, 0x80 | n) : n < 65536 ? Uint8Array.of(0x80 | op, 0x80 | 126, n >> 8, n & 0xff) : (() => {
        const h = new Uint8Array(10);
        h[0] = 0x80 | op;
        h[1] = 0x80 | 127;
        const v = new DataView(h.buffer);
        v.setUint32(2, Math.floor(n / 0x100000000));
        v.setUint32(6, n >>> 0);
        return h;
      })();
      const mask = crypto.getRandomValues(new Uint8Array(4));
      const body = new Uint8Array(n);
      for (let i = 0; i < n; i++) body[i] = payload[i] ^ mask[i & 3];
      this.bufferedAmount += n;
      return s.write(concat(head, mask, body)).finally(() => (this.bufferedAmount -= n));
    }

    send(data: Data): void {
      if (this.readyState === 0) throw new DOMException("still connecting", "InvalidStateError");
      if (this.readyState !== 1) return;
      if (typeof data === "string") {
        void this.frame(0x1, enc.encode(data)).catch(() => this.fail(1006));
      } else if (data instanceof Blob) {
        void data.arrayBuffer().then((b) => this.frame(0x2, new Uint8Array(b))).catch(() => this.fail(1006));
      } else if (ArrayBuffer.isView(data)) {
        void this.frame(0x2, new Uint8Array(data.buffer, data.byteOffset, data.byteLength).slice()).catch(() => this.fail(1006));
      } else {
        void this.frame(0x2, new Uint8Array(data as ArrayBuffer).slice()).catch(() => this.fail(1006));
      }
    }

    close(code = 1000, reason = ""): void {
      if (this.readyState >= 2) return;
      if (this.readyState === 0) {
        this.fail(1006);
        return;
      }
      this.readyState = 2;
      this.sentClose = true;
      const r = enc.encode(reason).subarray(0, 123);
      void this.frame(0x8, concat(Uint8Array.of(code >> 8, code & 0xff), r)).catch(() => this.fail(1006));
    }
  }
  return RelaySocket as unknown as typeof WebSocket;
}

