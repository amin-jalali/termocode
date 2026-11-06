// Bookmark sign-in of a remembered browser (WEBP-12). A paired device that
// ticked "Remember this browser" gets a bookmark key from the laptop as an
// httpOnly, SameSite=Strict cookie, sent only to /login/ paths: page code
// never sees the key, so a script on the page cannot copy it. Here we keep
// only a "remembered" mark (no secret), so the page knows to try
// POST /login/device when it opens; the answer sets the next key. Tabs
// take turns (Web Locks): two tabs that opened at once must not both use
// one key, or the second use would look like a stolen copy and end the
// bookmark.
//
// Pages from before the cookie kept the key itself in localStorage under
// OLD_KEY. It is sent once in the body, the laptop moves it into the
// cookie, and the copy here is deleted.
import { signal } from "@preact/signals";
import { postJSON, type Reply } from "./auth";

const OLD_KEY = "termocode.device";
const MARK = "termocode.remembered";
const LOCK = "termocode.device";
const SHAPE = /^tcb1\.dev-[0-9a-f]{8}\.[A-Za-z0-9_-]{43}$/;

function oldKey(): string | null {
  try {
    const v = localStorage.getItem(OLD_KEY);
    return v !== null && SHAPE.test(v) ? v : null;
  } catch {
    return null;
  }
}

function dropOldKey(): void {
  try {
    localStorage.removeItem(OLD_KEY);
  } catch {
    // Nothing kept.
  }
}

function marked(): boolean {
  try {
    return localStorage.getItem(MARK) === "1";
  } catch {
    return false;
  }
}

function mark(on: boolean): void {
  try {
    if (on) localStorage.setItem(MARK, "1");
    else localStorage.removeItem(MARK);
  } catch {
    // No storage: the cookie still works, the page just does not know.
  }
  remembered.value = on;
}

/** isRemembered: this browser may hold a bookmark cookie (or an old key). */
export function isRemembered(): boolean {
  return marked() || oldKey() !== null;
}

/** remembered is true while this browser keeps a bookmark key. */
export const remembered = signal(isRemembered());

/** keepDeviceKey notes a sign-in or pairing reply that set the cookie; an
 * old key here is then stale (the laptop moved on) and goes. */
export function keepDeviceKey(r: Reply): void {
  if (r.status !== 200 || r.data.remembered !== true) return;
  dropOldKey();
  mark(true);
}

async function oneAtATime<T>(run: () => Promise<T>): Promise<T> {
  const locks = typeof navigator !== "undefined" ? navigator.locks : undefined;
  return locks ? locks.request(LOCK, run) : run();
}

/** useDeviceKey signs in with the kept key: the reply, or null when this
 * browser is not remembered. A refused key is dropped; a network or busy
 * answer keeps it for the next try. */
export function useDeviceKey(): Promise<Reply | null> {
  return oneAtATime(async () => {
    const old = oldKey();
    if (old === null && !marked()) return null;
    const r = await postJSON("/login/device", old !== null ? { key: old } : {});
    if (r.status === 200) keepDeviceKey(r); // an old key is now in the cookie
    else if (r.status === 400 || r.status === 401) {
      dropOldKey();
      mark(false);
    }
    return r;
  });
}

/** forgetThisBrowser ends the bookmark here and on the laptop. */
export function forgetThisBrowser(): Promise<void> {
  return oneAtATime(async () => {
    const old = oldKey();
    dropOldKey();
    mark(false);
    await postJSON("/login/forget", old !== null ? { key: old } : {});
  });
}
