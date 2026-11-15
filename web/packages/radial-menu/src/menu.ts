// The radial menu itself (radial_menu.dart RadialMenu): a centre hub, an
// inner ring of categories and an outer ring of the active category's
// actions, drawn as SVG in the 600 × 600 design space. Tap an action to
// run it; hold it 520 ms (a progress arc fills) for its details card with
// ★ favorite. One requestAnimationFrame loop paints every animation
// (entrance, rings, the clockwise bloom, the hold, the flash, the close)
// from a clock, so tests drive it with a fake clock and fake frames.
import { ACT_GAP, ACT_ICON_R, ACT_ICON_S, C, CAT_GAP, CAT_ICON_R, CAT_ICON_S, GAP_RING, HUB, HUB_RING, MAX_ITEMS, PAD, PROG_INSET, PROG_R, RI1, RI2, RO1, RO2, actCentre, arcLength, arcPath, catCentre, hitTest, placeTip, polar, sectorPath, toDesign, type Insets, type Hit } from "./geometry";
import { iconSVG } from "./icons";
import { DUR, SLOP, collapseFrame, entranceFrame, flashScale, holdScale, petalFrame, ringsFrame, sparkFrame } from "./motion";

/** Item is one action of a ring. */
export interface Item {
  id: string;
  label: string;
  icon: string;
  /** detail is the one line the details card shows. */
  detail?: string;
  /** run fires on a tap, after the menu starts to close. Without it the
   * tap shows "▸ run <label>" (the design's preview data). */
  run?: () => void;
}

/** Category is one wedge of the inner ring and its actions. */
export interface Category {
  id: string;
  label: string;
  icon: string;
  items: Item[];
}

/** Clock is time and timers (tests pass a fake one). */
export interface Clock {
  now(): number;
  setTimeout(fn: () => void, ms: number): unknown;
  clearTimeout(id: unknown): void;
}

/** Strings are every word the menu shows; {label} is the item's name. */
export interface Strings {
  menu: string;
  tapToClose: string;
  favOn: string;
  favOff: string;
  saved: string;
  removed: string;
  run: string;
  full: string;
  savedEmpty: string;
  savedLabel: string;
  fabLabel: string;
  close: string;
}

export const STRINGS: Strings = {
  menu: "menu",
  tapToClose: "tap to close",
  favOn: "★  favorited",
  favOff: "☆  add to favorites",
  saved: "★ saved {label}",
  removed: "removed {label} from saved",
  run: "▸ run {label}",
  full: "saved is full: remove one first",
  savedEmpty: "hold any action to save it here",
  savedLabel: "SAVED",
  fabLabel: "Radial menu",
  close: "Close menu",
};

/** Theme colours; the rest of the palette is the design's own. */
export interface Theme {
  accent?: string;
  accentDim?: string;
  font?: string;
}

/** The SAVED category's id: its items are the favourites. */
export const SAVED = "saved";

export interface MenuOptions {
  categories: Category[];
  /** ids of the favourite items, in order (they make the SAVED ring) */
  favorites?: string[];
  onFavoritesChange?(ids: string[]): void;
  /** saved: false leaves the SAVED ring out */
  saved?: boolean;
  strings?: Partial<Strings>;
  theme?: Theme;
  /** start open (the floating button opens it so) */
  startOpen?: boolean;
  /** called when the close animation ends; without it the hub folds the
   * rings back in place (the standalone menu) */
  onClosed?(): void;
  /** where the entrance grows from and the close shrinks to, as a
   * fraction of the box (0..1); default the centre */
  origin?: [number, number];
  reducedMotion?: boolean;
  insets?: () => Insets;
  clock?: Clock;
  doc?: Document;
  win?: Window;
}

const NS = "http://www.w3.org/2000/svg";
let uid = 0;

function fill(s: string, label: string): string {
  return s.replace("{label}", label);
}

const realClock = (win: Window | undefined): Clock => ({
  now: () => (typeof performance !== "undefined" ? performance.now() : Date.now()),
  setTimeout: (fn, ms) => (win ?? globalThis).setTimeout(fn, ms),
  clearTimeout: (id) => (win ?? globalThis).clearTimeout(id as number),
});

/** Anim is one running animation: started at t0, lasting dur ms. */
interface Anim {
  t0: number;
  dur: number;
}

