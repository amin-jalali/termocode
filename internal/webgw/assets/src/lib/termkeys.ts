// The desktop terminal's keys: what a key press sends to the shell, as
// an xterm-style terminal sends it (Ctrl+letter is a control character,
// Alt is Esc first, arrows and function keys are CSI sequences with the
// modifier number). Ctrl+letter goes by the key's place (e.code), so it
// works with any keyboard layout. The workbench's own shortcuts (Go to
// file, the command list, the panels) never reach the shell, as on the
// laptop's editor. Pure: no DOM, no state.

/** KeyLike is the part of a KeyboardEvent the mapping reads. */
export interface KeyLike {
  key: string;
  code: string;
  ctrlKey: boolean;
  altKey: boolean;
  shiftKey: boolean;
  metaKey: boolean;
}

/** mods is xterm's modifier number (1 + Shift + 2·Alt + 4·Ctrl). */
function mods(e: KeyLike): number {
  return 1 + (e.shiftKey ? 1 : 0) + (e.altKey ? 2 : 0) + (e.ctrlKey ? 4 : 0);
}

/** csi is a cursor key: ESC [ X, or ESC [ 1 ; m X with modifiers. */
function csi(e: KeyLike, x: string): string {
  const m = mods(e);
  return m > 1 ? `\x1b[1;${m}${x}` : `\x1b[${x}`;
}

/** tilde is a ~ key: ESC [ n ~, or ESC [ n ; m ~ with modifiers. */
function tilde(e: KeyLike, n: number): string {
  const m = mods(e);
  return m > 1 ? `\x1b[${n};${m}~` : `\x1b[${n}~`;
}

const CURSOR: Record<string, string> = { ArrowUp: "A", ArrowDown: "B", ArrowRight: "C", ArrowLeft: "D", Home: "H", End: "F" };
const TILDE: Record<string, number> = { Insert: 2, Delete: 3, PageUp: 5, PageDown: 6, F5: 15, F6: 17, F7: 18, F8: 19, F9: 20, F10: 21, F11: 23, F12: 24 };
const SS3: Record<string, string> = { F1: "P", F2: "Q", F3: "R", F4: "S" };

/** ctrlChar is the control character of Ctrl+key (null: none). */
function ctrlChar(e: KeyLike): string | null {
  const m = /^Key([A-Z])$/.exec(e.code);
  if (m) return String.fromCharCode(m[1].charCodeAt(0) & 0x1f);
  switch (e.key) {
    case " ":
    case "@":
    case "2":
      return "\x00";
    case "[":
      return "\x1b";
    case "\\":
      return "\x1c";
    case "]":
      return "\x1d";
    case "^":
    case "6":
      return "\x1e";
    case "_":
    case "-":
    case "/":
      return "\x1f";
  }
  if (e.key.length === 1) {
    const c = e.key.toUpperCase().charCodeAt(0);
    if (c >= 64 && c <= 95) return String.fromCharCode(c & 0x1f);
  }
  return null;
}

/** workbenchKey: the key is one of the workbench's (it never reaches
 * the shell). mac: Cmd is the shortcut key. */
export function workbenchKey(e: KeyLike, mac: boolean): boolean {
  const mod = mac ? e.metaKey : e.ctrlKey;
  const k = e.key.toLowerCase();
  if (e.key === "F1") return true;
  if (mod && !e.altKey && (k === "p" || k === "j" || k === "b" || k === "," || k === "`")) return true;
  if (mod && e.shiftKey && !e.altKey && (k === "e" || k === "f" || k === "g")) return true;
  if (e.ctrlKey && (k === "tab" || k === "pageup" || k === "pagedown") && !e.altKey) return true;
  return false;
}

/** keyBytes is what the shell gets for a key press, or null when the key
 * is not the terminal's to send: a plain character (the input's own
 * typing sends it, so dead keys and input methods work), Cmd shortcuts,
 * a lone modifier. mac: Option types characters (it is not Alt there). */
export function keyBytes(e: KeyLike, mac: boolean): string | null {
  if (e.metaKey) return null;
  const esc = e.altKey && !mac ? "\x1b" : "";
  switch (e.key) {
    case "Enter":
      return esc + "\r";
    case "Backspace":
      return esc + (e.ctrlKey ? "\x08" : "\x7f");
    case "Tab":
      return e.shiftKey ? "\x1b[Z" : esc + "\t";
    case "Escape":
      return "\x1b";
  }
  if (e.key in CURSOR) return csi(e, CURSOR[e.key]);
  if (e.key in TILDE) return tilde(e, TILDE[e.key]);
  if (e.key in SS3) {
    const m = mods(e);
    return m > 1 ? `\x1b[1;${m}${SS3[e.key]}` : `\x1bO${SS3[e.key]}`;
  }
  if (e.ctrlKey && !(e.altKey && e.key.length === 1 && !/^Key[A-Z]$/.test(e.code))) {
    // Ctrl+Alt+key is AltGr on some keyboards: then the input types it.
    const c = ctrlChar(e);
    return c === null ? null : (e.altKey ? "\x1b" : "") + c;
  }
  if (e.altKey && !mac && e.key.length === 1) return "\x1b" + e.key;
  return null;
}

/** pasteText is pasted text as a terminal takes it: line breaks are
 * Enter (\r), as typing them would be. */
export function pasteText(text: string): string {
  return text.replace(/\r\n?|\n/g, "\r");
}

/** pasteLines counts the lines of a paste (a final line break ends the
 * last one, it does not start another). */
export function pasteLines(text: string): number {
  const n = text.split(/\r\n?|\n/).length;
  return /(\r\n?|\n)$/.test(text) ? n - 1 : n;
}
