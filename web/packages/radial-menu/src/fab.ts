// The floating button (radial_menu.dart RadialMenuButton): the collapsed
// hub, 64 px, by default in the bottom-right corner. Tap it to open the
// menu where the button is; hold it to pick it up (a blue ring spins
// around it) and drag it anywhere. Dropped past an edge it springs back
// to the nearest spot on screen. The open menu sits over an invisible
// tap catcher (no dark scrim: the page stays visible); a tap outside
// sucks the menu back into the button.
import { FAB_SIZE, clampFab, defaultFab, placeMenu, type Insets, type Pt } from "./geometry";
import { iconSVG } from "./icons";
import { RadialMenu, STRINGS, type Category, type Clock, type Strings, type Theme } from "./menu";
import { DUR, SLOP, springAt } from "./motion";

export interface MountOptions {
  categories: Category[];
  /** ids of the favourite items (the SAVED ring), in order */
  favorites?: string[];
  onFavoritesChange?(ids: string[]): void;
  /** the button's saved centre (page px); null: the default corner */
  position?: Pt | null;
  /** called with the resting centre after every drop */
  onMove?(p: Pt): void;
  onOpen?(): void;
  onClose?(): void;
  theme?: Theme;
  strings?: Partial<Strings>;
  /** button diameter, default 64 */
  size?: number;
  reducedMotion?: boolean;
  /** the screen's safe areas; default read from env(safe-area-inset-*) */
  insets?: () => Insets;
  /** the visible layer (shrinks with an on-screen keyboard); default the
   * window and its visual viewport */
  viewport?: () => { w: number; h: number };
  clock?: Clock;
  doc?: Document;
  win?: Window;
}

export interface RadialFab {
  readonly root: HTMLElement;
  readonly button: HTMLElement;
  /** the open menu, or null */
  readonly menu: RadialMenu | null;
  readonly isOpen: boolean;
  readonly dragging: boolean;
  /** the button's centre now (clamped to the layer) */
  readonly position: Pt;
  open(): void;
  close(): void;
  setCategories(c: Category[]): void;
  setFavorites(ids: string[]): void;
  setPosition(p: Pt | null): void;
  /** re-reads the layer (the host calls it after a layout change) */
  relayout(): void;
  destroy(): void;
}

/** hubMarkup is the closed hub drawn at d px (_ClosedHubPainter): the
 * dark disc with its accent ring, the hairline inner ring, the ✦ and
 * "menu". */
function hubMarkup(d: number, menu: string): string {
  const r = d / 2;
  const ring = Math.max(1.2, (1.3 * d) / 100);
  return (
    `<svg class="rm-fab-hub" viewBox="0 0 ${d} ${d}" width="${d}" height="${d}" aria-hidden="true">` +
    `<circle cx="${r}" cy="${r}" r="${r - 2}" fill="#0C0C11" stroke="var(--rm-accent-dim, #0E639C)" stroke-width="${ring}"/>` +
    `<circle cx="${r}" cy="${r}" r="${r - 2 - d * 0.08}" fill="none" stroke="rgba(255,255,255,.08)" stroke-width="1"/>` +
    `<g transform="translate(${r} ${r - d * 0.06}) scale(${(d / 100) * 1.5})">${iconSVG("ask", "var(--rm-accent, #007ACC)")}</g>` +
    `<text x="${r}" y="${r + d * 0.22}" class="rm-fab-text" font-size="${d * 0.085}" letter-spacing="${d * 0.02}" text-anchor="middle" dominant-baseline="central">${menu}</text>` +
    `</svg>`
  );
}

/** ringMarkup is the pick-up ring (_DragRingPainter): three 64° accent
 * arcs that spin, over a soft static glow. */