/** RadialMenu draws the menu into a box element it owns (stage). */
export class RadialMenu {
  readonly stage: HTMLElement;
  private o: MenuOptions;
  private s: Strings;
  private doc: Document;
  private win: Window | undefined;
  private clock: Clock;
  private reduced: boolean;
  private id = ++uid;
  private base: Category[];
  private favs: string[];
  private active = 0;
  open: boolean;
  closing = false;
  private destroyed = false;
  // animations (null: not running)
  private entrance: Anim | null;
  private rings: { t0: number; from: number; to: number } | null = null;
  private ringsV: number;
  private reveal: Anim | null = null;
  private press: { i: number; t0: number; pointer: number; x: number; y: number; timer: unknown } | null = null;
  private fired = false;
  private flash: { i: number; t0: number } | null = null;
  private collapse: Anim | null = null;
  private hot = -1;
  private hotCat = -1;
  private frame: number | null = null;
  // the details card and the toast
  private tipItem: Item | null = null;
  private tipGuard = false;
  private toastTimer: unknown = null;
  /** the box's place on the page: set by the host before every use */
  box = { left: 0, top: 0, side: 0 };
  // parts
  private collapseEl: HTMLElement;
  private entranceEl: HTMLElement;
  private svg: SVGSVGElement;
  private ringsG: SVGGElement;
  private innerG: SVGGElement;
  private outerG: SVGGElement;
  private spark: SVGCircleElement;
  private centerG: SVGGElement;
  private pulse: HTMLElement;
  private tip: HTMLElement;
  private tipIcon: HTMLElement;
  private tipTitle: HTMLElement;
  private tipDesc: HTMLElement;
  private tipFav: HTMLElement;
  private toastEl: HTMLElement;
  private petals: { g: SVGGElement; path: SVGPathElement; icon: SVGGElement; prog: SVGPathElement; len: number; c: [number, number] }[] = [];
  private catEls: SVGPathElement[] = [];

  constructor(stage: HTMLElement, opts: MenuOptions) {
    this.stage = stage;
    this.o = opts;
    this.s = { ...STRINGS, ...opts.strings };
    this.doc = opts.doc ?? document;
    this.win = opts.win ?? (typeof window === "undefined" ? undefined : window);
    this.clock = opts.clock ?? realClock(this.win);
    this.reduced = opts.reducedMotion ?? !!this.win?.matchMedia?.("(prefers-reduced-motion: reduce)").matches;
    this.base = opts.categories;
    this.favs = [...(opts.favorites ?? [])];
    this.open = !!opts.startOpen;
    this.ringsV = this.open ? 1 : 0;
    this.entrance = this.reduced ? null : { t0: this.clock.now(), dur: DUR.entrance };

    stage.classList.add("rm-stage");
    this.theme(opts.theme);
    const div = (cls: string, parent: Element): HTMLElement => {
      const e = this.doc.createElement("div");
      e.className = cls;
      parent.appendChild(e);
      return e;
    };
    div("rm-glow", stage);
    this.collapseEl = div("rm-collapse", stage);
    this.entranceEl = div("rm-entrance", this.collapseEl);
    this.entranceEl.style.setProperty("inset", PAD + "px");
    this.svg = this.svgEl("svg", { viewBox: "0 0 600 600", class: "rm-svg", "aria-label": this.s.fabLabel, role: "menu" }) as SVGSVGElement;
    this.entranceEl.appendChild(this.svg);
    this.svg.appendChild(this.defs());
    this.ringsG = this.svgEl("g", { class: "rm-rings" }) as SVGGElement;
    this.ringsG.appendChild(this.svgEl("circle", { class: "rm-gapring", cx: C, cy: C, r: GAP_RING }));
    this.innerG = this.svgEl("g", { class: "rm-inner" }) as SVGGElement;
    this.outerG = this.svgEl("g", { class: "rm-outer" }) as SVGGElement;
    this.spark = this.svgEl("circle", { class: "rm-spark", r: 17, filter: `url(#rm${this.id}-blur)` }) as SVGCircleElement;
    this.ringsG.append(this.innerG, this.outerG, this.spark);
    this.centerG = this.svgEl("g", { class: "rm-center" }) as SVGGElement;
    this.svg.append(this.ringsG, this.centerG);
    this.pulse = div("rm-pulse", this.collapseEl);
    this.tip = div("rm-tip", this.collapseEl);
    this.tip.setAttribute("role", "dialog");
    div("rm-tip-bar", this.tip);
    const head = div("rm-tip-head", this.tip);
    this.tipIcon = this.doc.createElement("span");
    this.tipIcon.className = "rm-tip-ic";
    this.tipTitle = this.doc.createElement("span");
    this.tipTitle.className = "rm-tip-title";
    head.append(this.tipIcon, this.tipTitle);
    this.tipDesc = div("rm-tip-desc", this.tip);
    this.tipFav = this.doc.createElement("button");
    this.tipFav.className = "rm-tip-fav";
    this.tipFav.setAttribute("type", "button");
    this.tip.appendChild(this.tipFav);
    this.tip.hidden = true;
    this.toastEl = div("rm-toast", this.collapseEl);
    this.toastEl.setAttribute("role", "status");
    this.toastEl.setAttribute("aria-live", "polite");

    // The card keeps its own taps; its favourite button toggles.
    this.tip.addEventListener("pointerdown", (e) => e.stopPropagation());
    this.tip.addEventListener("pointerup", (e) => e.stopPropagation());
    this.tipFav.addEventListener("click", (e) => {
      e.stopPropagation();
      if (this.tipItem) {
        this.toggleFav(this.tipItem);
        this.updateTipFav();
      }
    });
    stage.addEventListener("pointerdown", (e) => this.down(e as PointerEvent));
    stage.addEventListener("pointermove", (e) => this.move(e as PointerEvent));
    stage.addEventListener("pointerup", (e) => this.up(e as PointerEvent));
    stage.addEventListener("pointercancel", () => this.cancel());
    stage.addEventListener("pointerleave", (e) => {
      if ((e as PointerEvent).pointerType === "mouse") this.setHot(-1, -1);
    });
    stage.addEventListener("contextmenu", (e) => e.preventDefault());
    stage.addEventListener("keydown", (e) => this.key(e as KeyboardEvent));

    this.buildInner();
    this.buildOuter();
    this.buildCenter();
    if (this.open && !this.reduced) this.reveal = { t0: this.clock.now(), dur: DUR.reveal };
    this.paint();
    this.kick();
  }

