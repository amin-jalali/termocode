// The relay's page (relay.html, ADR 0022): it runs before the panel's
// own code. It reads the pairing link (#r=<room>&k=<laptop key>&p=<one
// time secret>) and keeps the room and key for later visits, makes this
// browser's relay key once (IndexedDB, not extractable), opens the Noise
// tunnel through the relay to the laptop, and puts fetch and WebSocket
// for the laptop's own paths through it. A paired phone signs in with
// its relay key alone (and the one-time code, in the panel's code step,
// when the laptop asks for it). Then it loads the panel (app.js, by its SRI hash):
// the panel runs unchanged, and the relay sees only ciphertext.

import { newStaticKey, unb64url, type StaticKey } from "./noise";
import { tunnelFetch } from "./http";
import { keepRelaySignIn } from "./signin";
import { makeSocketClass } from "./socket";
import { Tunnel, type Wire } from "./tunnel";

const CONF = "termocode.relay";
const LINK = /^#r=([A-Za-z0-9_-]{22})&k=([A-Za-z0-9_-]{43})(?:&p=([A-Za-z0-9_-]{43}))?$/;
/** OURS are the laptop's own paths: they go through the tunnel. */
const OURS = /^\/(auth|ws|watch)(\/|$)|^\/(pair|login)\//;

interface Conf {
  room: string;
  k: string;
}

function readConf(): Conf | null {
  try {
    const v = JSON.parse(localStorage.getItem(CONF) ?? "null") as Partial<Conf> | null;
    if (v && typeof v.room === "string" && typeof v.k === "string" && LINK.test(`#r=${v.room}&k=${v.k}`)) return { room: v.room, k: v.k };
  } catch {
    // none kept
  }
  return null;
}

// The link leaves the URL at once; only the pairing secret stays, as the
// panel's own pairing link (#p=), for its pairing screen.
let conf = readConf();
let pair: string | null = null;
const m = LINK.exec(location.hash);
if (m) {
  conf = { room: m[1], k: m[2] };
  pair = m[3] ?? null;
  try {
    localStorage.setItem(CONF, JSON.stringify(conf));
  } catch {
    // this visit only
  }
  history.replaceState(null, "", location.pathname + (pair ? "#p=" + pair : "#/chat"));
}

/** staticKey is this browser's relay key, made once. */
async function staticKey(): Promise<StaticKey> {
  const db = await new Promise<IDBDatabase>((ok, fail) => {
    const r = indexedDB.open("termocode-relay", 1);
    r.onupgradeneeded = () => r.result.createObjectStore("keys");
    r.onsuccess = () => ok(r.result);
    r.onerror = () => fail(r.error);
  });
  const get = <T>(): Promise<T | undefined> =>
    new Promise((ok, fail) => {
      const q = db.transaction("keys").objectStore("keys").get("static");
      q.onsuccess = () => ok(q.result as T | undefined);
      q.onerror = () => fail(q.error);
    });
  const have = await get<StaticKey>();
  if (have && have.priv && have.pub) return have;
  const k = await newStaticKey(false);
  await new Promise<void>((ok, fail) => {
    const tx = db.transaction("keys", "readwrite");
    tx.objectStore("keys").put(k, "static");
    tx.oncomplete = () => ok();
    tx.onerror = () => fail(tx.error);
  });
  return k;
}

const NativeWS = WebSocket;
const nativeFetch = globalThis.fetch.bind(globalThis);

/** wire opens the relay WebSocket of the room. */
function wire(c: Conf): Promise<Wire> {
  return new Promise((ok, fail) => {
    const scheme = location.protocol === "https:" ? "wss:" : "ws:";
    const ws = new NativeWS(scheme + "//" + location.host + "/relay/v1/" + c.room + "?side=page");
    ws.binaryType = "arraybuffer";
    const w: Wire = {
      send: (b) => ws.send(new Uint8Array(b)),
      close: () => ws.close(),
      onmessage: null,
      onclose: null,
    };
    ws.onopen = () => ok(w);
    ws.onerror = () => fail(new Error("relay: no connection"));
    ws.onmessage = (e: MessageEvent) => {
      if (e.data instanceof ArrayBuffer) w.onmessage?.(new Uint8Array(e.data));
    };
    ws.onclose = () => w.onclose?.();
  });
}

let current: Promise<Tunnel> | null = null;

/** tunnel is the open tunnel, or a new one when the last one ended. */
function tunnel(): Promise<Tunnel> {
  const c = conf;
  if (!c) return Promise.reject(new Error("relay: not paired"));
  if (!current) {
    const p = (async () => {
      const t = await Tunnel.open(await wire(c), await staticKey(), unb64url(c.k), pair ? { pair } : {});
      t.onclose = () => {
        if (current === p) current = null;
      };
      return t;
    })();
    current = p;
    p.catch(() => {
      if (current === p) current = null;
    });
  }
  return current;
}

if (conf) {
  globalThis.fetch = (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const u = new URL(input instanceof Request ? input.url : String(input), location.href);
    if (u.origin !== location.origin || !OURS.test(u.pathname)) return nativeFetch(input, init);
    return tunnel().then((t) => tunnelFetch(t, u, init));
  };
  const RelaySocket = makeSocketClass(tunnel);
  globalThis.WebSocket = new Proxy(NativeWS, {
    construct(target, args: [string | URL, (string | string[])?]) {
      const u = new URL(String(args[0]), location.href);
      const own = (u.protocol === "ws:" || u.protocol === "wss:") && u.host === location.host && !u.pathname.startsWith("/relay/");
      return own ? new RelaySocket(args[0], args[1]) : new target(args[0], args[1]);
    },
  });
}

/** signIn: a paired phone signs in with its relay key alone; with
 * web_require_totp the panel's code step takes the ticket on. */
async function signIn(): Promise<void> {
  if (!conf || pair) return;
  try {
    const r = await globalThis.fetch("/login/relay", { method: "POST", headers: { "Content-Type": "application/json" }, body: "{}" });
    keepRelaySignIn(r.ok, (await r.json()) as object, sessionStorage);
  } catch {
    // not paired, the laptop is away or the relay is down: the panel says so
  }
}

/** loadApp starts the panel by its SRI hash (relay.html's meta). */
function loadApp(): void {
  const sri = document.querySelector('meta[name="termocode-app"]')?.getAttribute("content") ?? "";
  const s = document.createElement("script");
  s.type = "module";
  s.integrity = sri;
  const tt = (globalThis as unknown as { trustedTypes?: { createPolicy(n: string, p: { createScriptURL(u: string): string }): { createScriptURL(u: string): unknown } } }).trustedTypes;
  const only = "/assets/app.js";
  const url = tt ? tt.createPolicy("relay", { createScriptURL: (u) => (u === only ? u : "") }).createScriptURL(only) : only;
  (s as unknown as { src: unknown }).src = url;
  document.head.appendChild(s);
}

void signIn().finally(loadApp);