function ringMarkup(d: number): string {
  const s = d + 22, c = s / 2, r = s / 2 - 3;
  const arc = (a0: number): string => {
    const a1 = a0 + 64;
    const p = (a: number): string => `${c + r * Math.cos((a * Math.PI) / 180)},${c + r * Math.sin((a * Math.PI) / 180)}`;
    return `<path d="M${p(a0)} A${r},${r} 0 0 1 ${p(a1)}" fill="none" stroke="var(--rm-accent, #007ACC)" stroke-width="2.6" stroke-linecap="round"/>`;
  };
  return (
    `<svg class="rm-fab-ring" viewBox="0 0 ${s} ${s}" width="${s}" height="${s}" aria-hidden="true">` +
    `<circle cx="${c}" cy="${c}" r="${r}" fill="none" stroke="rgba(0,122,204,.2)" stroke-width="3" class="rm-fab-ring-glow"/>` +
    `<g class="rm-fab-spin">${arc(0)}${arc(120)}${arc(240)}</g></svg>`
  );
}

/** readInsets reads env(safe-area-inset-*) through a hidden probe. */
function probeInsets(doc: Document, root: HTMLElement, win: Window | undefined): () => Insets {
  const probe = doc.createElement("div");
  probe.className = "rm-probe";
  root.appendChild(probe);
  return () => {
    const cs = win?.getComputedStyle?.(probe);
    const n = (k: string): number => parseFloat(cs?.getPropertyValue(k) ?? "") || 0;
    return { top: n("padding-top"), right: n("padding-right"), bottom: n("padding-bottom"), left: n("padding-left") };
  };
}

const realClock = (win: Window | undefined): Clock => ({
  now: () => (typeof performance !== "undefined" ? performance.now() : Date.now()),
  setTimeout: (fn, ms) => (win ?? globalThis).setTimeout(fn, ms),
  clearTimeout: (id) => (win ?? globalThis).clearTimeout(id as number),
});

/** mount puts the floating button and its menu on the page, in el
 * (usually document.body): a full-screen layer whose empty area lets
 * every touch through to the page. */