  // ── data ───────────────────────────────────────────────────────────────

  /** cats are the categories shown: SAVED (the favourites) first. */
  cats(): Category[] {
    if (this.o.saved === false) return this.base;
    const all = new Map<string, Item>();
    for (const c of this.base) for (const it of c.items) if (!all.has(it.id)) all.set(it.id, it);
    const items = this.favs.map((id) => all.get(id)).filter((x): x is Item => !!x);
    return [{ id: SAVED, label: this.s.savedLabel, icon: "star", items }, ...this.base];
  }

  get activeIndex(): number {
    return this.active;
  }

  get favorites(): string[] {
    return [...this.favs];
  }

  /** items of the active category, at most MAX_ITEMS */
  items(): Item[] {
    return (this.cats()[this.active]?.items ?? []).slice(0, MAX_ITEMS);
  }

  setCategories(c: Category[]): void {
    this.base = c;
    this.active = Math.min(this.active, this.cats().length - 1);
    this.buildInner();
    this.buildOuter();
    this.buildCenter();
    this.paint();
  }

  setFavorites(ids: string[]): void {
    this.favs = [...ids];
    if (this.cats()[this.active]?.id === SAVED) this.buildOuter();
    this.paint();
  }

  isFav(it: Item): boolean {
    return this.favs.includes(it.id);
  }

  /** toggleFav adds it to SAVED (toast, pulse) or takes it out (toast). */
  toggleFav(it: Item): void {
    const i = this.favs.indexOf(it.id);
    if (i >= 0) {
      this.favs.splice(i, 1);
      this.showToast(fill(this.s.removed, it.label));
    } else if (this.favs.length >= MAX_ITEMS) {
      this.showToast(this.s.full);
      return;
    } else {
      this.favs.push(it.id);
      this.firePulse();
      this.showToast(fill(this.s.saved, it.label));
    }
    this.o.onFavoritesChange?.([...this.favs]);
    if (this.cats()[this.active]?.id === SAVED) {
      this.buildOuter();
      this.paint();
    }
  }

  // ── state changes ──────────────────────────────────────────────────────

