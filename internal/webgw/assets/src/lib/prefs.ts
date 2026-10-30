// Per-device preferences in localStorage. Never a secret, never a token:
// only the choices below, validated on read. Storage may be missing or
// throw (private mode); the page then runs on the defaults.
import { effect, signal } from "@preact/signals";

const KEY = "termocode.prefs.v1";

export type Theme = "system" | "dark" | "light";
export type Direction = "auto" | "ltr" | "rtl";
export type Language = "auto" | "en" | "fa";

export interface Prefs {
  theme: Theme;
  dir: Direction;
  lang: Language;
  firstRunDone: boolean;
}

const defaults: Prefs = { theme: "system", dir: "auto", lang: "auto", firstRunDone: false };

function pick<T extends string>(v: unknown, allowed: readonly T[], def: T): T {
  return typeof v === "string" && (allowed as readonly string[]).includes(v) ? (v as T) : def;
}

function load(): Prefs {
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return defaults;
    const v = JSON.parse(raw) as Record<string, unknown>;
    return {
      theme: pick(v.theme, ["system", "dark", "light"], defaults.theme),
      dir: pick(v.dir, ["auto", "ltr", "rtl"], defaults.dir),
      lang: pick(v.lang, ["auto", "en", "fa"], defaults.lang),
      firstRunDone: v.firstRunDone === true,
    };
  } catch {
    return defaults;
  }
}

export const prefs = signal<Prefs>(load());

/** setPref changes one preference and saves all of them. */
export function setPref<K extends keyof Prefs>(k: K, v: Prefs[K]): void {
  prefs.value = { ...prefs.value, [k]: v };
}

effect(() => {
  const p = prefs.value;
  try {
    localStorage.setItem(KEY, JSON.stringify(p));
  } catch {
    // Storage is full or blocked: the choice still holds for this page.
  }
});
