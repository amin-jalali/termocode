// App key sign-in (MOB-27): inside the termocode Android app, and only
// when the laptop allows it (web_app_key_login), a key in the phone's
// keystore stands in for a passkey. The app's bridge signs; it builds the
// signed text itself with the WebView's real origin, so this page passes
// only the purpose and the laptop's nonce. The key never reaches page code.
import { postJSON, type Reply } from "./auth";

interface AppKeyBridge {
  appKey(): Promise<{ public_key: string }>;
  appKeySign(o: { purpose: "pair" | "login"; nonce: string }): Promise<{ signature: string }>;
}

function bridge(): AppKeyBridge | null {
  const cap = (window as unknown as { Capacitor?: { Plugins?: Record<string, unknown> } }).Capacitor;
  const p = cap?.Plugins?.TermocodeAppKey as Partial<AppKeyBridge> | undefined;
  return p && typeof p.appKey === "function" && typeof p.appKeySign === "function" ? (p as AppKeyBridge) : null;
}

/** inApp: this page runs inside the termocode app, with its key bridge. */
export function inApp(): boolean {
  return bridge() !== null;
}

const B64URL = /^[A-Za-z0-9_-]{1,200}$/;

async function signed(purpose: "pair" | "login", nonce: string): Promise<{ public_key: string; signature: string } | null> {
  const b = bridge();
  if (b === null) return null;
  try {
    const { public_key } = await b.appKey();
    const { signature } = await b.appKeySign({ purpose, nonce });
    if (!B64URL.test(public_key) || !B64URL.test(signature)) return null;
    return { public_key, signature };
  } catch {
    return null; // the user cancelled the phone's lock screen, or no key
  }
}

/** appKeyPair is the app key part of /pair/finish, or null. */
export async function appKeyPair(pairID: string): Promise<{ app_key: string; app_sig: string } | null> {
  const s = await signed("pair", pairID);
  return s && { app_key: s.public_key, app_sig: s.signature };
}

/** appKeyLogin signs in with the app key: the laptop's reply, or null
 * when there is no app, the laptop has it off, or the user cancelled. */
export async function appKeyLogin(): Promise<Reply | null> {
  if (!inApp()) return null;
  const start = await postJSON("/login/app/start", {});
  const id = start.data.login_id;
  const ch = start.data.challenge;
  if (start.status !== 200 || typeof id !== "string" || typeof ch !== "string") return null;
  const s = await signed("login", ch);
  if (s === null) return null;
  return postJSON("/login/app/finish", { login_id: id, public_key: s.public_key, signature: s.signature });
}
