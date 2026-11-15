// The phone terminal's one way in (PHONE-TERM): every key, from the
// phone keyboard through the terminal engine, from the key bar, or from
// an on-screen keyboard of the page, becomes the bytes a terminal sends
// and goes to the shown shell (term.send). The key bar's sticky Ctrl and
// Alt apply to the next key, whichever way it comes.
//
// For an on-screen keyboard: pressTermKey({ key, code, ctrl, alt, shift,
// meta }) sends one key (key is KeyboardEvent.key: "a", "Enter",
// "ArrowUp", "F5", …), and setTermInputMode("none") keeps the phone's
// own keyboard closed while the terminal has the focus ("text" brings
// it back).
import { signal } from "@preact/signals";
import { keyBytes } from "../../lib/termkeys";
import { alt, ctrl, sendText, used, withMods } from "../TermInput";

/** TermKey is one key press (the KeyboardEvent fields that matter). */
export interface TermKey {
  key: string;
  code?: string;
  ctrl?: boolean;
  alt?: boolean;
  shift?: boolean;
  meta?: boolean;
}

/** typeBuf is the shell the keys go to (null: none can type now). */
export const typeBuf = signal<number | null>(null);

/** termInputMode is the terminal input's inputmode: "text" opens the
 * phone's keyboard, "none" leaves the typing to the page's own. */
export const termInputMode = signal<"text" | "none">("text");

export function setTermInputMode(mode: "text" | "none"): void {
  termInputMode.value = mode;
}

/** termKeyBytes is what the shell gets for k, with the key bar's sticky
 * Ctrl and Alt (it does not let them go). null: nothing to send. */
export function termKeyBytes(k: TermKey): string | null {
  const c = !!k.ctrl || ctrl.peek() > 0;
  const a = !!k.alt || alt.peek() > 0;
  if (k.meta) return null;
  const named = keyBytes({ key: k.key, code: k.code ?? "", ctrlKey: c, altKey: a, shiftKey: !!k.shift, metaKey: false }, false);
  if (named !== null) return named;
  return k.key.length > 0 && [...k.key].length === 1 ? withMods(k.key, c, a) : null;
}

/** pressTermKey sends one key to the shown shell; false: nothing went. */
export function pressTermKey(k: TermKey): boolean {
  const buf = typeBuf.peek();
  const bytes = termKeyBytes(k);
  if (buf === null || bytes === null) return false;
  used();
  void sendText(buf, bytes);
  return true;
}

/** typeTerm sends what the terminal engine took (typing, input
 * methods, paste): one plain character takes the sticky modifiers. */
export function typeTerm(data: string): void {
  const buf = typeBuf.peek();
  if (buf === null || !data) return;
  if ([...data].length === 1 && data >= " " && (ctrl.peek() > 0 || alt.peek() > 0)) {
    data = withMods(data, ctrl.peek() > 0, alt.peek() > 0);
    used();
  }
  void sendText(buf, data);
}
