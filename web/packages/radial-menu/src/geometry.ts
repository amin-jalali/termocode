// Geometry of the radial menu, in its 600 × 600 design space (the
// mobocode design, float_button/mobocode-radial-v6.html, as ported to
// radial_menu.dart). Pure: numbers in, numbers or path strings out.

/** Centre of the 600 × 600 design space. */
export const C = 300;
export const VIEW = 600;
/** Inner (category) ring. */
export const RI1 = 56;
export const RO1 = 116;
/** Outer (action) ring. */
export const RI2 = 128;
export const RO2 = 212;
/** Centre hub radius; its inner hairline ring sits 6 inside. */
export const HUB = 50;
export const HUB_RING = HUB - 6;
/** The hairline between the two rings. */
export const GAP_RING = 122;
/** Half-gap between neighbouring wedges, in degrees. */
export const CAT_GAP = 1.2;
export const ACT_GAP = 0.7;
/** Icon placement: radius and scale (icons are drawn in a ±9 box). */
export const CAT_ICON_R = (RI1 + RO1) / 2;
export const CAT_ICON_S = 0.82;
export const ACT_ICON_R = (RI2 + RO2) / 2;
export const ACT_ICON_S = 1.62;
/** The long-press progress arc: 5° in from each edge, 5 in from the rim. */
export const PROG_INSET = 5;
export const PROG_R = RO2 - 5;
/** The box around the rings: at most 480 px (the design's own size),
 * smaller only when its disc would come closer than 12 px to an edge;
 * 18 px padding around the drawing. */
export const MAX_SIDE = 480;
export const MARGIN = 12;
export const PAD = 18;
/** The dark disc behind the rings (the design's stage, round): the page
 * never shows between the wedges. */
export const DISC_R = 232;
/** The radius the box is kept on screen by: the whole disc. */
export const CLAMP_R = DISC_R;
/** The outer ring always has at least six equal wedges (the design's);
 * a ring with fewer actions shows the rest empty. */
export const MIN_SLOTS = 6;
/** Most actions a ring takes (marking-menu budget: positions become
 * muscle memory). */
export const MAX_ITEMS = 8;

export type Pt = [number, number];

/** polar is the point at radius r and angle deg (0° = top, clockwise). */
export function polar(r: number, deg: number): Pt {
  const a = ((deg - 90) * Math.PI) / 180;
  return [C + r * Math.cos(a), C + r * Math.sin(a)];
}

const f = (n: number): string => String(Math.round(n * 1000) / 1000);

/** sectorPath is the ring wedge from d1 to d2 (degrees) between ri and ro. */
export function sectorPath(d1: number, d2: number, ri: number, ro: number): string {
  const p1 = polar(ro, d1), p2 = polar(ro, d2), p3 = polar(ri, d2), p4 = polar(ri, d1);
  const lg = d2 - d1 > 180 ? 1 : 0;
  return `M${f(p1[0])},${f(p1[1])} A${ro},${ro} 0 ${lg} 1 ${f(p2[0])},${f(p2[1])} L${f(p3[0])},${f(p3[1])} A${ri},${ri} 0 ${lg} 0 ${f(p4[0])},${f(p4[1])} Z`;
}

/** arcPath is the clockwise arc from d1 to d2 at radius r. */
export function arcPath(d1: number, d2: number, r: number): string {
  const p1 = polar(r, d1), p2 = polar(r, d2);
  return `M${f(p1[0])},${f(p1[1])} A${r},${r} 0 ${d2 - d1 > 180 ? 1 : 0} 1 ${f(p2[0])},${f(p2[1])}`;
}

/** arcLength is the length of arcPath(d1, d2, r). */
export function arcLength(d1: number, d2: number, r: number): number {
  return (Math.abs(d2 - d1) * Math.PI * r) / 180;
}

/** slots is how many wedges the outer ring draws for n actions. */
export function slots(n: number): number {
  return Math.max(MIN_SLOTS, n);
}

/** catCentre: categories start at the bottom (180°) and go clockwise. */
export function catCentre(i: number, n: number): number {
  return 180 + (i * 360) / n;
}

/** actCentre: actions start at the top (0°) and go clockwise. */
export function actCentre(i: number, n: number): number {
  return (i * 360) / n;
}

/** within says whether deg is at most half from centre, around the circle. */
export function within(deg: number, centre: number, half: number): boolean {
  let d = Math.abs(deg - centre) % 360;
  if (d > 180) d = 360 - d;
  return d <= half;
}

export type Hit = { kind: "hub" } | { kind: "cat"; i: number } | { kind: "act"; i: number } | null;

/** hitTest maps a point of the design space to what is under it. A closed
 * menu has only its hub. The outer ring has wedges (slots) for its items
 * first, then empty ones, which hit nothing. */
export function hitTest(x: number, y: number, open: boolean, cats: number, items: number, wedges = items): Hit {
  const dx = x - C, dy = y - C;
  const r = Math.sqrt(dx * dx + dy * dy);
  if (r <= HUB) return { kind: "hub" };
  if (!open) return null;
  let deg = (Math.atan2(dy, dx) * 180) / Math.PI + 90;
  deg %= 360;
  if (deg < 0) deg += 360;
  if (r >= RI1 && r <= RO1 && cats > 0) {
    const step = 360 / cats;
    for (let i = 0; i < cats; i++) if (within(deg, catCentre(i, cats) % 360, step / 2)) return { kind: "cat", i };
  }
  if (r >= RI2 && r <= RO2 && items > 0) {
    const n = Math.max(wedges, items), step = 360 / n;
    for (let i = 0; i < n; i++) if (within(deg, actCentre(i, n) % 360, step / 2)) return i < items ? { kind: "act", i } : null;
  }
  return null;
}

