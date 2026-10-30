// The key bar's keys (WEB-20, WEB-56). The user picks the bar from this
// fixed list; prefs keep only these names, so nothing typed (and never a
// secret) is ever stored. Pure.

/** KeyId names one key of the list. */
export type KeyId =
  | "esc" | "tab" | "ctrl" | "alt" | "left" | "down" | "up" | "right" | "int"
  | "home" | "end" | "pgup" | "pgdn" | "del"
  | "ctrl_a" | "ctrl_d" | "ctrl_e" | "ctrl_l" | "ctrl_r" | "ctrl_z"
  | "pipe" | "tilde" | "slash" | "dash";

/** KEY_IDS is every key, in the order the editor offers them. */
export const KEY_IDS: readonly KeyId[] = [
  "esc", "tab", "ctrl", "alt", "left", "down", "up", "right", "int",
  "home", "end", "pgup", "pgdn", "del",
  "ctrl_a", "ctrl_d", "ctrl_e", "ctrl_l", "ctrl_r", "ctrl_z",
  "pipe", "tilde", "slash", "dash",
];

/** DEFAULT_KEYS is the bar until the user changes it. */
export const DEFAULT_KEYS: readonly KeyId[] = ["esc", "tab", "ctrl", "alt", "left", "down", "up", "right", "int"];

/** MAX_KEYS bounds the bar. */
export const MAX_KEYS = KEY_IDS.length;

export function isKeyId(v: unknown): v is KeyId {
  return typeof v === "string" && (KEY_IDS as readonly string[]).includes(v);
}

/** readKeys keeps the known names of v, each once; anything else is dropped. */
export function readKeys(v: unknown): KeyId[] {
  if (!Array.isArray(v)) return [];
  const out: KeyId[] = [];
  for (const x of v) if (isKeyId(x) && !out.includes(x) && out.length < MAX_KEYS) out.push(x);
  return out;
}

/** barKeys is the bar to draw: the user's keys, or the default ones. */
export function barKeys(stored: readonly KeyId[]): readonly KeyId[] {
  return stored.length > 0 ? stored : DEFAULT_KEYS;
}

/** move returns keys with the one at i moved by d (−1 up, +1 down). */
export function move(keys: readonly KeyId[], i: number, d: -1 | 1): KeyId[] {
  const out = [...keys];
  const j = i + d;
  if (i < 0 || i >= out.length || j < 0 || j >= out.length) return out;
  [out[i], out[j]] = [out[j], out[i]];
  return out;
}
