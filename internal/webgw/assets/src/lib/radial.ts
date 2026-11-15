// The floating radial menu (RM-2) on a touch phone or tablet: radial.js
// and radial.css load with this file's one dynamic import when the shell
// shows (radial.js links radial.css itself), and get the page's own ops (radial/actions.ts), so the page's
// first files and the desktop never carry them. A failed load stays off.
import { h, render } from "preact";
import { useEffect } from "preact/hooks";
import type * as RadialModule from "../radial";
import { link, live, store } from "../session";
import { readGit } from "../state/git";
import { t } from "../strings";
import { AskLaptop } from "../ui/AskLaptop";
import { newChat } from "../views/Chat";
import { showOnLaptop } from "../views/FileView";
import { newTerm } from "../views/Term";
import { layout } from "./layout";
import { editing } from "./live";
import { go, route } from "./router";
import { toast } from "./toasts";
import { addRef } from "./tochat";

const host: RadialModule.RadialHost = {
  h, render, AskLaptop, go, route, layout, link, live, t, toast, addRef, newChat, newTerm, showOnLaptop,
  git: () => readGit(store.topic("git").peek()?.data),
  edit: (p) => (editing.value = p),
};

let mod: Promise<typeof RadialModule> | null = null;
const load = (): Promise<typeof RadialModule> =>
  (mod ??= import("/assets/radial.js").catch((e: unknown) => {
    mod = null;
    throw e;
  }));

/** useRadial shows the radial while on (a phone or tablet shell) on a
 * touch screen. */
export function useRadial(on: boolean): void {
  useEffect(() => {
    if (!on || !(navigator.maxTouchPoints > 0 || matchMedia("(pointer: coarse)").matches)) return;
    let stop: (() => void) | null = null, gone = false;
    load().then((m) => !gone && (stop = m.init(host)), () => undefined);
    return () => {
      gone = true;
      stop?.();
    };
  }, [on]);
}