/** Insets are the screen's safe areas (notch, home bar) in px. */
export interface Insets {
  top: number;
  right: number;
  bottom: number;
  left: number;
}

export const NO_INSETS: Insets = { top: 0, right: 0, bottom: 0, left: 0 };

const clamp = (v: number, a: number, b: number): number => Math.min(Math.max(v, Math.min(a, b)), Math.max(a, b));

/** stageSide is the menu box's side on a w × h screen: the design's 480
 * (a 444 px drawing), or less when the disc would not fit 12 px inside. */
export function stageSide(w: number, h: number): number {
  return Math.min(MAX_SIDE, ((Math.min(w, h) - 2 * MARGIN) * VIEW) / (2 * DISC_R) + 2 * PAD);
}

/** ringRadius is how far the menu (its disc) reaches from the centre, in px, for a box of side. */
export function ringRadius(side: number): number {
  return (CLAMP_R / VIEW) * (side - 2 * PAD);
}

/** Placement of the open menu's box. */
export interface MenuBox {
  left: number;
  top: number;
  side: number;
  /** the centre, after clamping */
  cx: number;
  cy: number;
  /** where the button is, as a fraction of the box (0..1): the entrance
   * grows from there and the close shrinks into it */
  ox: number;
  oy: number;
}

/** placeMenu centres the box on the button (anchor; null: the screen's
 * centre) and moves it inward only as far as the rings need to stay on
 * screen. */
export function placeMenu(anchor: Pt | null, w: number, h: number, ins: Insets = NO_INSETS): MenuBox {
  const side = stageSide(w, h);
  const rr = ringRadius(side);
  const ax = anchor ? anchor[0] : w / 2;
  const ay = anchor ? anchor[1] : h / 2;
  const cx = clamp(ax, ins.left + MARGIN + rr, w - ins.right - MARGIN - rr);
  const cy = clamp(ay, ins.top + MARGIN + rr, h - ins.bottom - MARGIN - rr);
  const left = cx - side / 2, top = cy - side / 2;
  return { left, top, side, cx, cy, ox: clamp((ax - left) / side, 0, 1), oy: clamp((ay - top) / side, 0, 1) };
}

/** toDesign maps a point of the page to the design space of a box. */
export function toDesign(x: number, y: number, box: { left: number; top: number; side: number }): Pt {
  const s = (box.side - 2 * PAD) / VIEW;
  return [(x - box.left - PAD) / s, (y - box.top - PAD) / s];
}

/** fromDesign maps a point of the design space to the page. */
export function fromDesign(x: number, y: number, box: { left: number; top: number; side: number }): Pt {
  const s = (box.side - 2 * PAD) / VIEW;
  return [box.left + PAD + x * s, box.top + PAD + y * s];
}

/** The floating button: 64 px, 16 px from the right and 12 px above the
 * bottom (safe area included); dragged spots stay 12 px inside. */
export const FAB_SIZE = 64;
export const FAB_RIGHT = 16;
export const FAB_BOTTOM = 12;
export const FAB_MARGIN = 12;

/** defaultFab is the button's centre in a w × h layer. */
export function defaultFab(w: number, h: number, ins: Insets = NO_INSETS, size = FAB_SIZE): Pt {
  const r = size / 2;
  return [w - ins.right - FAB_RIGHT - r, h - ins.bottom - FAB_BOTTOM - r];
}

/** clampFab is the nearest centre that keeps the button fully on screen. */
export function clampFab(p: Pt, w: number, h: number, ins: Insets = NO_INSETS, size = FAB_SIZE): Pt {
  const r = size / 2, m = FAB_MARGIN;
  return [clamp(p[0], ins.left + m + r, w - ins.right - m - r), clamp(p[1], ins.top + m + r, h - ins.bottom - m - r)];
}

/** The details card: 14 px from the press, at least 8 px from any edge. */
export const TIP_GAP = 14;
export const TIP_MARGIN = 8;
export const TIP_MAX_W = 178;

/** placeTip puts a w × h card centred above the press at (ax, ay), below
 * it when there is no room above, and inside the screen on every edge.
 * below says the card hangs under the press (its arrow points up);
 * arrow is the press's x inside the card. */
export function placeTip(ax: number, ay: number, w: number, h: number, sw: number, sh: number, safeTop: number, safeBottom: number): { left: number; top: number; below: boolean; arrow: number } {
  let left = ax - w / 2;
  let top = ay - h - TIP_GAP;
  let below = false;
  if (top < safeTop) {
    top = ay + TIP_GAP;
    below = true;
  }
  left = clamp(left, TIP_MARGIN, sw - TIP_MARGIN - w);
  top = clamp(top, safeTop, sh - safeBottom - h);
  return { left, top, below, arrow: clamp(ax - left, 12, Math.max(12, w - 12)) };
}
