// Motion of the radial menu: its curves, timings and the per-frame values
// of each animation (radial_menu.dart). Pure: progress in, values out.

/** cubic is a CSS-style cubic-bézier easing (x1, y1, x2, y2). */
export function cubic(x1: number, y1: number, x2: number, y2: number): (t: number) => number {
  const bx = (t: number): number => 3 * x1 * t * (1 - t) * (1 - t) + 3 * x2 * t * t * (1 - t) + t * t * t;
  const by = (t: number): number => 3 * y1 * t * (1 - t) * (1 - t) + 3 * y2 * t * t * (1 - t) + t * t * t;
  const dx = (t: number): number => 3 * x1 * (1 - t) * (1 - t) + 6 * (x2 - x1) * t * (1 - t) + 3 * (1 - x2) * t * t;
  return (x: number): number => {
    if (x <= 0) return 0;
    if (x >= 1) return 1;
    let t = x;
    for (let i = 0; i < 8; i++) {
      const e = bx(t) - x;
      if (Math.abs(e) < 1e-6) break;
      const d = dx(t);
      if (Math.abs(d) < 1e-6) break;
      t -= e / d;
    }
    // Newton can leave [0,1] near flat ends: finish by bisection.
    if (t < 0 || t > 1 || Math.abs(bx(t) - x) > 1e-5) {
      let a = 0, b = 1;
      t = x;
      for (let i = 0; i < 40; i++) {
        if (bx(t) < x) a = t;
        else b = t;
        t = (a + b) / 2;
      }
    }
    return by(t);
  };
}

/** softBack is the menu's one spring: a gentle overshoot shared by the
 * rings, each petal's bloom and the details card. */
export const SOFT_BACK = [0.2, 0.9, 0.3, 1.12] as const;
export const softBack = cubic(...SOFT_BACK);
/** menuIn is the design's entrance curve. */
export const MENU_IN = [0.2, 0.85, 0.25, 1] as const;
export const menuIn = cubic(...MENU_IN);
// The Flutter curves the port uses, with their exact control points.
export const easeOutCubic = cubic(0.215, 0.61, 0.355, 1);
export const easeInCubic = cubic(0.55, 0.055, 0.675, 0.19);
export const easeOut = cubic(0, 0, 0.58, 1);
export const easeInOut = cubic(0.42, 0, 0.58, 1);
export const EASE_OUT_BACK = [0.175, 0.885, 0.32, 1.275] as const;
export const easeOutBack = cubic(...EASE_OUT_BACK);

/** Durations in ms. */
export const DUR = {
  entrance: 400,
  rings: 380,
  reveal: 560,
  hold: 520,
  flash: 240,
  pulse: 600,
  collapse: 300,
  toast: 200,
  toastStay: 1500,
  tip: 220,
  tipGuard: 450,
  fabIn: 220,
  breath: 2800,
  spin: 3000,
  spring: 460,
  /** a press held this long on the button picks it up (Flutter's long press) */
  pickUp: 500,
} as const;

/** The finger may move this far (px) and still tap or pick up the button. */
export const SLOP = 18;

/** How much of the reveal each petal takes: the rest is the stagger, so
 * the petals open one after another, clockwise, yet overlap. */
export const BLOOM_WINDOW = 0.55;

const clamp01 = (v: number): number => Math.min(1, Math.max(0, v));

/** entranceFrame: the menu fades in and grows .9 → 1 from the button. */
export function entranceFrame(v: number): { opacity: number; scale: number } {
  return { opacity: clamp01(v), scale: 0.9 + 0.1 * menuIn(clamp01(v)) };
}

/** ringsFrame: the rings fade, grow .66 → 1 with the spring, and settle a
 * -7° twist. */
export function ringsFrame(v: number): { opacity: number; scale: number; twist: number } {
  const t = clamp01(v);
  return { opacity: t, scale: 0.66 + 0.34 * softBack(t), twist: (1 - easeOutCubic(t)) * -7 };
}

/** petalFrame: petal i of n during the reveal at v; show false before
 * its turn. */
export function petalFrame(i: number, n: number, v: number): { show: boolean; opacity: number; scale: number } {
  if (v >= 1) return { show: true, opacity: 1, scale: 1 };
  const p = easeOutCubic(clamp01(v));
  const t0 = (i / n) * (1 - BLOOM_WINDOW);
  const lp = clamp01((p - t0) / BLOOM_WINDOW);
  if (lp <= 0.001) return { show: false, opacity: 0, scale: 0.6 };
  return { show: true, opacity: easeOut(lp), scale: 0.6 + 0.4 * softBack(lp) };
}

/** sparkFrame: the glow that rides the bloom's leading edge. */
export function sparkFrame(v: number): { deg: number; alpha: number } {
  const p = easeOutCubic(clamp01(v));
  return { deg: 360 * p, alpha: clamp01(0.12 + 0.5 * (1 - p)) };
}

/** holdScale: a held petal swells the longer it is held. */
export function holdScale(k: number): number {
  return 1 + 0.05 * easeOut(clamp01(k));
}

/** flashScale: a fired petal gives one soft pop. */
export function flashScale(k: number): number {
  return k > 0 && k < 1 ? 1 + 0.07 * Math.sin(Math.PI * k) : 1;
}

/** collapseFrame: the close sucks the menu into the button. */
export function collapseFrame(v: number): { opacity: number; rotate: number; scale: number } {
  const t = easeInCubic(clamp01(v));
  return { opacity: 1 - t, rotate: 0.5 * t, scale: 1 - 0.92 * t };
}

/** pulseFrame: the favourite ring. */
export function pulseFrame(v: number): { opacity: number; scale: number } {
  const t = clamp01(v);
  return { opacity: 0.55 * (1 - t), scale: 0.55 + 0.75 * t };
}

/** breathFrame: the resting button's slow breath at b (0..1, eased). */
export function breathFrame(b: number): { scale: number; alpha: number; blur: number; grow: number } {
  return { scale: 1 + 0.02 * b, alpha: 0.2 + 0.18 * b, blur: 10 + 5 * b, grow: 2 * b };
}

/** breathAt is the breath's eased level at time ms (repeat, reversing). */
export function breathAt(ms: number): number {
  const cycle = (ms / DUR.breath) % 2;
  return easeInOut(cycle <= 1 ? cycle : 2 - cycle);
}

/** springAt is the button's spot during the spring back from a to b. */
export function springAt(a: [number, number], b: [number, number], v: number): [number, number] {
  const e = easeOutBack(clamp01(v));
  return [a[0] + (b[0] - a[0]) * e, a[1] + (b[1] - a[1]) * e];
}
