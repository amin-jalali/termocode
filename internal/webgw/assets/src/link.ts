// A link's confirm sheet, in link.js (WEB-54): the page loads it the
// first time a link is tapped (lib/links.ts), so the page's first
// files do not carry it. It imports no value of the page: Preact, the
// sheet, the link, the strings and the toasts come from the host (one
// copy of each in the page); only types are imported.
//
// The sheet shows the full URL in mono, Cancel first and focused, and
// Open as a real link to a new tab with no opener and no referrer. A
// link to localhost is a port of the computer termocode runs on, not of
// this device: it never opens as it is. Forward port (a controller only)
// sends web.forward with the port only (no host: the target is always
// 127.0.0.1 there); the hub's card asks (in this page, or on the
// laptop). When the forward is open the hub sends fwd.ready to this page
// alone, with the address it built: the sheet offers it, the link's path
// kept, as a real link (a tab opened later than the tap would be
// blocked). In SSH mode termocode ssh opens the tab itself.
import type { ComponentChildren, h as preactH, render as preactRender, VNode } from "preact";
import type { ReadonlySignal, Signal } from "@preact/signals";
import type { FwdReady, Meta } from "../../../webproto/proto";
import type { StringKey } from "./strings";

/** LinkHost is what the page gives this file. */
export interface LinkHost {
  h: typeof preactH;
  render: typeof preactRender;
  Sheet: (p: { onLater: () => void; children: ComponentChildren; label?: string }) => VNode | null;
  link: {
    meta: ReadonlySignal<Meta | null>;
    request(op: string, args?: unknown): Promise<unknown>;
    onEvent(fn: (kind: string, data: unknown) => void): () => void;
  };
  toast(severity: "info" | "warn", title: string): void;
  t(key: StringKey, vars?: Record<string, string | number>): string;
  /** ask: the URL whose sheet is open (null: none). */
  ask: Signal<string | null>;
}

let host: LinkHost;

// The port preview's own strings: they load with this chunk only, so the
// page's first files do not carry them; English only, like device.js.
// Same voice as strings.ts. {port} marks the port.
const FS = {
  title: "Port {port} of the computer",
  body: "This link is on the computer termocode runs on, not on this device.",
  forward: "Forward port",
  ssh: "Port {port} opens on your computer.",
};

const fs = (k: keyof typeof FS, port = 0) => FS[k].replace("{port}", String(port));

/** wanted: the link each asked port came from, for its path. */
const wanted = new Map<number, string>();

/** ready is the last forwarded address (fwd.ready): it opens as it is,
 * never a second forward. */
let ready = "";

/** init takes the page's host; lib/links.ts calls it once, first. */
export function init(hst: LinkHost): void {
  host = hst;
  // The sheet draws in a box of its own, whenever ask holds a URL.
  const box = document.body.appendChild(document.createElement("div"));
  hst.ask.subscribe((href) => hst.render(href ? hst.h(LinkSheet, { href }) : null, box));
  hst.link.onEvent((kind, data) => {
    if (kind !== "fwd.ready") return;
    const d = data as FwdReady;
    if (d.mode === "ssh") {
      hst.toast("info", fs("ssh", d.port));
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
      hst.ask.value = ready = u.href;
    } catch {
      // no address: nothing to open
    }
  });
}

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

/** askForward sends web.forward for the port of href. */
function askForward(href: string): void {
  const port = loopbackPort(href);
  wanted.set(port, href);
  host.link.request("web.forward", { port }).catch((e: unknown) => host.toast("warn", (e as Error).message || host.t("retry")));
}

export function LinkSheet({ href }: { href: string }) {
  const { h, t } = host;
  const close = () => {
    host.ask.value = null;
  };
  const port = href === ready ? 0 : loopbackPort(href);
  const controller = host.link.meta.value?.role === "controller";
  const title = port ? fs("title", port) : t("link.title");
  const yes = port
    ? controller &&
      h(
        "button",
        {
          type: "button",
          class: "btn btn-primary",
          onClick: () => {
            close();
            askForward(href);
          },
        },
        fs("forward"),
      )
    : h("a", { class: "btn btn-primary", href, target: "_blank", rel: "noopener noreferrer", onClick: close }, t("link.open"));
  const card = h(
    "section",
    { class: "card card-dialog link-ask", role: "dialog", "aria-modal": "true", "aria-labelledby": "link-ask-t" },
    h("header", { class: "card-head" }, h("h2", { class: "card-title", id: "link-ask-t" }, title)),
    h("pre", { class: "card-subject", dir: "ltr" }, href),
    h("p", { class: "card-body" }, port ? fs("body") : t("link.body")),
    h(
      "footer",
      { class: "card-foot" },
      h("div", { class: "choices" }, h("button", { type: "button", class: "btn btn-neutral", onClick: close }, t("cancel")), yes || null),
    ),
  );
  return h(host.Sheet, { onLater: close, label: title, children: card });
}
