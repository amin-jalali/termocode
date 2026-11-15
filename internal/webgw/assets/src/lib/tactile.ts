// The Tactile Code keyboard (tactile.js) for a text surface: on a touch
// device, unless This phone → Keyboard turned it off, the surface's focus
// target gets the on-screen keyboard instead of the system one.
import type * as TactileModule from "../tactile/main";
import { prefs } from "./prefs";
import { kbPrefs } from "./tactileprefs";

type Mod = typeof TactileModule;
let mod: Promise<Mod> | undefined;

function load(): Promise<Mod> {
  const l = document.createElement("link");
  l.rel = "stylesheet";
  l.href = "/assets/tactile.css";
  document.head.append(l);
  return import("/assets/tactile.js");
}

/** tactile binds el; the returned function unbinds it. */
export function tactile(el: HTMLElement, sink: TactileModule.Sink | TactileModule.EditorSpec): () => void {
  let unbind: (() => void) | undefined;
  let gone = false;
  if (matchMedia("(pointer: coarse)").matches && kbPrefs().on)
    (mod ??= load()).then(
      (m) => {
        if (!gone) unbind = m.bind(el, sink, { haptics: () => prefs.peek().haptics });
      },
      () => (mod = undefined),
    );
  return () => {
    gone = true;
    unbind?.();
  };
}
