// Passkeys in the page (WEB-33): the gateway sends WebAuthn options as
// JSON (binary fields as base64url); the browser wants ArrayBuffers in
// and gives ArrayBuffers back. This file converts both ways and calls
// navigator.credentials; it never fetches (net/auth.ts does) and never
// keeps anything.

type Obj = Record<string, unknown>;

/** passkeysHere: this browser can make and use passkeys on this page: a
 * secure context (https on the tailnet, or localhost) on a host name; a
 * passkey cannot belong to an IP address such as 127.0.0.1. */
export function passkeysHere(): boolean {
  const ipHost = /^[0-9.]+$|^\[.*\]$/.test(location.hostname);
  return !ipHost && window.isSecureContext && typeof window.PublicKeyCredential === "function" && !!navigator.credentials;
}

function isObj(v: unknown): v is Obj {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function str(o: Obj, k: string): string {
  const v = o[k];
  if (typeof v !== "string") throw new Error("passkey options: " + k);
  return v;
}

function fromB64url(s: string): ArrayBuffer {
  const b64 = s.replace(/-/g, "+").replace(/_/g, "/") + "===".slice((s.length + 3) % 4);
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out.buffer;
}

function toB64url(b: ArrayBuffer): string {
  const bytes = new Uint8Array(b);
  let bin = "";
  for (const x of bytes) bin += String.fromCharCode(x);
  return btoa(bin).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

function descriptors(v: unknown): PublicKeyCredentialDescriptor[] | undefined {
  if (!Array.isArray(v)) return undefined;
  const out: PublicKeyCredentialDescriptor[] = [];
  for (const d of v) {
    if (isObj(d) && typeof d.id === "string") out.push({ type: "public-key", id: fromB64url(d.id) });
  }
  return out;
}

function publicKey(options: unknown): Obj {
  if (!isObj(options) || !isObj(options.publicKey)) throw new Error("passkey options");
  return options.publicKey;
}

function num(o: Obj, k: string): number | undefined {
  const v = o[k];
  return typeof v === "number" ? v : undefined;
}

/** createPasskey makes a passkey from the gateway's creation options and
 * returns the answer as JSON for /pair/finish. It throws when the user
 * cancels or the browser refuses. */
export async function createPasskey(options: unknown): Promise<Obj> {
  const pk = publicKey(options);
  const rp = pk.rp;
  const user = pk.user;
  if (!isObj(rp) || !isObj(user) || !Array.isArray(pk.pubKeyCredParams)) throw new Error("passkey options");
  const params: PublicKeyCredentialParameters[] = [];
  for (const p of pk.pubKeyCredParams) {
    if (isObj(p) && typeof p.alg === "number") params.push({ type: "public-key", alg: p.alg });
  }
  const sel = isObj(pk.authenticatorSelection) ? pk.authenticatorSelection : {};
  const publicKeyOpts: PublicKeyCredentialCreationOptions = {
    challenge: fromB64url(str(pk, "challenge")),
    rp: { id: str(rp, "id"), name: str(rp, "name") },
    user: { id: fromB64url(str(user, "id")), name: str(user, "name"), displayName: str(user, "displayName") },
    pubKeyCredParams: params,
    timeout: num(pk, "timeout"),
    excludeCredentials: descriptors(pk.excludeCredentials),
    authenticatorSelection: {
      residentKey: "required",
      requireResidentKey: true,
      userVerification: "required",
      authenticatorAttachment: sel.authenticatorAttachment === "platform" || sel.authenticatorAttachment === "cross-platform" ? sel.authenticatorAttachment : undefined,
    },
    attestation: "none",
  };
  const cred = await navigator.credentials.create({ publicKey: publicKeyOpts });
  if (!(cred instanceof PublicKeyCredential)) throw new Error("no passkey");
  const res = cred.response;
  if (!(res instanceof AuthenticatorAttestationResponse)) throw new Error("no passkey");
  return {
    id: cred.id,
    rawId: toB64url(cred.rawId),
    type: cred.type,
    response: {
      clientDataJSON: toB64url(res.clientDataJSON),
      attestationObject: toB64url(res.attestationObject),
      transports: typeof res.getTransports === "function" ? res.getTransports() : [],
    },
    clientExtensionResults: {},
  };
}

/** usePasskey signs in with any passkey of this site from the gateway's
 * request options and returns the answer as JSON for /login/finish. */
export async function usePasskey(options: unknown): Promise<Obj> {
  const pk = publicKey(options);
  const publicKeyOpts: PublicKeyCredentialRequestOptions = {
    challenge: fromB64url(str(pk, "challenge")),
    rpId: typeof pk.rpId === "string" ? pk.rpId : undefined,
    timeout: num(pk, "timeout"),
    allowCredentials: descriptors(pk.allowCredentials),
    userVerification: "required",
  };
  const cred = await navigator.credentials.get({ publicKey: publicKeyOpts });
  if (!(cred instanceof PublicKeyCredential)) throw new Error("no passkey");
  const res = cred.response;
  if (!(res instanceof AuthenticatorAssertionResponse)) throw new Error("no passkey");
  return {
    id: cred.id,
    rawId: toB64url(cred.rawId),
    type: cred.type,
    response: {
      clientDataJSON: toB64url(res.clientDataJSON),
      authenticatorData: toB64url(res.authenticatorData),
      signature: toB64url(res.signature),
      userHandle: res.userHandle ? toB64url(res.userHandle) : null,
    },
    clientExtensionResults: {},
  };
}

/** deviceName names this browser for the laptop's card and device list:
 * "iPhone · Safari", "Android · Chrome", "Mac · Firefox". */
export function deviceName(ua: string = navigator.userAgent): string {
  const os = /iPhone/.test(ua)
    ? "iPhone"
    : /iPad/.test(ua) || (/Macintosh/.test(ua) && navigator.maxTouchPoints > 1)
      ? "iPad"
      : /Android/.test(ua)
        ? "Android"
        : /Mac OS X|Macintosh/.test(ua)
          ? "Mac"
          : /Windows/.test(ua)
            ? "Windows"
            : /Linux/.test(ua)
              ? "Linux"
              : "Device";
  const browser = /Edg\//.test(ua)
    ? "Edge"
    : /Firefox\/|FxiOS/.test(ua)
      ? "Firefox"
      : /Chrome\/|CriOS/.test(ua)
        ? "Chrome"
        : /Safari\//.test(ua)
          ? "Safari"
          : "Browser";
  return os + " · " + browser;
}
