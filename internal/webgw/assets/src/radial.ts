// radial.js: the floating radial menu (the package in
// web/packages/radial-menu, RM-2) with termocode's own actions, built
// alone with its stylesheet (radial.css). The page loads both with one
// dynamic import (lib/radial.ts) on a touch phone or tablet only, so the
// page's first files and the desktop never carry it. It imports no value
// of the page: the page gives it its ops (radial/actions.ts RadialHost).
// The favourites and the button's spot stay on this device.
import "../../../../web/packages/radial-menu/src/radial-menu.css";
import { mount } from "../../../../web/packages/radial-menu/src/fab";
import { categories, DEFAULT_FAVORITES, type RadialHost } from "./radial/actions";

export type { RadialHost } from "./radial/actions";

const FAVS = "termocode.radial.favorites";
const POS = "termocode.radial.position";

function read<T>(key: string, ok: (v: unknown) => v is T): T | null {
  try {
    const v: unknown = JSON.parse(localStorage.getItem(key) ?? "null");
    return ok(v) ? v : null;
  } catch {
    return null;
  }
}

function write(key: string, v: unknown): void {
  try {
    localStorage.setItem(key, JSON.stringify(v));
  } catch {
    /* private mode or full: the radial still works, it just forgets */
  }
}

const isIds = (v: unknown): v is string[] => Array.isArray(v) && v.length <= 8 && v.every((x) => typeof x === "string" && x.length < 64);
const isPt = (v: unknown): v is [number, number] => Array.isArray(v) && v.length === 2 && v.every((x) => typeof x === "number" && Number.isFinite(x));

/** reserve is the room at the bottom the page keeps for itself: the tab
 * bar, a composer or the Home's box, whichever reaches highest. */
function reserve(): number {
  const vh = innerHeight;
  let top = vh;
  for (const el of document.querySelectorAll(".tabbar, .composer, .home-foot")) {
    const r = el.getBoundingClientRect();
    if (r.height > 0 && r.top > vh / 2) top = Math.min(top, r.top);
  }
  return vh - top;
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
    extraInsets: () => ({ bottom: reserve() }),
  });
  // Over the page and its bars, under its sheets and cards.
  fab.root.style.setProperty("--rm-z", "35");
  let raf = 0;
  const again = (): void => {
    cancelAnimationFrame(raf);
    raf = requestAnimationFrame(() => fab.relayout());
  };
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
    fab.destroy();
  };
}
