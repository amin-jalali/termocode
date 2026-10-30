// Follow the laptop's theme (WEB-UX-8, web UX 7.1). The hub sends the
// active termocode theme in the status topic:
//
//   status.theme = { name: "one-dark", light?: false,
//                    colors: { BgEditor: "#282c34", TextPrimary: "#abb2bf", … } }
//
// colors holds IndexHex values keyed by theme.Palette256 field names;
// light says the laptop theme itself is a light one. The page maps the
// fields to its tokens for the scheme it shows: as they are for the same
// scheme, derived (hue kept, lightness moved) for the other one. Then
// every rule of tokens_test.go (TestWebTokensContrast) is enforced by
// moving the foreground's lightness, so a theme can never make the page
// unreadable. Pure: no DOM here; main.tsx writes the result to :root.

/** TOKEN_FIELDS maps each colour token to its palette field. */
export const TOKEN_FIELDS: Readonly<Record<string, string>> = {
  "--bg": "BgEditor",
  "--bg-2": "BgSidebar",
  "--bg-panel": "BgPanel",
  "--bg-float": "BgFloat",
  "--bg-hover": "BgHover",
  "--sel": "BgSelection",
  "--strip": "BgStatusBar",
  "--on-strip": "OnAccent",
  "--text": "TextPrimary",
  "--text-2": "TextSecondary",
  "--muted": "TextMuted",
  "--dim": "TextDim",
  "--border": "BorderDefault",
  "--focus": "BorderFocus",
  "--ai": "AIAccent",
  "--blue": "AccentBlue",
  "--green": "AccentGreen",
  "--amber": "AccentAmber",
  "--red": "AccentRedCoral",
  "--git-mod": "GitModified",
  "--git-add": "GitAdded",
  "--git-del": "GitDeleted",
  "--git-conf": "GitConflict",
};

/** THEME_TOKENS are every token this module may set (main.tsx clears them). */
export const THEME_TOKENS: readonly string[] = [...Object.keys(TOKEN_FIELDS), "--on-ai", "--ai-on-strip"];

export interface LaptopTheme {
  name: string;
  light: boolean;
  colors: Record<string, string>;
}

const HEX = /^#[0-9a-fA-F]{6}$/;
const FIELDS: ReadonlySet<string> = new Set(Object.values(TOKEN_FIELDS));

/** readTheme checks status.theme; anything malformed is null (the page
 * keeps its own tokens). Unknown fields and bad colours are dropped. */
export function readTheme(v: unknown): LaptopTheme | null {
  if (typeof v !== "object" || v === null || Array.isArray(v)) return null;
  const o = v as Record<string, unknown>;
  const c = o.colors;
  if (typeof c !== "object" || c === null || Array.isArray(c)) return null;
  const colors: Record<string, string> = {};
  for (const [k, hex] of Object.entries(c as Record<string, unknown>)) {
    if (FIELDS.has(k) && typeof hex === "string" && HEX.test(hex)) colors[k] = hex.toLowerCase();
  }
  if (!colors.BgEditor || !colors.TextPrimary) return null;
  const name = typeof o.name === "string" ? o.name.slice(0, 40) : "";
  return { name, light: o.light === true, colors };
}

// ---- colour maths (sRGB, WCAG 2 contrast, HSL) ----

type RGB = [number, number, number];

function rgb(hex: string): RGB {
  return [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16)) as RGB;
}

function hex([r, g, b]: RGB): string {
  return "#" + [r, g, b].map((x) => Math.round(Math.min(255, Math.max(0, x))).toString(16).padStart(2, "0")).join("");
}

function lum(h: string): number {
  const [r, g, b] = rgb(h).map((v) => {
    const s = v / 255;
    return s <= 0.03928 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4);
  });
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

/** contrast is the WCAG contrast ratio of two colours. */
export function contrast(a: string, b: string): number {
  const x = lum(a), y = lum(b);
  return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05);
}

type HSL = [number, number, number];

function hsl(h: string): HSL {
  const [r, g, b] = rgb(h).map((v) => v / 255);
  const max = Math.max(r, g, b), min = Math.min(r, g, b);
  const l = (max + min) / 2;
  if (max === min) return [0, 0, l];
  const d = max - min;
  const s = l > 0.5 ? d / (2 - max - min) : d / (max + min);
  let hue = max === r ? (g - b) / d + (g < b ? 6 : 0) : max === g ? (b - r) / d + 2 : (r - g) / d + 4;
  hue /= 6;
  return [hue, s, l];
}

function fromHSL([h, s, l]: HSL): string {
  if (s === 0) return hex([l * 255, l * 255, l * 255]);
  const q = l < 0.5 ? l * (1 + s) : l + s - l * s;
  const p = 2 * l - q;
  const f = (t: number) => {
    if (t < 0) t += 1;
    if (t > 1) t -= 1;
    if (t < 1 / 6) return p + (q - p) * 6 * t;
    if (t < 1 / 2) return q;
    if (t < 2 / 3) return p + (q - p) * (2 / 3 - t) * 6;
    return p;
  };
  return hex([f(h + 1 / 3) * 255, f(h) * 255, f(h - 1 / 3) * 255]);
}