  /** toggle is the hub: open the rings, or close. */
  toggle(): void {
    if (this.open) {
      this.requestClose();
      return;
    }
    this.open = true;
    this.startRings(1);
    this.startReveal();
    this.buildCenter();
    this.kick();
  }

  /** requestClose closes: into the button (onClosed) or the rings fold
   * back in place. */
  requestClose(): void {
    if (this.closing || this.destroyed) return;
    this.hideTip();
    this.resetPress();
    if (!this.o.onClosed) {
      if (!this.open) return;
      this.open = false;
      this.startRings(0);
      this.buildCenter();
      this.kick();
      return;
    }
    this.closing = true;
    if (this.reduced) {
      this.o.onClosed();
      return;
    }
    this.collapse = { t0: this.clock.now(), dur: DUR.collapse };
    this.kick();
  }

  /** switchCat shows category i's actions (no back step). */
  switchCat(i: number): void {
    if (i === this.active || i < 0 || i >= this.cats().length) return;
    this.active = i;
    this.hideTip();
    this.buildInner();
    this.buildOuter();
    this.buildCenter();
    this.startReveal();
    this.kick();
    if (this.items().length === 0 && this.cats()[i].id === SAVED) this.showToast(this.s.savedEmpty);
  }

  /** runAction is a tap on action i: flash, close, run. */
  runAction(i: number): void {
    const it = this.items()[i];
    if (!it) return;
    this.hideTip();
    this.flash = this.reduced ? null : { i, t0: this.clock.now() };
    if (it.run) {
      this.requestClose();
      it.run();
      return;
    }
    this.showToast(fill(this.s.run, it.label));
    this.kick();
  }

  private startRings(to: number): void {
    if (this.reduced) {
      this.ringsV = to;
      this.rings = null;
      return;
    }
    this.rings = { t0: this.clock.now(), from: this.ringsV, to };
  }

  private startReveal(): void {
    this.reveal = this.reduced ? null : { t0: this.clock.now(), dur: DUR.reveal };
  }

  // ── pointer ────────────────────────────────────────────────────────────

  private hit(e: { clientX: number; clientY: number }): Hit {
    const [x, y] = toDesign(e.clientX, e.clientY, this.box);
    if (x < 0 || y < 0 || x > 600 || y > 600) return null;
    return hitTest(x, y, this.open, this.cats().length, this.items().length);
  }

  /** inside says whether a page point is on the drawing (not the padding). */
  inside(x: number, y: number): boolean {
    const [dx, dy] = toDesign(x, y, this.box);
    return dx >= 0 && dy >= 0 && dx <= 600 && dy <= 600;
  }

  private down(e: PointerEvent): void {
    // The box's padding is outside the menu: the page's tap catcher has it.
    if (this.closing || !this.inside(e.clientX, e.clientY)) return;
    e.stopPropagation();
    const h = this.hit(e);
    if (!h) {
      if (!this.tipGuard) this.hideTip();
      return;
    }
    try {
      this.stage.setPointerCapture?.(e.pointerId);
    } catch {
      /* a synthetic pointer */
    }
    if (h.kind === "act") {
      this.resetPress();
      this.fired = false;
      const i = h.i;
      this.press = {
        i,
        t0: this.clock.now(),
        pointer: e.pointerId,
        x: e.clientX,
        y: e.clientY,
        timer: this.clock.setTimeout(() => this.held(), DUR.hold),
      };
      this.kick();
    }
  }

  private held(): void {
    const p = this.press;
    if (!p) return;
    this.fired = true;
    const it = this.items()[p.i];
    this.press = null;
    if (it) this.openTip(it, p.x, p.y);
    this.paint();
  }

  private move(e: PointerEvent): void {
    if (this.closing) return;
    const h = this.hit(e);
    if (this.press && e.pointerId === this.press.pointer) {
      // Off the held wedge (or a big slide): no hold, no tap.
      const far = Math.hypot(e.clientX - this.press.x, e.clientY - this.press.y) > SLOP * 3;
      if (!h || h.kind !== "act" || h.i !== this.press.i || far) this.resetPress();
      return;
    }
    if (e.pointerType === "mouse" && !this.press) this.setHot(h?.kind === "act" ? h.i : -1, h?.kind === "cat" ? h.i : -1);
  }