export function mount(el: HTMLElement, opts: MountOptions): RadialFab {
  let o = opts;
  const doc = o.doc ?? document;
  const win = o.win ?? (typeof window === "undefined" ? undefined : window);
  const clock = o.clock ?? realClock(win);
  const size = o.size ?? FAB_SIZE;
  const s: Strings = { ...STRINGS, ...o.strings };
  const reduced = (): boolean => o.reducedMotion ?? !!win?.matchMedia?.("(prefers-reduced-motion: reduce)").matches;

  const root = doc.createElement("div");
  root.className = "rm-root";
  el.appendChild(root);
  const insets = o.insets ?? probeInsets(doc, root, win);
  const viewport =
    o.viewport ??
    ((): { w: number; h: number } => {
      const vv = win?.visualViewport;
      const ih = win?.innerHeight ?? 844;
      return { w: win?.innerWidth ?? 390, h: vv ? Math.min(ih, vv.offsetTop + vv.height) : ih };
    });
  const theme = (t: Theme | undefined): void => {
    if (t?.accent) root.style.setProperty("--rm-accent", t.accent);
    if (t?.accentDim) root.style.setProperty("--rm-accent-dim", t.accentDim);
    if (t?.font) root.style.setProperty("--rm-font", t.font);
  };
  theme(o.theme);

  const button = doc.createElement("button");
  button.className = "rm-fab";
  button.setAttribute("type", "button");
  button.setAttribute("aria-label", s.fabLabel);
  button.setAttribute("aria-haspopup", "menu");
  button.setAttribute("aria-expanded", "false");
  button.style.setProperty("width", size + "px");
  button.style.setProperty("height", size + "px");
  button.innerHTML = `<span class="rm-fab-glow" aria-hidden="true"></span><span class="rm-fab-body">${hubMarkup(size, s.menu)}</span>${ringMarkup(size)}`;
  root.appendChild(button);

  let saved: Pt | null = o.position ?? null; // the logical spot (null: default)
  let favs = [...(o.favorites ?? [])];
  let cats = o.categories;
  let menu: RadialMenu | null = null;
  let overlay: HTMLElement | null = null;
  let dragging = false;
  let destroyed = false;
  let spring: { from: Pt; to: Pt; t0: number } | null = null;
  let frame: number | null = null;

  const layer = (): { w: number; h: number } => viewport();
  /** resting is the logical spot clamped to the current layer: when the
   * keyboard shrinks the layer the button rides up, and drops back when
   * it closes (the logical spot is kept). */
  const resting = (): Pt => {
    const { w, h } = layer();
    const ins = insets();
    return clampFab(saved ?? defaultFab(w, h, ins, size), w, h, ins, size);
  };
  let shown: Pt = resting();

  const place = (p: Pt): void => {
    shown = p;
    button.style.setProperty("left", p[0] - size / 2 + "px");
    button.style.setProperty("top", p[1] - size / 2 + "px");
  };

  const breathe = (): void => {
    const vis = (doc as Document & { visibilityState?: string }).visibilityState;
    button.classList.toggle("rm-breathing", !menu && vis !== "hidden" && !reduced() && !destroyed);
  };

  const tick = (): void => {
    frame = null;
    if (!spring || destroyed) return;
    const v = Math.min(1, (clock.now() - spring.t0) / DUR.spring);
    place(springAt(spring.from, spring.to, v));
    if (v >= 1) {
      spring = null;
      place(resting());
      return;
    }
    frame = (win?.requestAnimationFrame?.(tick) as number | undefined) ?? null;
  };

  const relayout = (): void => {
    if (!dragging && !spring) place(resting());
  };

  // ── the button's gestures: tap opens, hold picks it up ─────────────────
  let press: { id: number; x: number; y: number; start: Pt; moved: boolean; timer: unknown } | null = null;

  const pickUp = (): void => {
    if (!press || menu) return;
    spring = null;
    dragging = true;
    saved = shown;
    press.start = shown;
    button.classList.add("rm-dragging");
  };

  const drop = (): void => {
    if (!dragging) return;
    dragging = false;
    button.classList.remove("rm-dragging");
    const from = shown;
    const { w, h } = layer();
    const to = clampFab(from, w, h, insets(), size);
    // The logical spot is the clamped one at once; the spring only moves
    // the button there.
    saved = to;
    o.onMove?.(to);
    if (Math.hypot(to[0] - from[0], to[1] - from[1]) > 0.5 && !reduced()) {
      spring = { from, to, t0: clock.now() };
      if (frame === null) frame = (win?.requestAnimationFrame?.(tick) as number | undefined) ?? null;
    } else place(to);
  };

  button.addEventListener("pointerdown", (ev) => {
    const e = ev as PointerEvent;
    if (menu || press) return;
    e.preventDefault();
    try {
      button.setPointerCapture?.(e.pointerId);
    } catch {
      /* a synthetic pointer */
    }
    press = { id: e.pointerId, x: e.clientX, y: e.clientY, start: shown, moved: false, timer: clock.setTimeout(pickUp, DUR.pickUp) };
  });
  button.addEventListener("pointermove", (ev) => {
    const e = ev as PointerEvent;
    if (!press || e.pointerId !== press.id) return;
    const dx = e.clientX - press.x, dy = e.clientY - press.y;
    if (dragging) {
      place([press.start[0] + dx, press.start[1] + dy]);
      return;
    }
    if (Math.hypot(dx, dy) > SLOP) {
      // A slide before the hold: neither a tap nor a pick-up.
      press.moved = true;
      clock.clearTimeout(press.timer);
    }
  });
  const release = (ev: Event, cancelled: boolean): void => {
    const e = ev as PointerEvent;
    if (!press || e.pointerId !== press.id) return;
    clock.clearTimeout(press.timer);
    const p = press;
    press = null;
    if (dragging) return drop();
    if (!cancelled && !p.moved) api.open();
  };
  button.addEventListener("pointerup", (e) => release(e, false));
  button.addEventListener("pointercancel", (e) => release(e, true));
  button.addEventListener("contextmenu", (e) => e.preventDefault());
  button.addEventListener("keydown", (ev) => {
    const e = ev as KeyboardEvent;
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      api.open();
    }
  });

  const onVis = (): void => breathe();
  doc.addEventListener?.("visibilitychange", onVis);
  win?.addEventListener?.("resize", relayout);
  win?.visualViewport?.addEventListener?.("resize", relayout);

  const closed = (): void => {
    menu?.destroy();
    menu = null;
    overlay?.remove();
    overlay = null;
    button.hidden = false;
    button.setAttribute("aria-expanded", "false");
    // The button comes back with its little entrance.
    button.classList.remove("rm-in");
    void button.getBoundingClientRect();
    if (!reduced()) button.classList.add("rm-in");
    breathe();
    relayout();
    o.onClose?.();
    button.focus?.();
  };

  const api: RadialFab = {
    root,
    button,
    get menu() {
      return menu;
    },
    get isOpen() {
      return menu !== null;
    },
    get dragging() {
      return dragging;
    },
    get position() {
      return shown;
    },
    open() {
      if (menu || dragging || destroyed) return;
      const { w, h } = layer();
      const ins = insets();
      const box = placeMenu(shown, w, h, ins);
      overlay = doc.createElement("div");
      overlay.className = "rm-overlay";
      const stage = doc.createElement("div");
      stage.style.setProperty("left", box.left + "px");
      stage.style.setProperty("top", box.top + "px");
      stage.style.setProperty("width", box.side + "px");
      stage.style.setProperty("height", box.side + "px");
      overlay.appendChild(stage);
      root.appendChild(overlay);
      // The invisible catcher: a tap that misses the menu closes it.
      let down: { id: number; x: number; y: number } | null = null;
      overlay.addEventListener("pointerdown", (ev) => {
        const e = ev as PointerEvent;
        down = { id: e.pointerId, x: e.clientX, y: e.clientY };
      });
      overlay.addEventListener("pointerup", (ev) => {
        const e = ev as PointerEvent;
        const d = down;
        down = null;
        if (d && d.id === e.pointerId && Math.hypot(e.clientX - d.x, e.clientY - d.y) <= SLOP) menu?.requestClose();
      });
      overlay.addEventListener("keydown", (ev) => {
        if ((ev as KeyboardEvent).key === "Escape") menu?.requestClose();
      });
      menu = new RadialMenu(stage, {
        categories: cats,
        favorites: favs,
        onFavoritesChange: (ids) => {
          favs = ids;
          o.onFavoritesChange?.(ids);
        },
        strings: o.strings,
        theme: o.theme,
        startOpen: true,
        onClosed: closed,
        origin: [box.ox, box.oy],
        reducedMotion: reduced(),
        insets,
        clock,
        doc,
        win,
      });
      menu.box = { left: box.left, top: box.top, side: box.side };
      button.hidden = true;
      button.setAttribute("aria-expanded", "true");
      breathe();
      o.onOpen?.();
      // Focus the first action so a keyboard can go on from here.
      (stage.querySelector?.(".rm-act") as HTMLElement | null)?.focus?.();
    },
    close() {
      menu?.requestClose();
    },
    setCategories(c) {
      cats = c;
      menu?.setCategories(c);
    },
    setFavorites(ids) {
      favs = [...ids];
      menu?.setFavorites(ids);
    },
    setPosition(p) {
      saved = p;
      relayout();
    },
    relayout,
    destroy() {
      destroyed = true;
      if (press) clock.clearTimeout(press.timer);
      if (frame !== null) win?.cancelAnimationFrame?.(frame);
      menu?.destroy();
      doc.removeEventListener?.("visibilitychange", onVis);
      win?.removeEventListener?.("resize", relayout);
      win?.visualViewport?.removeEventListener?.("resize", relayout);
      root.remove();
    },
  };

  place(shown);
  if (!reduced()) button.classList.add("rm-in");
  breathe();
  return api;
}
