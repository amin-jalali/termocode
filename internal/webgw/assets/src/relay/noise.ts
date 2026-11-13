// The page's side of the relay's end-to-end layer (ADR 0022, WEB-53):
// Noise_IK_25519_AESGCM_SHA256 as the initiator, with the browser's own
// WebCrypto only (X25519, AES-256-GCM, SHA-256, HMAC): no crypto library
// in the page. The gateway's side is internal/relaytun (flynn/noise); a
// Go test runs this file against it.

const PROTOCOL = "Noise_IK_25519_AESGCM_SHA256";
/** PROLOGUE binds both ends to this protocol and version. */
export const PROLOGUE = new TextEncoder().encode("termocode-relay-v1");
const X25519 = { name: "X25519" };
const subtle = (): SubtleCrypto => crypto.subtle;

/** StaticKey is the page's own key: the private half is not extractable. */
export interface StaticKey {
  priv: CryptoKey;
  pub: Uint8Array;
}

export function concat(...parts: Uint8Array[]): Uint8Array {
  let n = 0;
  for (const p of parts) n += p.length;
  const out = new Uint8Array(n);
  let o = 0;
  for (const p of parts) {
    out.set(p, o);
    o += p.length;
  }
  return out;
}

/** buf is a fresh ArrayBuffer of b, as WebCrypto takes it. */
function buf(b: Uint8Array): ArrayBuffer {
  const out = new ArrayBuffer(b.length);
  new Uint8Array(out).set(b);
  return out;
}

/** newStaticKey makes a key pair whose private half never leaves WebCrypto. */
export async function newStaticKey(extractable = false): Promise<StaticKey> {
  const kp = (await subtle().generateKey(X25519, extractable, ["deriveBits"])) as CryptoKeyPair;
  return { priv: kp.privateKey, pub: new Uint8Array(await subtle().exportKey("raw", kp.publicKey)) };
}

async function dh(priv: CryptoKey, pub: Uint8Array): Promise<Uint8Array> {
  const k = await subtle().importKey("raw", buf(pub), X25519, false, []);
  return new Uint8Array(await subtle().deriveBits({ name: "X25519", public: k } as EcdhKeyDeriveParams, priv, 256));
}

async function sha256(b: Uint8Array): Promise<Uint8Array> {
  return new Uint8Array(await subtle().digest("SHA-256", buf(b)));
}

async function hmac(key: Uint8Array, data: Uint8Array): Promise<Uint8Array> {
  const k = await subtle().importKey("raw", buf(key), { name: "HMAC", hash: "SHA-256" }, false, ["sign"]);
  return new Uint8Array(await subtle().sign("HMAC", k, buf(data)));
}

/** hkdf2 is Noise's HKDF with two outputs. */
async function hkdf2(ck: Uint8Array, ikm: Uint8Array): Promise<[Uint8Array, Uint8Array]> {
  const tk = await hmac(ck, ikm);
  const o1 = await hmac(tk, Uint8Array.of(1));
  const o2 = await hmac(tk, concat(o1, Uint8Array.of(2)));
  return [o1, o2];
}

/** CipherState is one direction's key and nonce. Calls must not overlap:
 * the nonce goes up by one per message, in order. */
export class CipherState {
  private n = 0;
  private constructor(private readonly key: CryptoKey | null) {}

  static async make(k: Uint8Array | null): Promise<CipherState> {
    if (k === null) return new CipherState(null);
    return new CipherState(await subtle().importKey("raw", buf(k), { name: "AES-GCM" }, false, ["encrypt", "decrypt"]));
  }

  get hasKey(): boolean {
    return this.key !== null;
  }

  private iv(): ArrayBuffer {
    const iv = new ArrayBuffer(12);
    const v = new DataView(iv);
    v.setUint32(4, Math.floor(this.n / 0x100000000));
    v.setUint32(8, this.n >>> 0);
    return iv;
  }

