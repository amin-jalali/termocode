// fetch over a tunnel stream (ADR 0022): one HTTP/1.1 request per
// stream, "Connection: close", answered by the laptop's own handlers.
// Enough for the panel's JSON posts; no cookies, no redirects.

import { concat } from "./noise";
import type { Stream, Tunnel } from "./tunnel";

const enc = new TextEncoder();
const dec = new TextDecoder();

/** Head is a parsed response head. */
export interface Head {
  status: number;
  headers: Headers;
  rest: Uint8Array; // bytes after the head
}

/** readHead reads up to the blank line after the headers. */
export async function readHead(s: Stream): Promise<Head> {
  let got: Uint8Array = new Uint8Array(0);
  for (;;) {
    const end = findCRLF2(got);
    if (end >= 0) {
      const lines = dec.decode(got.subarray(0, end)).split("\r\n");
      const m = /^HTTP\/1\.[01] (\d{3})/.exec(lines[0] ?? "");
      if (!m) throw new Error("relay: bad response");
      const headers = new Headers();
      for (const l of lines.slice(1)) {
        const i = l.indexOf(":");
        if (i <= 0) continue;
        const k = l.slice(0, i).trim().toLowerCase();
        if (k === "set-cookie") continue; // the tunnel keeps no cookies
        headers.append(k, l.slice(i + 1).trim());
      }
      return { status: Number(m[1]), headers, rest: got.subarray(end + 4) };
    }
    if (got.length > 64 * 1024) throw new Error("relay: head too big");
    const b = await s.read();
    if (b === null) throw new Error("relay: no response");
    got = concat(got, b);
  }
}

function findCRLF2(b: Uint8Array): number {
  for (let i = 0; i + 3 < b.length; i++) {
    if (b[i] === 13 && b[i + 1] === 10 && b[i + 2] === 13 && b[i + 3] === 10) return i;
  }
  return -1;
}

/** readBody reads the body after a head: chunked, by length, or to the end. */
async function readBody(s: Stream, h: Head): Promise<Uint8Array> {
  let got = h.rest;
  const more = async (): Promise<boolean> => {
    const b = await s.read();
    if (b === null) return false;
    got = concat(got, b);
    return true;
  };
  if ((h.headers.get("transfer-encoding") ?? "").toLowerCase().includes("chunked")) {
    const parts: Uint8Array[] = [];
    let at = 0;
    for (;;) {
      let nl = -1;
      for (;;) {
        for (let i = at; i + 1 < got.length; i++) {
          if (got[i] === 13 && got[i + 1] === 10) {
            nl = i;
            break;
          }
        }
        if (nl >= 0 || !(await more())) break;
      }
      if (nl < 0) throw new Error("relay: cut chunk");
      const size = parseInt(dec.decode(got.subarray(at, nl)), 16);
      if (!(size >= 0)) throw new Error("relay: bad chunk");
      at = nl + 2;
      while (got.length < at + size + 2) if (!(await more())) throw new Error("relay: cut chunk");
      if (size === 0) return concat(...parts);
      parts.push(got.slice(at, at + size));
      at += size + 2;
    }
  }
  const len = h.headers.get("content-length");
  if (len !== null) {
    const n = Number(len);
    while (got.length < n) if (!(await more())) throw new Error("relay: cut body");
    return got.subarray(0, n);
  }
  while (await more()) {
    // to the end of the stream
  }
  return got;
}

function bodyBytes(body: BodyInit | null | undefined): Uint8Array {
  if (body === null || body === undefined) return new Uint8Array(0);
  if (typeof body === "string") return enc.encode(body);
  if (body instanceof Uint8Array) return body;
  if (body instanceof ArrayBuffer) return new Uint8Array(body);
  throw new Error("relay: this body kind is not supported");
}

/** tunnelFetch is fetch for the laptop's own paths, over one stream. */
export async function tunnelFetch(t: Tunnel, url: URL, init: RequestInit = {}): Promise<Response> {
  const method = (init.method ?? "GET").toUpperCase();
  const body = bodyBytes(init.body);
  const headers = new Headers(init.headers);
  let head = `${method} ${url.pathname}${url.search} HTTP/1.1\r\nHost: laptop\r\nConnection: close\r\n`;
  headers.forEach((v, k) => {
    if (!/^(host|connection|content-length|cookie|origin)$/i.test(k)) head += `${k}: ${v}\r\n`;
  });
  if (method !== "GET" || body.length > 0) head += `Content-Length: ${body.length}\r\n`;
  const s = t.openStream();
  const abort = () => s.close();
  init.signal?.addEventListener("abort", abort);
  try {
    await s.write(concat(enc.encode(head + "\r\n"), body));
    const h = await readHead(s);
    const b = await readBody(s, h);
    const status = h.status;
    const nobody = status === 204 || status === 304 || method === "HEAD";
    return new Response(nobody ? null : new Uint8Array(b), { status, headers: h.headers });
  } catch {
    throw new TypeError("Failed to fetch"); // as a real network error
  } finally {
    init.signal?.removeEventListener("abort", abort);
    s.close();
  }
}
