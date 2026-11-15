// radial.js: the floating radial menu (the package in
// web/packages/radial-menu, RM-2) with termocode's own actions, built
// alone with its stylesheet (radial.css). The page loads it with its one
// dynamic import, in lib/radial.ts, on a touch phone or tablet only, so the
// page's first files and the desktop never carry it. It imports no value
// of the page: the page gives it its ops (radial/actions.ts RadialHost).
// The favourites and the button's spot stay on this device.
import "../../../../web/packages/radial-menu/src/radial-menu.css";
import { mount } from "../../../../web/packages/radial-menu/src/fab";
import { categories, DEFAULT_FAVORITES, type RadialHost } from "./radial/actions";

export type { RadialHost } from "./radial/actions";
import { MoreList as List } from "./radial/morelist";
import "./radial/morelist.css";

// The favourites and the button's spot: no secret, this device only.
const FAVS = "termocode.radial.favorites";
const POS = "termocode.radial.position";

export function read<T>(key: string, ok: (v: unknown) => v is T): T | null {
  try {
    const v: unknown = JSON.parse(localStorage.getItem(key) ?? "null");
    return ok(v) ? v : null;
  } catch {
    return null;
  }
}

export function write(key: string, v: unknown): void {
  try {
    localStorage.setItem(key, JSON.stringify(v));
  } catch {
    /* private mode or full: the radial still works, it just forgets */
  }
}

export const isIds = (v: unknown): v is string[] => Array.isArray(v) && v.length <= 8 && v.every((x) => typeof x === "string" && x.length < 64);
const isPt = (v: unknown): v is [number, number] => Array.isArray(v) && v.length === 2 && v.every((x) => typeof x === "number" && Number.isFinite(x));

/** reserve is the room the page keeps for itself: at the bottom the
 * tab bar, a composer, the terminal's key bar and Follow chip, the
 * attention pill, the Home's box, the Git page's commit bar or the
 * Tactile keyboard, whichever reaches highest; at a side the tablet's
 * rail (on the right in a right-to-left page). */
function reserve(): { bottom: number; left: number; right: number } {
  const vh = innerHeight, vw = innerWidth;
  let top = vh, left = 0, right = 0;
  for (const el of document.querySelectorAll(".tabbar, .composer, .keybar, .term-follow, .pill-wrap, .home-foot, .tk-dock, .gp-dock")) {
    const r = el.getBoundingClientRect();
    if (r.height > 0 && r.top > vh / 2) top = Math.min(top, r.top);
  }
  for (const el of document.querySelectorAll(".rail")) {
    const r = el.getBoundingClientRect();
    if (r.width > 0 && r.width < vw / 2) {
      if (r.left > vw / 2) right = Math.max(right, vw - r.left);
      else left = Math.max(left, r.right);
    }
  }
  return { bottom: vh - top, left, right };
}

/** sheet links radial.css once; the button waits for it (no unstyled flash). */
let sheet: Promise<void> | null = null;
function linkSheet(): Promise<void> {
  return (sheet ??= new Promise<void>((ok, no) => {
    const l = document.createElement("link");
    l.rel = "stylesheet";
    l.href = "/assets/radial.css";
    l.onload = () => ok();
    l.onerror = () => {
      sheet = null;
      l.remove();
      no(new Error("radial.css"));
    };
    document.head.appendChild(l);
  }));
}

/** MoreList is the More list (RM-3); it links radial.css, which holds
 * its look, on a phone without the radial too. */
export function MoreList(p: { host: RadialHost }) {
  linkSheet().catch(() => undefined);
  return List(p);
}

/** init puts the floating button on the page once its stylesheet is in;
 * it returns its removal. */
export function init(host: RadialHost): () => void {
  let stop: (() => void) | null = null, gone = false;
  linkSheet().then(
    () => {
      if (!gone) stop = show(host);
    },
    () => undefined,
  );
  return () => {
    gone = true;
    stop?.();
  };
}

function show(host: RadialHost): () => void {
  const fab = mount(document.body, {
    categories: categories(host, host.layout.peek()),
    favorites: read(FAVS, isIds) ?? DEFAULT_FAVORITES,
    onFavoritesChange: (ids) => write(FAVS, ids),
    position: read(POS, isPt),
    onMove: (p) => write(POS, p),
    extraInsets: reserve,
  });
  // Over the page and its bars, under its sheets and cards.
  fab.root.style.setProperty("--rm-z", "35");
  let raf = 0;
  const again = (): void => {
    cancelAnimationFrame(raf);
    raf = requestAnimationFrame(() => fab.relayout());
  };
  // Bars come and go without a route change (the terminal's key bar, the
  // keyboard dock): look twice a second, move only when they changed.
  let last = "";
  const watch = setInterval(() => {
    const now = JSON.stringify(reserve());
    if (now !== last) {
      last = now;
      if (!fab.dragging) fab.relayout();
    }
  }, 500);
  // A sheet or a card over the page hides the button (it would sit on
  // the sheet's own buttons): looked at whenever the page's tree changes.
  const sheets = new MutationObserver(() => {
    const on = !!document.querySelector(".sheet-layer");
    fab.root.style.setProperty("display", on ? "none" : "");
    if (on && fab.isOpen) fab.close();
  });
  sheets.observe(document.body, { childList: true, subtree: true });
  const offs = [
    host.route.subscribe(again),
    host.layout.subscribe((l) => {
      fab.setCategories(categories(host, l));
      again();
    }),
  ];
  return () => {
    offs.forEach((f) => f());
    cancelAnimationFrame(raf);
    clearInterval(watch);
    sheets.disconnect();
    fab.destroy();
  };
}
