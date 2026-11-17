// mount puts the keyboard into a page element: absolutely placed keycaps
// over a deck, driven by pointer events (every finger is its own
// pointer, so chords and rollover work), redrawn from the engine's state.
import { Engine, type Clock, type KeyEvent, type MediaAction } from "./engine";
import { makeTick, webClick, webVibrate } from "./feedback";
import { setIcon, type Glyph } from "./icons";
import { defaultHeight, isNarrow, wantsCompact, type Placed } from "./layout";

/** Colours of the board; the accent usually comes from the page theme. */
export interface Theme {
  accent: string;
  deck: string;
  face: string;
  faceDown: string;
  well: string;
  fg: string;
  fgDim: string;
  fgLabel: string;
  fgFn: string;
}

const THEME_VAR: Record<keyof Theme, string> = {
  accent: "--tk-accent",
  deck: "--tk-deck",
  face: "--tk-face",
  faceDown: "--tk-face-down",
  well: "--tk-well",
  fg: "--tk-fg",
  fgDim: "--tk-fg-dim",
  fgLabel: "--tk-fg-label",
  fgFn: "--tk-fg-fn",
};

export interface MountOptions {
  /** compact (phone), full (tablet) or auto (short side < 720 → compact). */
  layout?: "compact" | "full" | "auto";
  /** Board height in px; default width × 0.40 clamped 180…360. */
  height?: number;
  onKey(e: KeyEvent): void;
  onAI?(): void;
  /** Spacebar trackpad motion (accelerated px). */
  onPadMove?(dx: number, dy: number): void;
  /** F1–F4, F6, F7 without fn. A page cannot change the system volume or
   * brightness; the host decides what to do. */
  onMedia?(a: MediaAction): void;
  /** F9 flipped the key sound; on is the new state. */
  onToggleSound?(on: boolean): void;
  theme?: Partial<Theme>;
  /** 0 off, 1 light, 2 medium (default), 3 strong. */
  hapticLevel?: number;
  /** Key click sound, default on. */
  soundOn?: boolean;
  /** Replaces navigator.vibrate (e.g. to honour a page's haptics switch). */
  vibrate?: (ms: number) => void;
  /** Replaces the Web Audio click. */
  click?: (volume: number) => void;
  /** Device tilt drives the keycap shadows (default on). */
  tilt?: boolean;
  clock?: Clock;
  doc?: Document;
  win?: Window;
}

export interface TactileKeyboard {
  readonly el: HTMLElement;
  readonly engine: Engine;
  readonly compact: boolean;
  readonly height: number;
  setOptions(o: Partial<MountOptions>): void;
  /** Re-reads the host's width (and the window for layout "auto"). */
  relayout(): void;
  destroy(): void;
}

const ICON_SIZE = { func: 12, other: 13 };
/** LABEL_PX is a word label's size at most (.tk-label in the CSS). */
const LABEL_PX = 11;