  async encrypt(ad: Uint8Array, pt: Uint8Array): Promise<Uint8Array> {
    if (!this.key) return pt;
    const ct = await subtle().encrypt({ name: "AES-GCM", iv: this.iv(), additionalData: buf(ad), tagLength: 128 }, this.key, buf(pt));
    this.n++;
    return new Uint8Array(ct);
  }

  async decrypt(ad: Uint8Array, ct: Uint8Array): Promise<Uint8Array> {
    if (!this.key) return ct;
    const pt = await subtle().decrypt({ name: "AES-GCM", iv: this.iv(), additionalData: buf(ad), tagLength: 128 }, this.key, buf(ct));
    this.n++;
    return new Uint8Array(pt);
  }
}

class Symmetric {
  ck: Uint8Array;
  h: Uint8Array;
  cs: CipherState | null = null;

  constructor() {
    const name = new TextEncoder().encode(PROTOCOL); // 28 bytes: padded to 32
    this.h = new Uint8Array(32);
    this.h.set(name);
    this.ck = this.h.slice();
  }

  async mixHash(data: Uint8Array): Promise<void> {
    this.h = await sha256(concat(this.h, data));
  }

  async mixKey(ikm: Uint8Array): Promise<void> {
    const [ck, k] = await hkdf2(this.ck, ikm);
    this.ck = ck;
    this.cs = await CipherState.make(k);
  }

  async encryptAndHash(pt: Uint8Array): Promise<Uint8Array> {
    const ct = this.cs ? await this.cs.encrypt(this.h, pt) : pt;
    await this.mixHash(ct);
    return ct;
  }

  async decryptAndHash(ct: Uint8Array): Promise<Uint8Array> {
    const pt = this.cs ? await this.cs.decrypt(this.h, ct) : ct;
    await this.mixHash(ct);
    return pt;
  }

  async split(): Promise<[CipherState, CipherState]> {
    const [k1, k2] = await hkdf2(this.ck, new Uint8Array(0));
    return [await CipherState.make(k1), await CipherState.make(k2)];
  }
}

/** Handshake is a finished handshake: the two directions and the
 * gateway's answer (its second payload). */
export interface Handshake {
  send: CipherState;
  recv: CipherState;
  reply: Uint8Array;
}

/** handshake runs the initiator's side of IK: s is the page's key, rs the
 * gateway's public key from the QR code, payload the hello. write sends
 * a message; read waits for the next one. A wrong gateway key or a
 * changed message throws. */
export async function handshake(
  write: (b: Uint8Array) => void,
  read: () => Promise<Uint8Array>,
  s: StaticKey,
  rs: Uint8Array,
  payload: Uint8Array,
): Promise<Handshake> {
  const ss = new Symmetric();
  await ss.mixHash(PROLOGUE);
  await ss.mixHash(rs);
  const e = await newStaticKey(false);
  // -> e, es, s, ss
  await ss.mixHash(e.pub);
  await ss.mixKey(await dh(e.priv, rs));
  const encS = await ss.encryptAndHash(s.pub);
  await ss.mixKey(await dh(s.priv, rs));
  const encP = await ss.encryptAndHash(payload);
  write(concat(e.pub, encS, encP));
  // <- e, ee, se
  const m2 = await read();
  if (m2.length < 32 + 16) throw new Error("relay: short handshake");
  const re = m2.slice(0, 32);
  await ss.mixHash(re);
  await ss.mixKey(await dh(e.priv, re));
  await ss.mixKey(await dh(s.priv, re));
  const reply = await ss.decryptAndHash(m2.slice(32));
  const [send, recv] = await ss.split();
  return { send, recv, reply };
}

/** b64url and unb64url carry keys as 43 base64url characters. */
export function b64url(b: Uint8Array): string {
  let s = "";
  for (const c of b) s += String.fromCharCode(c);
  return btoa(s).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

export function unb64url(s: string): Uint8Array {
  const t = atob(s.replace(/-/g, "+").replace(/_/g, "/"));
  const out = new Uint8Array(t.length);
  for (let i = 0; i < t.length; i++) out[i] = t.charCodeAt(i);
  return out;
}
