// Port preview from the page (WEB-54). A link to localhost is a port of
// the computer termocode runs on, not of this device: it never opens as
// it is. Forward port sends web.forward with the port only (no host: the
// target is always 127.0.0.1 there); the hub's card asks (in this page,
// or on the laptop). When the forward is open the hub sends fwd.ready to
// this page alone, with the address it built: the link sheet offers it,
// the link's path kept, as a real link (a tab opened later than the tap
// would be blocked). In SSH mode termocode ssh opens the tab itself.
import type { FwdReady } from "../../../../webproto/proto";
import { link } from "../session";
import { t } from "../strings";
import { errText } from "../ui/CardSheet";
import { linkAsk, linkReady } from "./links";
import { toast } from "./toasts";

/** loopbackPort is the port of an http(s) link to localhost (or
 * 127.x.x.x, [::1], 0.0.0.0), else 0. */
export function loopbackPort(href: string): number {
  try {
    const u = new URL(href);
    if (/^(localhost|.+\.localhost|\[::1\]|0\.0\.0\.0|127(\.\d{1,3}){3})$/i.test(u.hostname) && /^https?:$/.test(u.protocol)) {
      return Number(u.port) || (u.protocol === "https:" ? 443 : 80);
    }
  } catch {
    // not a URL
  }
  return 0;
}

/** wanted: the link each asked port came from, for its path. */
const wanted = new Map<number, string>();

/** askForward sends web.forward for the port of href. */
export function askForward(href: string): void {
  const port = loopbackPort(href);
  wanted.set(port, href);
  link.request("web.forward", { port }).catch((e: unknown) => toast("warn", errText(e)));
}

link.onEvent((kind, data) => {
  if (kind !== "fwd.ready") return;
  const d = data as FwdReady;
  if (d.mode === "ssh") {
    toast("info", t("fwd.ssh", { port: String(d.port) }));
    return;
  }
  // The hub's address keeps its origin: only the link's path, query and
  // fragment are set on it.
  try {
    const u = new URL(d.url ?? "");
    const from = wanted.get(d.port);
    if (from) {
      const f = new URL(from);
      u.pathname = f.pathname;
      u.search = f.search;
      u.hash = f.hash;
    }
    if (!/^https?:$/.test(u.protocol)) return;
    linkReady.value = true;
    linkAsk.value = u.href;
  } catch {
    // no address: nothing to open
  }
});
