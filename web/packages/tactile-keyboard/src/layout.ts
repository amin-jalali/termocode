// The Tactile Code layout: a Dell XPS US-ANSI laptop keyboard with an AI
// key in place of the Windows key. Each key has a flex weight inside its
// row; placeKeys turns the rows into pixel rectangles with the same math
// as the original keyboard (8px pad, row and key gaps, a shorter
// function row, a compact phone layout without it).

/** What a key does when it fires. */
export type KeyKind =
  | "char" // a symbol key with a shifted alternate
  | "letter" // case = shift XOR caps
  | "mod" // shift, ctrl, alt, fn, caps: toggles state
  | "special" // a named key: enter, tab, esc, arrows, F-keys…
  | "ai" // the ✨ AI key
  | "space"
  | "arrowStack"; // the stacked up/down cell, drawn as two half keys

/** Icon names a key may draw (the package's own SVG glyphs). */
export type IconName =
  | "volume_off"
  | "volume_mute"
  | "volume_up"
  | "play_arrow"
  | "keyboard"
  | "brightness_low"
  | "brightness_high"
  | "desktop_windows"
  | "chevron_left"
  | "chevron_right"
  | "menu";

export interface KeyDef {
  kind: KeyKind;
  flex: number;
  /** For mod/special keys: the key id (shift, ctrl, enter, f1, …). */
  id?: string;
  /** A word drawn small (esc, tab, shift, backspace…). */
  label?: string;
  /** The main glyph: a symbol, or the upper-case letter. */
  main?: string;
  /** The shifted alternate drawn above main (! over 1). */
  shifted?: string;
  icon?: IconName;
  /** Function-row tag (F1…F12) drawn bottom-right. */
  fnum?: string;
  /** Draws the caps-lock LED dot. */
  led?: boolean;
  /** A small secondary mark (the menu mark on the right ctrl). */
  secondaryIcon?: IconName;
}

const letter = (main: string): KeyDef => ({ kind: "letter", main, flex: 1 });
const sym = (main: string, shifted: string, flex = 1): KeyDef => ({ kind: "char", main, shifted, flex });
const special = (id: string, o: Partial<KeyDef> = {}): KeyDef => ({ kind: "special", id, flex: 1, ...o });
const mod = (id: string, label: string, o: Partial<KeyDef> = {}): KeyDef => ({ kind: "mod", id, label, flex: 1, ...o });

/** The rows, top to bottom. Row 0 (function row) is drawn shorter. */
export const DELL_LAYOUT: KeyDef[][] = [
  [
    special("esc", { label: "esc" }),
    special("f1", { icon: "volume_off", fnum: "F1" }),
    special("f2", { icon: "volume_mute", fnum: "F2" }),
    special("f3", { icon: "volume_up", fnum: "F3" }),
    special("f4", { icon: "play_arrow", fnum: "F4" }),
    special("f5", { icon: "keyboard", fnum: "F5" }),
    special("f6", { icon: "brightness_low", fnum: "F6" }),
    special("f7", { icon: "brightness_high", fnum: "F7" }),
    special("f8", { icon: "desktop_windows", fnum: "F8" }),
    special("f9", { fnum: "F9" }),
    special("f10", { label: "prt sc", fnum: "F10" }),
    special("home", { label: "home", fnum: "F11" }),
    special("end", { label: "end", fnum: "F12" }),
    special("insert", { label: "insert" }),
    special("delete", { label: "delete" }),
    special("blank"),
  ],
  [
    sym("`", "~", 1.2),
    sym("1", "!"),
    sym("2", "@"),
    sym("3", "#"),
    sym("4", "$"),
    sym("5", "%"),
    sym("6", "^"),
    sym("7", "&"),
    sym("8", "*"),
    sym("9", "("),
    sym("0", ")"),
    sym("-", "_"),
    sym("=", "+"),
    special("backspace", { label: "backspace", flex: 1.8 }),
  ],
  [
    special("tab", { label: "tab", flex: 1.8 }),
    ..."QWERTYUIOP".split("").map(letter),
    sym("[", "{"),
    sym("]", "}"),
    sym("\\", "|", 1.2),
  ],
  [
    mod("caps", "caps lock", { flex: 2, led: true }),
    ..."ASDFGHJKL".split("").map(letter),
    sym(";", ":"),
    sym("'", '"'),
    special("enter", { label: "enter", flex: 2 }),
  ],
  [
    mod("shift", "shift", { flex: 2.5 }),
    ..."ZXCVBNM".split("").map(letter),
    sym(",", "<"),
    sym(".", ">"),
    sym("/", "?"),
    mod("shift", "shift", { flex: 2.5 }),
  ],
  [
    mod("ctrl", "ctrl", { flex: 1.5 }),
    mod("fn", "fn"),
    { kind: "ai", label: "AI", flex: 1 },
    mod("alt", "alt"),
    { kind: "space", id: "space", flex: 5.5 },
    mod("alt", "alt"),
    mod("ctrl", "ctrl", { secondaryIcon: "menu" }),
    special("left", { icon: "chevron_left" }),
    { kind: "arrowStack", flex: 1 },
    special("right", { icon: "chevron_right" }),
  ],
];

