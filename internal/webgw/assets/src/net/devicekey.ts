// The bookmark key of a remembered browser (WEBP-12). A paired device that
// ticked "Remember this browser" keeps one key here, in localStorage of
// this one origin, so a bookmark opens the panel without the passkey. The
// key is sent only in the body of POST /login/device (to sign in; the
// answer brings the next key) and /login/forget; it never goes into the
// address bar, the back list or a log. Tabs take turns (Web Locks): two tabs that
// opened at once must not both use one key, or the second use would look
// like a stolen copy and end the bookmark.
import { signal } from "@preact/signals";
import { postJSON, type Reply } from "./auth";

const KEY = "termocode.device";
const LOCK = "termocode.device";
const SHAPE = /^tcb1\.dev-[0-9a-f]{8}\.[A-Za-z0-9_-]{43}$/;

/** savedDeviceKey is this browser's bookmark key, or null. */
export function savedDeviceKey(): string | null {
  try {
    const v = localStorage.getItem(KEY);
    return v !== null && SHAPE.test(v) ? v : null;
  } catch {
    return null;
  }
}

/** remembered is true while this browser keeps a bookmark key. */
export const remembered = signal(savedDeviceKey() !== null);

/** keepDeviceKey keeps the key of a sign-in or pairing reply, if it has one. */
export function keepDeviceKey(r: Reply): void {
  const v = r.data.device_key;
  if (r.status !== 200 || typeof v !== "string" || !SHAPE.test(v)) return;
  try {
    localStorage.setItem(KEY, v);
    remembered.value = true;
  } catch {
    // No storage: no bookmark sign-in, the passkey still works.
  }
}

function dropKey(): void {
  try {
    localStorage.removeItem(KEY);
  } catch {
    // Nothing kept.
  }
  remembered.value = false;
}

async function oneAtATime<T>(run: () => Promise<T>): Promise<T> {
  const locks = typeof navigator !== "undefined" ? navigator.locks : undefined;
  return locks ? locks.request(LOCK, run) : run();
}

/** useDeviceKey signs in with the kept key: the reply, or null when no
 * key is kept. A refused key is dropped; a network or busy answer keeps
 * it for the next try. */
export function useDeviceKey(): Promise<Reply | null> {
  return oneAtATime(async () => {
    const key = savedDeviceKey();
    if (key === null) return null;
    const r = await postJSON("/login/device", { key });
    if (r.status === 200) keepDeviceKey(r);
    else if (r.status === 400 || r.status === 401) dropKey();
    return r;
  });
}

/** forgetThisBrowser ends the bookmark here and on the laptop. */
export function forgetThisBrowser(): Promise<void> {
  return oneAtATime(async () => {
    const key = savedDeviceKey();
    dropKey();
    if (key !== null) await postJSON("/login/forget", { key });
  });
}