/** withLight keeps the hue of c and sets its lightness (and caps its saturation). */
function withLight(c: string, l: number, maxSat = 1): string {
  const [h, s] = hsl(c);
  return fromHSL([h, Math.min(s, maxSat), l]);
}

/** readable moves fg's lightness away from the backgrounds until it has
 * min contrast on every one of them. */
export function readable(fg: string, bgs: string[], min: number): string {
  const ok = (c: string) => bgs.every((b) => contrast(c, b) >= min);
  if (ok(fg)) return fg;
  const dark = bgs.reduce((s, b) => s + lum(b), 0) / bgs.length < 0.18;
  const [h, s, l0] = hsl(fg);
  for (let i = 1; i <= 50; i++) {
    const c = fromHSL([h, s, Math.min(1, Math.max(0, dark ? l0 + i * 0.02 : l0 - i * 0.02))]);
    if (ok(c)) return c;
  }
  return dark ? "#ffffff" : "#000000";
}

// Backgrounds of a derived scheme: lightness by token, the source's hue.
const LIGHT_BG: Record<string, number> = {
  "--bg": 0.975, "--bg-2": 0.94, "--bg-panel": 1, "--bg-float": 1, "--bg-hover": 0.92, "--border": 0.85, "--sel": 0.89,
};
const DARK_BG: Record<string, number> = {
  "--bg": 0.11, "--bg-2": 0.19, "--bg-panel": 0.07, "--bg-float": 0.19, "--bg-hover": 0.15, "--border": 0.27, "--sel": 0.3,
};
const LIGHT_FG: Record<string, number> = { "--text": 0.11, "--text-2": 0.23, "--muted": 0.42, "--dim": 0.5 };
const DARK_FG: Record<string, number> = { "--text": 0.82, "--text-2": 0.74, "--muted": 0.6, "--dim": 0.5 };

const SURFACES = ["--bg", "--bg-2", "--bg-panel", "--bg-float"];
const ROWS = ["--bg", "--bg-panel", "--bg-hover"];

/** themeTokens returns the token values for the laptop theme shown in
 * scheme, every contrast rule of the page met. */
export function themeTokens(th: LaptopTheme, scheme: "dark" | "light"): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [tok, field] of Object.entries(TOKEN_FIELDS)) {
    const c = th.colors[field];
    if (c) out[tok] = c;
  }
  const same = (scheme === "light") === th.light;
  if (!same) {
    const bgs = scheme === "light" ? LIGHT_BG : DARK_BG;
    const fgs = scheme === "light" ? LIGHT_FG : DARK_FG;
    for (const [tok, l] of Object.entries(bgs)) if (out[tok]) out[tok] = withLight(out[tok], l, 0.35);
    for (const [tok, l] of Object.entries(fgs)) if (out[tok]) out[tok] = withLight(out[tok], l);
  }
  // Fill what the theme left out from what it has, so the rules below
  // always have both sides.
  const bg = out["--bg"];
  for (const tok of ["--bg-2", "--bg-panel", "--bg-float", "--bg-hover"]) out[tok] ??= bg;
  out["--sel"] ??= out["--bg-hover"];
  out["--strip"] ??= "#0070b8";
  out["--on-strip"] ??= "#ffffff";
  out["--text-2"] ??= out["--text"];
  out["--muted"] ??= out["--text-2"];
  out["--dim"] ??= out["--muted"];
  out["--border"] ??= out["--dim"];
  for (const tok of ["--ai", "--blue", "--green", "--amber", "--red", "--focus"]) out[tok] ??= out["--text"];
  out["--git-mod"] ??= out["--amber"];
  out["--git-add"] ??= out["--green"];
  out["--git-del"] ??= out["--red"];
  out["--git-conf"] ??= out["--blue"];

  // The rules of TestWebTokensContrast, enforced.
  const v = (toks: string[]) => toks.map((x) => out[x]);
  for (const tok of ["--text", "--text-2", "--muted", "--ai", "--blue", "--green", "--amber", "--red"]) {
    out[tok] = readable(out[tok], v(SURFACES), 4.5);
  }
  for (const tok of ["--git-mod", "--git-add", "--git-del", "--git-conf"]) out[tok] = readable(out[tok], v(ROWS), 4.5);
  for (const tok of ["--dim", "--focus"]) out[tok] = readable(out[tok], v(SURFACES), 3);
  // Backgrounds under one fixed text move instead of the text.
  out["--sel"] = readable(out["--sel"], [out["--text"]], 4.5);
  out["--strip"] = readable(out["--strip"], [out["--on-strip"]], 4.5);
  out["--ai-on-strip"] = readable("#f4edff", [out["--strip"]], 4.5);
  out["--on-ai"] = contrast("#ffffff", out["--ai"]) >= contrast("#1c1c1c", out["--ai"]) ? "#ffffff" : "#1c1c1c";
  out["--on-ai"] = readable(out["--on-ai"], [out["--ai"]], 4.5);
  return out;
}