/** One key placed on the board, in pixels from the board's top-left. */
export interface Placed {
  def: KeyDef;
  x: number;
  y: number;
  w: number;
  h: number;
  /** The row index in the rows used (compact rows start at the number row). */
  row: number;
  /** For the arrow stack halves: which half. */
  arrow?: "up" | "down";
  /** The id that fires: the half's arrow for the stack, else def.id. */
  id?: string;
  /** Horizontal position −1 (far left) … +1 (far right): the shadow side. */
  posX: number;
  /** In the (full layout's) function row: smaller legends. */
  funcRow: boolean;
}

/** The board's outer padding. */
export const PAD = 8;

/** placeKeys lays the rows out in a width × height board. */
export function placeKeys(width: number, height: number, compact: boolean): Placed[] {
  const rows = compact ? DELL_LAYOUT.slice(1) : DELL_LAYOUT;
  const weights = compact ? [1, 1, 1, 1, 1] : [0.7, 1, 1, 1, 1, 1];
  const rowGap = compact ? 1.5 : 3;
  const innerH = height - 2 * PAD - rowGap * (weights.length - 1);
  const unit = innerH / weights.reduce((a, b) => a + b, 0);
  const out: Placed[] = [];
  let y = PAD;
  rows.forEach((row, r) => {
    const rowH = weights[r] * unit;
    const keyGap = compact ? 1.5 : r === 0 ? 1.5 : 3;
    const total = row.reduce((a, k) => a + k.flex, 0);
    const innerW = width - 2 * PAD - keyGap * (row.length - 1);
    let x = PAD;
    for (const def of row) {
      const w = innerW * (def.flex / total);
      const posX = ((x + w / 2) / width) * 2 - 1;
      const funcRow = !compact && r === 0;
      if (def.kind === "arrowStack") {
        const half = rowH / 2; // the halves sit flush
        out.push({ def, x, y, w, h: half, row: r, arrow: "up", id: "up", posX, funcRow });
        out.push({ def, x, y: y + half, w, h: half, row: r, arrow: "down", id: "down", posX, funcRow });
      } else {
        out.push({ def, x, y, w, h: rowH, row: r, id: def.id, posX, funcRow });
      }
      x += w + keyGap;
    }
    y += rowH + rowGap;
  });
  return out;
}

/** hitTest returns the index of the key under (x, y), or −1 in a gap. */
export function hitTest(placed: Placed[], x: number, y: number): number {
  for (let i = 0; i < placed.length; i++) {
    const p = placed[i];
    if (x >= p.x && x < p.x + p.w && y >= p.y && y < p.y + p.h) return i;
  }
  return -1;
}

/** defaultHeight is the board height for a width when none is given. */
export function defaultHeight(width: number): number {
  return Math.min(360, Math.max(180, width * 0.4));
}

/** The short side, in CSS px, below which a screen is a phone. */
export const TABLET_BREAKPOINT = 720;

/** autoCompact: a phone in either orientation gets the compact layout. */
export function autoCompact(width: number, height: number): boolean {
  return Math.min(width, height) < TABLET_BREAKPOINT;
}
