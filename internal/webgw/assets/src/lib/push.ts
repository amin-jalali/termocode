// Web Push (MOB-30). On an https site, "Notify me" on a paired device
// also asks the laptop to wake it when something waits while the page
// is closed: the page registers the push-only worker (/sw.js, served by
// the gateway) and hands the browser's subscription to the laptop
// (push.subscribe), which keeps it in its secret store. The push holds
// at most a count; the worker shows its own fixed text. "Stop" forgets
// the subscription on both sides and removes the worker, so nothing of
// the page stays in the browser. All of it is best effort: the plain
// notifications of lib/notify.ts work without it.
import { link } from "../session";
import { lang } from "../strings";

interface PushState {
  state: "ready" | "none" | "locked" | "off";
  key?: string;
  on: boolean;
}

/** container is the browser's service workers, on an https site with push only. */
function container(): ServiceWorkerContainer | undefined {
  if (!window.isSecureContext || location.protocol !== "https:") return undefined;
  const c = navigator.serviceWorker as ServiceWorkerContainer | undefined;
  return c && typeof window.PushManager === "function" ? c : undefined;
}

/** canPush says whether this browser can get pushes here. */
export function canPush(): boolean {
  return container() !== undefined;
}

/** bytes decodes base64url (the laptop's public key). */
function bytes(b64: string): Uint8Array<ArrayBuffer> {
  const s = atob(b64.replace(/-/g, "+").replace(/_/g, "/") + "===".slice((b64.length + 3) % 4));
  const out = new Uint8Array(new ArrayBuffer(s.length));
  for (let i = 0; i < s.length; i++) out[i] = s.charCodeAt(i);
  return out;
}

function same(a: ArrayBuffer | null, b: Uint8Array): boolean {
  if (!a || a.byteLength !== b.length) return false;
  const x = new Uint8Array(a);
  return x.every((v, i) => v === b[i]);
}

/** pushOn runs after the user turned notifications on; true: pushes come. */
export async function pushOn(): Promise<boolean> {
  const c = container();
  if (!c) return false;
  try {
    const st = (await link.request("push.key", { make: true })) as PushState;
    if (st.state !== "ready" || !st.key) return false;
    const reg = await c.register("/sw.js?l=" + lang.value, { scope: "/" }); // the worker speaks the page's language
    await c.ready;
    const key = bytes(st.key);
    let sub = await reg.pushManager.getSubscription();
    if (sub && !same(sub.options.applicationServerKey, key)) {
      await sub.unsubscribe(); // made for an older laptop key
      sub = null;
    }
    sub ??= await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: key });
    const j = sub.toJSON();
    if (!j.endpoint || !j.keys?.p256dh || !j.keys.auth) return false;
    await link.request("push.subscribe", { endpoint: j.endpoint, p256dh: j.keys.p256dh, auth: j.keys.auth });
    return true;
  } catch {
    return false; // refused, offline or an old browser: plain notifications only
  }
}

/** pushOff forgets the subscription here and on the laptop and removes the worker. */
export async function pushOff(): Promise<void> {
  const c = container();
  if (!c) return;
  link.request("push.unsubscribe").catch(() => undefined);
  try {
    const reg = await c.getRegistration("/");
    const sub = await reg?.pushManager.getSubscription();
    await sub?.unsubscribe();
    await reg?.unregister();
  } catch {
    // nothing to remove
  }
}