  private up(e: PointerEvent): void {
    if (this.closing || (!this.press && !this.inside(e.clientX, e.clientY))) return;
    e.stopPropagation();
    const h = this.hit(e);
    const was = this.press?.i ?? -1;
    this.resetPress();
    if (!h) return;
    if (h.kind === "hub") return this.toggle();
    if (h.kind === "cat") return this.switchCat(h.i);
    if (h.kind === "act" && h.i === was) {
      if (this.fired) {
        this.fired = false; // the hold already opened the card
        return;
      }
      this.runAction(h.i);
    }
  }

  private cancel(): void {
    this.resetPress();
  }

  private resetPress(): void {
    if (this.press) this.clock.clearTimeout(this.press.timer);
    this.press = null;
    this.paint();
  }

  private setHot(i: number, c: number): void {
    if (i === this.hot && c === this.hotCat) return;
    this.hot = i;
    this.hotCat = c;
    this.catEls.forEach((p, k) => p.setAttribute("fill", `url(#rm${this.id}-${k === this.active ? "catActive" : k === c ? "catHover" : "cat"})`));
    this.paint();
  }

  // ── keyboard (beyond the touch design: every action is reachable) ──────

  private key(e: KeyboardEvent): void {
    const t = e.target as Element | null;
    const kind = t?.getAttribute?.("data-rm");
    const i = Number(t?.getAttribute?.("data-i") ?? -1);
    if (e.key === "Escape") {
      e.preventDefault();
      if (this.tipItem) this.hideTip();
      else this.requestClose();
      return;
    }
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      if (kind === "act") this.runAction(i);
      else if (kind === "cat") this.switchCat(i);
      else if (kind === "hub") this.toggle();
      return;
    }
    if ((e.key === "ArrowRight" || e.key === "ArrowLeft" || e.key === "ArrowDown" || e.key === "ArrowUp") && (kind === "act" || kind === "cat")) {
      e.preventDefault();
      const els = kind === "act" ? this.petals.map((p) => p.path) : this.catEls;
      const d = e.key === "ArrowRight" || e.key === "ArrowDown" ? 1 : -1;
      (els[(i + d + els.length) % els.length] as unknown as HTMLElement | undefined)?.focus?.();
      return;
    }
    if (kind === "act" && (e.key === "ContextMenu" || (e.key === "F10" && e.shiftKey))) {
      e.preventDefault();
      const it = this.items()[i];
      const p = this.petals[i];
      if (it && p) {
        const [x, y] = this.pagePoint(p.c[0], p.c[1]);
        this.openTip(it, x, y);
      }
    }
  }

  private pagePoint(x: number, y: number): [number, number] {
    const s = (this.box.side - 2 * PAD) / 600;
    return [this.box.left + PAD + x * s, this.box.top + PAD + y * s];
  }

  // ── building ───────────────────────────────────────────────────────────

  private svgEl(tag: string, attrs: Record<string, string | number> = {}): SVGElement {
    const e = this.doc.createElementNS(NS, tag) as SVGElement;
    for (const k in attrs) e.setAttribute(k, String(attrs[k]));
    return e;
  }

  private icon(name: string, x: number, y: number, s: number, col: string): SVGGElement {
    const g = this.svgEl("g", { transform: `translate(${x} ${y}) scale(${s})`, "pointer-events": "none", "data-icon": name }) as SVGGElement;
    g.innerHTML = iconSVG(name, col);
    return g;
  }

  private defs(): SVGElement {
    const d = this.svgEl("defs");
    const grad = (id: string, r: number, stops: [number, string][]): void => {
      const g = this.svgEl("radialGradient", { id: `rm${this.id}-${id}`, gradientUnits: "userSpaceOnUse", cx: C, cy: C, r });
      for (const [o, c] of stops) g.appendChild(this.svgEl("stop", { offset: o, style: `stop-color:${c}` }));
      d.appendChild(g);
    };
    // radial_menu.dart _paintInner / _paintOuter (mobocode-radial-v6.html: the darker originals)
    grad("cat", 116, [[0.4, "#24242F"], [1, "#131319"]]);
    grad("catHover", 116, [[0.4, "#2C2C38"], [1, "#1B1B22"]]);
    grad("catActive", 116, [[0.3, "var(--rm-cat-active, #264F78)"], [1, "#15314C"]]);
    grad("wedge", 212, [[0.5, "#272732"], [1, "#111116"]]);
    grad("wedgeHot", 212, [[0.5, "#34343F"], [1, "#1C1C24"]]);
    grad("wedgeAcc", 212, [[0.5, "#16456E"], [1, "#0E2D49"]]);
    const blur = this.svgEl("filter", { id: `rm${this.id}-blur`, x: "-200%", y: "-200%", width: "500%", height: "500%" });
    blur.appendChild(this.svgEl("feGaussianBlur", { stdDeviation: 13 }));
    d.appendChild(blur);
    return d;
  }

  private buildInner(): void {
    const cats = this.cats(), n = cats.length;
    this.innerG.replaceChildren();
    this.catEls = [];
    cats.forEach((cat, i) => {
      const c0 = catCentre(i, n), half = 180 / n, on = i === this.active;
      const p = this.svgEl("path", {
        d: sectorPath(c0 - half + CAT_GAP, c0 + half - CAT_GAP, RI1, RO1),
        class: "rm-cat" + (on ? " rm-on" : ""),
        fill: `url(#rm${this.id}-${on ? "catActive" : "cat"})`,
        "data-rm": "cat",
        "data-i": i,
        "data-id": cat.id,
        role: "menuitemradio",
        "aria-checked": on ? "true" : "false",
        "aria-label": cat.label,
        tabindex: on ? 0 : -1,
      }) as SVGPathElement;
      this.catEls.push(p);
      this.innerG.appendChild(p);
      const [x, y] = polar(CAT_ICON_R, c0);
      this.innerG.appendChild(this.icon(cat.icon, x, y, CAT_ICON_S, on ? "#AEDAFF" : "#BEBEC8"));
    });
  }

  private buildOuter(): void {
    const items = this.items(), n = items.length;
    this.outerG.replaceChildren();
    this.petals = [];
    items.forEach((it, i) => {
      const c0 = actCentre(i, n), half = 180 / n;
      const d1 = c0 - half, d2 = c0 + half;
      const g = this.svgEl("g", { class: "rm-item", "data-id": it.id }) as SVGGElement;
      const path = this.svgEl("path", {
        d: sectorPath(d1 + ACT_GAP, d2 - ACT_GAP, RI2, RO2),
        class: "rm-act",
        "data-rm": "act",
        "data-i": i,
        role: "menuitem",
        "aria-label": it.detail ? `${it.label}: ${it.detail}` : it.label,
        tabindex: 0,
      }) as SVGPathElement;
      const c = polar(ACT_ICON_R, c0);
      const icon = this.icon(it.icon, c[0], c[1], ACT_ICON_S, "currentColor");
      const len = arcLength(d1 + PROG_INSET, d2 - PROG_INSET, PROG_R);
      const prog = this.svgEl("path", { d: arcPath(d1 + PROG_INSET, d2 - PROG_INSET, PROG_R), class: "rm-prog", "stroke-dasharray": len, "stroke-dashoffset": len }) as SVGPathElement;
      g.append(path, icon, prog);
      this.outerG.appendChild(g);
      this.petals.push({ g, path, icon, prog, len, c });
    });
  }

  private buildCenter(): void {
    const g = this.centerG;
    g.replaceChildren();
    const hub = this.svgEl("circle", { cx: C, cy: C, r: HUB, class: "rm-hub" + (this.open ? "" : " rm-closed"), "data-rm": "hub", role: "button", tabindex: 0, "aria-label": this.open ? this.s.close : this.s.fabLabel });
    g.appendChild(hub);
    g.appendChild(this.svgEl("circle", { cx: C, cy: C, r: HUB_RING, class: "rm-hubring", "pointer-events": "none" }));
    const text = (cls: string, y: number, s: string): void => {
      const t = this.svgEl("text", { x: C, y, class: cls, "pointer-events": "none", "dominant-baseline": "central", "text-anchor": "middle" });
      t.textContent = s;
      g.appendChild(t);
    };
    if (this.open) {
      text("rm-cname", C - 1, this.cats()[this.active]?.label ?? "");
      text("rm-csub", C + 18, this.s.tapToClose);
    } else {
      g.appendChild(this.icon("ask", C, C - 5, 1.5, "var(--rm-accent, #007ACC)"));
      text("rm-csub", C + 20, this.s.menu);
    }
  }

  private theme(t: Theme | undefined): void {
    if (!t) return;
    const st = this.stage.style;
    if (t.accent) st.setProperty("--rm-accent", t.accent);
    if (t.accentDim) st.setProperty("--rm-accent-dim", t.accentDim);
    if (t.font) st.setProperty("--rm-font", t.font);
  }

  // ── painting ───────────────────────────────────────────────────────────

  private kick(): void {
    if (this.frame !== null || this.destroyed) return;
    const raf = this.win?.requestAnimationFrame?.bind(this.win);
    if (!raf) return;
    this.frame = raf(() => {
      this.frame = null;
      const more = this.paint();
      if (more) this.kick();
    }) as number;
  }

  private prog(a: Anim | null): number {
    if (!a) return 1;
    return Math.min(1, Math.max(0, (this.clock.now() - a.t0) / a.dur));
  }

  /** paint draws the current frame; it says whether anything still moves. */
  paint(): boolean {
    if (this.destroyed) return false;
    let moving = false;
    const now = this.clock.now();
    const [ox, oy] = this.o.origin ?? [0.5, 0.5];
    const origin = `${ox * 100}% ${oy * 100}%`;

    // menuIn
    const ev = this.prog(this.entrance);
    const en = entranceFrame(ev);
    if (ev >= 1) this.entrance = null;
    else moving = true;
    this.entranceEl.style.setProperty("opacity", String(en.opacity));
    this.entranceEl.style.setProperty("transform", `scale(${en.scale})`);
    this.entranceEl.style.setProperty("transform-origin", origin);

    // the suck-in close
    if (this.collapse) {
      const v = this.prog(this.collapse);
      const cf = collapseFrame(v);
      this.collapseEl.style.setProperty("opacity", String(cf.opacity));
      this.collapseEl.style.setProperty("transform", `rotate(${cf.rotate}rad) scale(${cf.scale})`);
      this.collapseEl.style.setProperty("transform-origin", origin);
      if (v >= 1) {
        this.collapse = null;
        this.o.onClosed?.();
        return false;
      }
      moving = true;
    }

    // rings open / close
    if (this.rings) {
      const r = this.rings;
      const span = Math.abs(r.to - r.from) || 1;
      const k = Math.min(1, (now - r.t0) / (DUR.rings * span));
      this.ringsV = r.from + (r.to - r.from) * k;
      if (k >= 1) this.rings = null;
      else moving = true;
    }
    const rf = ringsFrame(this.ringsV);
    this.ringsG.setAttribute("opacity", String(rf.opacity));
    this.ringsG.setAttribute("transform", `translate(${C} ${C}) rotate(${rf.twist}) scale(${rf.scale}) translate(${-C} ${-C})`);
    this.ringsG.setAttribute("visibility", this.ringsV > 0.001 ? "visible" : "hidden");
    this.ringsG.setAttribute("pointer-events", this.open ? "auto" : "none");

    // the bloom, the hold, the flash
    const rv = this.reveal ? this.prog(this.reveal) : 1;
    const blooming = rv < 1 && this.open;
    if (this.reveal && rv >= 1) this.reveal = null;
    if (blooming) moving = true;
    const pk = this.press ? Math.min(1, (now - this.press.t0) / DUR.hold) : 0;
    if (this.press) moving = true;
    let fk = 0;
    if (this.flash) {
      fk = Math.min(1, (now - this.flash.t0) / DUR.flash);
      if (fk >= 1) this.flash = null;
      else moving = true;
    }
    const n = this.petals.length;
    this.petals.forEach((p, i) => {
      const bf = blooming ? petalFrame(i, n, rv) : { show: true, opacity: 1, scale: 1 };
      const held = this.press?.i === i;
      const hot = held || this.hot === i;
      const flashing = this.flash?.i === i && fk > 0 && fk < 1;
      let s = bf.scale;
      if (held) s *= holdScale(pk);
      if (flashing) s *= flashScale(fk);
      p.g.setAttribute("visibility", bf.show ? "visible" : "hidden");
      p.g.setAttribute("opacity", String(bf.opacity));
      p.g.setAttribute("transform", s === 1 ? "" : `translate(${p.c[0]} ${p.c[1]}) scale(${s}) translate(${-p.c[0]} ${-p.c[1]})`);
      p.g.classList.toggle("rm-hot", hot);
      p.g.classList.toggle("rm-flash", flashing);
      p.path.setAttribute("fill", `url(#rm${this.id}-${flashing ? "wedgeAcc" : hot ? "wedgeHot" : "wedge"})`);
      const showProg = held && pk > 0.16;
      p.prog.setAttribute("opacity", showProg ? "1" : "0");
      p.prog.setAttribute("stroke-dashoffset", String(showProg ? p.len * (1 - pk) : p.len));
    });
    if (blooming && n > 0) {
      const sp = sparkFrame(rv);
      const [x, y] = polar(ACT_ICON_R, sp.deg);
      this.spark.setAttribute("cx", String(x));
      this.spark.setAttribute("cy", String(y));
      this.spark.setAttribute("opacity", String(sp.alpha));
      this.spark.setAttribute("visibility", "visible");
    } else this.spark.setAttribute("visibility", "hidden");
    return moving;
  }

  // ── the details card, the toast, the pulse ─────────────────────────────

  private updateTipFav(): void {
    const on = !!this.tipItem && this.isFav(this.tipItem);
    this.tipFav.classList.toggle("rm-on", on);
    this.tipFav.textContent = on ? this.s.favOn : this.s.favOff;
    this.tipFav.setAttribute("aria-pressed", on ? "true" : "false");
  }

  get tipShown(): Item | null {
    return this.tipItem;
  }

  /** openTip shows it's details card near the page point (x, y). */
  openTip(it: Item, x: number, y: number): void {
    this.tipItem = it;
    this.tipGuard = true;
    this.clock.setTimeout(() => (this.tipGuard = false), DUR.tipGuard);
    this.tipIcon.innerHTML = `<svg viewBox="-11.25 -11.25 22.5 22.5" aria-hidden="true">${iconSVG(it.icon, "currentColor")}</svg>`;
    this.tipTitle.textContent = it.label;
    this.tipDesc.textContent = it.detail ?? "";
    this.tipDesc.hidden = !it.detail;
    this.updateTipFav();
    this.tip.setAttribute("aria-label", it.label);
    this.tip.hidden = false;
    this.tip.classList.remove("rm-show");
    const r = this.tip.getBoundingClientRect();
    const w = r.width || 178, h = r.height || 110;
    const ins = this.o.insets?.() ?? { top: 0, right: 0, bottom: 0, left: 0 };
    const sw = this.win?.innerWidth ?? 390, sh = this.win?.innerHeight ?? 844;
    const p = placeTip(x, y, w, h, sw, sh, ins.top + 8, ins.bottom + 8);
    // The card lives in the box (it closes with it): page → box coords.
    this.tip.style.setProperty("left", p.left - this.box.left + "px");
    this.tip.style.setProperty("top", p.top - this.box.top + "px");
    this.tip.style.setProperty("--rm-arrow", p.arrow + "px");
    this.tip.classList.toggle("rm-below", p.below);
    void this.tip.getBoundingClientRect(); // restart the rise
    this.tip.classList.add("rm-show");
  }

  hideTip(): void {
    if (!this.tipItem) return;
    this.tipItem = null;
    this.tip.classList.remove("rm-show");
    this.tip.hidden = true;
  }

  get toastText(): string | null {
    return this.toastEl.classList.contains("rm-show") ? this.toastEl.textContent : null;
  }

  showToast(s: string): void {
    this.toastEl.textContent = s;
    this.toastEl.classList.add("rm-show");
    if (this.toastTimer !== null) this.clock.clearTimeout(this.toastTimer);
    this.toastTimer = this.clock.setTimeout(() => {
      this.toastTimer = null;
      this.toastEl.classList.remove("rm-show");
    }, DUR.toastStay);
  }

  private firePulse(): void {
    if (this.reduced) return;
    this.pulse.classList.remove("rm-go");
    void this.pulse.getBoundingClientRect(); // restart the animation
    this.pulse.classList.add("rm-go");
  }

  get pulsing(): boolean {
    return this.pulse.classList.contains("rm-go");
  }

  destroy(): void {
    this.destroyed = true;
    if (this.frame !== null) this.win?.cancelAnimationFrame?.(this.frame);
    this.frame = null;
    if (this.press) this.clock.clearTimeout(this.press.timer);
    if (this.toastTimer !== null) this.clock.clearTimeout(this.toastTimer);
    this.stage.replaceChildren();
  }
}
