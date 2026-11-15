// The Tactile Code keyboard's settings on this device (This phone →
// Keyboard): on/off, key sound, key vibration. Kept apart from the page's
// prefs so the keyboard chunk (tactile.js) reads them without the page.
// Never a secret: three small values, checked on read.
const KEY = "termocode.tactile.v1";

export interface KbPrefs {
  /** on: the Tactile keyboard instead of the system keyboard (default on). */
  on: boolean;
  /** sound: a soft click on each key (default on). */
  sound: boolean;
  /** haptic: 0 off, 1 light, 2 medium (default), 3 strong. */
  haptic: number;
}

export function kbPrefs(): KbPrefs {
  try {
    const v = JSON.parse(localStorage.getItem(KEY) ?? "{}") as Record<string, unknown>;
    return { on: v.on !== false, sound: v.sound !== false, haptic: v.haptic === 0 || v.haptic === 1 || v.haptic === 3 ? v.haptic : 2 };
  } catch {
    return { on: true, sound: true, haptic: 2 };
  }
}

export function setKbPrefs(p: Partial<KbPrefs>): void {
  try {
    localStorage.setItem(KEY, JSON.stringify({ ...kbPrefs(), ...p }));
  } catch {
    // storage blocked: the keyboard keeps its defaults
  }
}
