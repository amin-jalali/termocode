// Per-device preferences in localStorage. Never a secret, never a token:
// only the choices below, validated on read. Storage may be missing or
// throw (private mode); the page then runs on the defaults.
import { effect, signal } from "@preact/signals";
import { readKeys, type KeyId } from "./keys";
import { looksSecret } from "./secretish";

const KEY = "termocode.prefs.v1";

export type Theme = "system" | "dark" | "light";
export type Direction = "auto" | "ltr" | "rtl";
export type Language = "auto" | "en" | "fa";

export interface Prefs {
  theme: Theme;
  dir: Direction;
  lang: Language;
  firstRunDone: boolean;
  /** rowPx is the text size of terminal and file rows in px; 0 is the default. */
  rowPx: number;
  /** wakeLock: keep the screen on while a run streams (opt-in). */
  wakeLock: boolean;
  /** notify: OS notifications with fixed texts (opt-in, WEB-37). */
  notify: boolean;
  /** keys is the terminal key bar, as names from lib/keys.ts; [] is the default bar. */
  keys: KeyId[];
}

/** ROW_MIN and ROW_MAX bound rowPx (WEB-36). */
export const ROW_MIN = 10;
export const ROW_MAX = 24;

const defaults: Prefs = { theme: "system", dir: "auto", lang: "auto", firstRunDone: false, rowPx: 0, wakeLock: false, notify: false, keys: [] };

function rowSize(v: unknown): number {
  return typeof v === "number" && Number.isInteger(v) && v >= ROW_MIN && v <= ROW_MAX ? v : 0;
}

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
      rowPx: rowSize(v.rowPx),
      wakeLock: v.wakeLock === true,
      notify: v.notify === true,
      keys: readKeys(v.keys),
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

// The chat draft of this device (WEB-14): the last draft typed, kept so a
// reload does not lose it. Never a secret: a draft that looks like one is
// not stored (and an old stored one is removed). Kept for a day.
const DRAFT_KEY = "termocode.draft.v1";
const DRAFT_MAX = 8000;
const DRAFT_DAY_MS = 24 * 60 * 60 * 1000;

export interface StoredDraft {
  chat: string;
  text: string;
}

export function loadDraft(now: number): StoredDraft | null {
  try {
    const raw = localStorage.getItem(DRAFT_KEY);
    if (!raw) return null;
    const v = JSON.parse(raw) as Record<string, unknown>;
    if (typeof v.chat !== "string" || typeof v.text !== "string" || typeof v.at !== "number") return null;
    if (now - v.at > DRAFT_DAY_MS || looksSecret(v.text)) {
      localStorage.removeItem(DRAFT_KEY);
      return null;
    }
    return { chat: v.chat, text: v.text };
  } catch {
    return null;
  }
}

export function saveDraft(chat: string, text: string, now: number): void {
  try {
    if (!text || text.length > DRAFT_MAX || looksSecret(text)) localStorage.removeItem(DRAFT_KEY);
    else localStorage.setItem(DRAFT_KEY, JSON.stringify({ chat, text, at: now }));
  } catch {
    // Storage is full or blocked: the draft still lives in memory.
  }
}
