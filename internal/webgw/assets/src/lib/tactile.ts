// The Tactile Code keyboard (tactile.js) for a text surface: on a touch
// device, unless This phone → Keyboard turned it off, the surface's focus
// target gets the on-screen keyboard instead of the system one.
import type * as TactileModule from "../tactile/main";
import { prefs } from "./prefs";
import { kbPrefs } from "./tactileprefs";

type Mod = typeof TactileModule;
let mod: Promise<Mod> | undefined;
let got: Mod | undefined;

function load(): Promise<Mod> {
  if (!document.querySelector('link[href="/assets/tactile.css"]')) {
    const l = document.createElement("link");
    l.rel = "stylesheet";
    l.href = "/assets/tactile.css";
    document.head.append(l);
  }
  return import("/assets/tactile.js").then((m: Mod) => (got = m));
}

/** toTactile: the key bar's ⌨ key, back from the system keyboard. */
export const toTactile = (): void => got?.toTactile();

/** tactile binds el; the returned function unbinds it. A keyboard file
 * that could not load (the panel was down, the link dropped) loads again
 * on the surface's next focus, and the keyboard docks then. */
export function tactile(el: HTMLElement, sink: TactileModule.Sink | TactileModule.EditorSpec): () => void {
  let unbind: (() => void) | undefined;
  let gone = false;
  const tryBind = (): void => {
    el.removeEventListener("focus", tryBind);
    (mod ??= load()).then(
      (m) => {
        if (!gone && !unbind) unbind = m.bind(el, sink, { haptics: () => prefs.peek().haptics });
      },
      () => {
        mod = undefined;
        if (!gone) el.addEventListener("focus", tryBind);
      },
    );
  };
  if (matchMedia("(pointer: coarse)").matches && kbPrefs().on) tryBind();
  return () => {
    gone = true;
    el.removeEventListener("focus", tryBind);
    unbind?.();
  };
}