export function mount(host: HTMLElement, opts: MountOptions): TactileKeyboard {
  let o: MountOptions = { soundOn: true, hapticLevel: 2, tilt: true, ...opts };
  const doc = o.doc ?? document;
  const win = o.win ?? (typeof window === "undefined" ? undefined : window);
  const click = o.click ?? webClick();
  const vibrate = o.vibrate ?? webVibrate();
  let soundOn = o.soundOn !== false;

  const el = (tag: string, cls: string, parent?: HTMLElement): HTMLElement => {
    const e = doc.createElement(tag);
    e.className = cls;
    parent?.appendChild(e);
    return e;
  };

  const root = el("div", "tk-kb", host);
  root.setAttribute("role", "group");
  root.setAttribute("aria-label", "Tactile Code keyboard");
  const keysLayer = el("div", "tk-keys", root);
  const glowLayer = el("div", "tk-glow", root);
  const badge = el("div", "tk-pad-badge", root);
  setIcon(badge, "touch");
  el("span", "", badge).textContent = "TRACKPAD";
  badge.hidden = true;

  let keyEls: HTMLElement[] = [];
  let height = 0;
  let narrow = false;

  const engine = new Engine({
    clock: o.clock,
    onKey: (e) => o.onKey(e),
    onAI: () => o.onAI?.(),
    onPadMove: (dx, dy) => o.onPadMove?.(dx, dy),
    onMedia: (m) => o.onMedia?.(m),
    onToggleSound: () => {
      soundOn = !soundOn;
      o.onToggleSound?.(soundOn);
    },
    onChange: () => paint(),
    tick: makeTick(() => ({ soundOn, hapticLevel: o.hapticLevel ?? 2, click, vibrate })),
  });

  function applyTheme(): void {
    for (const [k, v] of Object.entries(o.theme ?? {})) if (v) root.style.setProperty(THEME_VAR[k as keyof Theme], v);
  }

  function wantCompact(): boolean {
    return wantsCompact(o.layout, win);
  }

  function relayout(): void {
    const w = host.getBoundingClientRect().width || win?.innerWidth || 360;
    height = o.height ?? defaultHeight(w);
    root.style.height = `${height}px`;
    engine.layout(w, height, wantCompact());
    narrow = isNarrow(w, engine.compact);
    root.classList.toggle("tk-narrow", narrow);
    build();
  }

  function build(): void {
    keyEls = engine.placed.map((p) => keyEl(p));
    (keysLayer as unknown as { replaceChildren(...n: HTMLElement[]): void }).replaceChildren(...keyEls);
    paint();
  }

  // emWidth is a label's width in em in the board's font (a canvas
  // measure, cached), with a little room to spare; without a canvas, a
  // generous 0.62 em a letter. It measures at the labels' own size
  // (11px): a system font with optical sizes (macOS, iOS) draws small
  // text in a wider cut than at 100px, which let "caps lock" spill.
  const ems = new Map<string, number>();
  let ctx: CanvasRenderingContext2D | null | undefined;
  function emWidth(text: string): number {
    let v = ems.get(text);
    if (v !== undefined) return v;
    if (ctx === undefined) {
      const c = doc.createElement("canvas") as HTMLCanvasElement;
      ctx = typeof c.getContext === "function" ? c.getContext("2d") : null;
      if (ctx) ctx.font = `400 ${LABEL_PX}px ${(win?.getComputedStyle?.(root).fontFamily) || "system-ui, sans-serif"}`;
    }
    const m = ctx?.measureText(text).width;
    v = m ? (m / LABEL_PX) * 1.08 : text.length * 0.62;
    ems.set(text, v);
    return v;
  }

  function icon(parent: HTMLElement, g: Glyph, cls: string, size?: number): void {
    const s = el("span", cls, parent);
    setIcon(s, g);
    if (size) s.style.setProperty("--tk-icon", `${size}px`);
  }

  function keyEl(p: Placed): HTMLElement {
    const def = p.def;
    const k = doc.createElement("div");
    const id = p.arrow ?? def.id ?? (def.kind === "ai" ? "ai" : def.main!);
    k.dataset.key = id;
    const cls = ["tk-key", `tk-${def.kind}`];
    if (p.funcRow) cls.push("tk-func");
    const wide = !!def.label && (def.kind === "mod" || id === "enter" || id === "backspace" || id === "tab");
    if (wide) cls.push(p.x + p.w / 2 > engine.width / 2 ? "tk-hug-r" : "tk-hug-l");
    if (def.main === "`") cls.push("tk-left");
    if (id === "blank") cls.push("tk-blank");
    k.className = cls.join(" ");
    k.style.left = `${p.x}px`;
    k.style.top = `${p.y}px`;
    k.style.width = `${p.w}px`;
    k.style.height = `${p.h}px`;
    k.style.setProperty("--tk-px", p.posX.toFixed(4));
    if (id === "blank") return k;
    k.setAttribute("aria-label", def.label ?? def.main ?? def.fnum ?? id);
    el("span", "tk-well", k);
    el("span", "tk-rim", k);
    const face = el("span", "tk-face", k);
    if (def.kind === "char" && def.shifted) {
      const st = el("span", "tk-legend tk-stack", face);
      el("span", "tk-up", st).textContent = def.shifted;
      el("span", "tk-main", st).textContent = def.main!;
    } else if (narrow && id === "backspace") {
      el("span", "tk-legend tk-main", face).textContent = "\u232b"; // upright: the word would be a few px tall
    } else if (def.kind === "letter") {
      el("span", "tk-legend tk-main", face).textContent = def.main!;
    } else if (def.kind === "ai") {
      icon(face, "sparkle", "tk-legend tk-icon tk-ai", 14);
    } else if (p.arrow) {
      icon(face, p.arrow === "up" ? "arrow_up" : "arrow_down", "tk-legend tk-icon", 13);
    } else if (def.kind !== "space") {
      const core = el("span", "tk-legend tk-core", face);
      if (def.icon) icon(core, def.icon, "tk-icon", p.funcRow ? ICON_SIZE.func : ICON_SIZE.other);
      if (def.label) {
        const l = el("span", "tk-label", core);
        l.textContent = def.label;
        l.style.setProperty("--tk-em", emWidth(def.label).toFixed(3)); // the upright board fits it to one line
      }
    }
    if (def.fnum) el("span", "tk-fnum", face).textContent = def.fnum;
    if (def.led) el("span", "tk-led", face);
    if (def.secondaryIcon) icon(face, def.secondaryIcon, "tk-sec");
    return k;
  }

  function paint(): void {
    const down = engine.pressedIndexes();
    engine.placed.forEach((p, i) => {
      const k = keyEls[i];
      if (!k) return;
      k.classList.toggle("is-down", down.has(i));
      const m = p.def.kind === "mod" ? p.def.id! : "";
      k.classList.toggle("is-armed", !!m && engine.sticky.has(m));
      k.classList.toggle("is-held", !!m && engine.held.has(m));
      if (p.def.led) {
        const led = k.children[2]?.children;
        for (const c of Array.from(led ?? [])) if ((c as HTMLElement).classList.contains("tk-led")) (c as HTMLElement).classList.toggle("is-on", engine.caps);
      }
    });
    root.style.setProperty("--tk-g", String(engine.backlight / 3));
    root.classList.toggle("tk-lit", engine.backlight > 0);
    badge.hidden = !engine.pad;
    const spots: HTMLElement[] = [];
    if (engine.backlight > 0) {
      for (const pt of engine.pointers.values()) {
        const s = doc.createElement("span");
        s.className = "tk-glow-spot";
        s.style.left = `${pt.x}px`;
        s.style.top = `${pt.y}px`;
        s.style.setProperty("--tk-core", String(0.06 + engine.backlight * 0.09));
        spots.push(s);
      }
    }
    (glowLayer as unknown as { replaceChildren(...n: HTMLElement[]): void }).replaceChildren(...spots);
  }

  // pointers: each finger is a pointer; capture keeps its moves and up
  const local = (e: PointerEvent): [number, number] => {
    const r = root.getBoundingClientRect();
    return [e.clientX - r.left, e.clientY - r.top];
  };
  const onDown = (e: PointerEvent): void => {
    e.preventDefault(); // keep the focus (and the caret) in the text target
    try {
      root.setPointerCapture(e.pointerId);
    } catch {
      // a synthetic pointer cannot be captured
    }
    engine.down(e.pointerId, ...local(e));
  };
  const onMove = (e: PointerEvent): void => engine.move(e.pointerId, ...local(e));
  const onUp = (e: PointerEvent): void => engine.up(e.pointerId);
  const onCancel = (e: PointerEvent): void => engine.cancel(e.pointerId);
  const stop = (e: Event): void => e.preventDefault();
  root.addEventListener("pointerdown", onDown as EventListener);
  root.addEventListener("pointermove", onMove as EventListener);
  root.addEventListener("pointerup", onUp as EventListener);
  root.addEventListener("pointercancel", onCancel as EventListener);
  root.addEventListener("mousedown", stop);
  root.addEventListener("contextmenu", stop);

  // tilt: gravity on x/y, normalized and smoothed (lerp 0.2)
  let tx = 0;
  let ty = 0;
  const clamp1 = (v: number): number => Math.max(-1, Math.min(1, v));
  const onMotion = (e: Event): void => {
    const g = (e as DeviceMotionEvent).accelerationIncludingGravity;
    if (!g || g.x == null || g.y == null) return;
    tx += (clamp1(g.x / 9.8) - tx) * 0.2;
    ty += (clamp1(g.y / 9.8) - ty) * 0.2;
    root.style.setProperty("--tk-tx", tx.toFixed(4));
    root.style.setProperty("--tk-ty", ty.toFixed(4));
  };
  const motionOn = (): void => {
    if (win && o.tilt !== false) win.addEventListener("devicemotion", onMotion);
  };
  motionOn();
  const onResize = (): void => {
    if (o.layout === "auto" && wantCompact() !== engine.compact) relayout();
  };
  win?.addEventListener("resize", onResize);
  let ro: ResizeObserver | undefined;
  const RO = (globalThis as { ResizeObserver?: typeof ResizeObserver }).ResizeObserver;
  if (RO) {
    let lastW = -1;
    ro = new RO(() => {
      const w = host.getBoundingClientRect().width;
      if (w && Math.abs(w - lastW) > 0.5) {
        lastW = w;
        relayout();
      }
    });
    ro.observe(host);
  }

  applyTheme();
  root.style.setProperty("--tk-tx", "0");
  root.style.setProperty("--tk-ty", "0");
  relayout();

  return {
    el: root,
    engine,
    get compact() {
      return engine.compact;
    },
    get height() {
      return height;
    },
    setOptions(p) {
      const relay = ("layout" in p && p.layout !== o.layout) || ("height" in p && p.height !== o.height);
      if ("soundOn" in p) soundOn = p.soundOn !== false;
      o = { ...o, ...p };
      if (p.theme) applyTheme();
      if (relay) relayout();
    },
    relayout,
    destroy() {
      engine.destroy();
      ro?.disconnect();
      win?.removeEventListener("devicemotion", onMotion);
      win?.removeEventListener("resize", onResize);
      root.remove();
    },
  };
}
